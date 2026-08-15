import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import type {
  AgentSession,
  AgentStartCapability,
  AgentType,
  AttachmentMode,
  AttachmentRef,
  DeliveryStatus,
  MessageType,
  NekoMessage,
  PendingUserInput,
  QueueItem,
  PromptQueueState,
  SessionListPayload,
  SessionMessage
} from '@/types/protocol'
import { nestTransportMode } from '@/types/protocol'
import { nekoWS } from '@/api/websocket'
import { apiFetch } from '@/api/http'
import { getPhoneId } from '@/api/http'
import { decryptSealedPayload, encryptSessionPayload, ingestKeyPackageMessage } from '@/crypto/keys'
import { tGlobal } from '@/i18n'
import { mergeHistoryLists, upsertMessageList } from '@/utils/messageMerge'

export const MAX_OUTBOX = 40
export const PROMPT_ACK_TIMEOUT_MS = 15_000
export const LEGACY_OUTBOX_STORAGE_KEY = 'nekonest_prompt_outbox'
export const OUTBOX_STORAGE_KEY_PREFIX = 'nekonest_prompt_outbox:item:'

export const useSessionStore = defineStore('sessions', () => {
  const sessions = ref<AgentSession[]>([])
  /** Null means an older daemon did not publish a device-level start catalog. */
  const startCapabilities = ref<AgentStartCapability[] | null>(null)
  const catalogStatus = ref<'loading' | 'ready'>('loading')
  const catalogDeviceId = ref<string | null>(null)
  const catalogProducerVersion = ref<string | null>(null)
  const currentSession = ref<AgentSession | null>(null)
  const messages = ref<SessionMessage[]>([])
  const loading = ref(false)
  const importing = ref(false)
  const streaming = ref(false)
  const lastError = ref<string | null>(null)
  const lastUserInputResult = ref<{ requestId: string; status: string } | null>(null)
  const wsStatus = ref<'connecting' | 'connected' | 'disconnected' | 'auth_error' | 'transport_error'>('disconnected')

  const HANDLER_ID = 'session-store'
  let historyGeneration = 0
  let streamPollTimer: number | null = null
  let streamIdleTimer: number | null = null
  let activeDeviceId: string | null = null

  const currentSessionCatalogVisible = computed(() => {
    const session = currentSession.value
    if (!session) return false
    if (session.id.startsWith('local_draft_')) return true
    const deviceId = session.device_id || activeDeviceId
    return catalogStatus.value === 'ready' && catalogDeviceId.value === deviceId &&
      sessions.value.some(candidate => candidate.id === session.id)
  })
  /** Buffered live messages: key = deviceId::sessionId */
  const inbox = new Map<string, SessionMessage[]>()
  /** Unacked send_prompt outbox keyed by client_msg_id (stable across reconnect). */
  type OutboxItem = {
    clientMsgId: string
    deviceId: string
    sessionId: string
    prompt: string
    attachments?: Array<{ id?: string; url: string; name?: string; mime?: string; size?: number }>
    collaborationMode?: 'plan'
    status: 'queued' | 'sending' | 'failed'
    error?: string
    retryAllowed: boolean
    createdAt: number
    /** Exact sealed command frame. Retrying must never create a new nonce. */
    sealedWire?: NekoMessage
  }
  const outbox = new Map<string, OutboxItem>()
  const ackTimers = new Map<string, number>()
  const sealingPromises = new Map<string, Promise<boolean>>()
  /** Daemon-owned FIFO snapshots, keyed by device/session. */
  const promptQueues = ref<Record<string, PromptQueueState>>({})
  /** Queue snapshot for the currently open thread; prompt bodies stay in the durable outbox. */
  const currentPromptQueue = computed<PromptQueueState | null>(() => {
    const session = currentSession.value
    if (!session?.device_id || !session.id) return null
    return promptQueues.value[queueKey(session.device_id, session.id)] || null
  })

  function queueKey(deviceId: string, sessionId: string) {
    return `${deviceId}::${sessionId}`
  }

  const sealedInboundApplicationTypes = new Set<MessageType>([
    'session_list', 'session_update', 'session_message', 'session_history',
    'prompt_queued', 'prompt_accepted', 'prompt_failed', 'prompt_sent',
    'user_input_result', 'queue_update', 'prompt_cancelled',
    'thread_starting', 'thread_owned', 'thread_failed', 'thread_indeterminate'
  ])

  function messagePassesTransportPolicy(msg: NekoMessage): boolean {
    const mode = nestTransportMode()
    if (msg.transport_mode && msg.transport_mode !== mode) return false
    if (msg.payload && msg.sealed_payload) return false
    if (mode === 'open') return !msg.sealed_payload
    if (!sealedInboundApplicationTypes.has(msg.type)) return true
    // A server-generated definitive delivery failure may expose only allowed
    // routing metadata. It cannot contain application text.
    if (msg.type === 'prompt_failed' && !msg.payload && !msg.sealed_payload && msg.client_msg_id) return true
    if (msg.type === 'thread_indeterminate' && !msg.payload && !msg.sealed_payload && msg.client_msg_id) return true
    return !!msg.sealed_payload && !msg.payload
  }

  function withAuthenticatedMessagePayload(
    msg: NekoMessage,
    apply: (payload: Record<string, unknown>) => void
  ) {
    if (nestTransportMode() === 'open') {
      apply((msg.payload || {}) as Record<string, unknown>)
      return
    }
    if (!msg.sealed_payload || !msg.device_id) return
    const sp = msg.sealed_payload
    void decryptSealedPayload(msg.device_id, msg.session_id, sp, {
      protocol_version: msg.protocol_version || '1.1',
      transport_mode: 'sealed',
      type: msg.type,
      device_id: msg.device_id,
      session_id: msg.session_id,
      client_msg_id: msg.client_msg_id,
      key_scope: sp.key_scope,
      key_epoch: sp.epoch,
      sender_id: sp.sender_id,
      sequence: sp.sequence,
      timestamp: msg.timestamp
    }).then(plain => {
      if (plain && typeof plain === 'object' && !Array.isArray(plain)) {
        const payload = plain as Record<string, unknown>
        const innerClientMsgId = String(payload.client_msg_id || payload.operation_id || '').trim()
        if (msg.client_msg_id && innerClientMsgId && innerClientMsgId !== msg.client_msg_id) return
        apply(payload)
      }
    })
  }

  function isSealedPromptEnvelope(value: unknown, clientMsgId: string): value is NekoMessage {
    if (!value || typeof value !== 'object') return false
    const frame = value as Partial<NekoMessage>
    return frame.type === 'send_prompt' && frame.client_msg_id === clientMsgId &&
      typeof frame.device_id === 'string' && typeof frame.timestamp === 'number' &&
      !!frame.sealed_payload && !frame.payload
  }

  function applyQueueUpdate(deviceId: string, sessionId: string, payload: Record<string, unknown>) {
    if (!sessionId) return
    const rawItems = Array.isArray(payload.items) ? payload.items : []
    const items: QueueItem[] = rawItems.flatMap<QueueItem>(value => {
      if (!value || typeof value !== 'object') return []
      const item = value as Record<string, unknown>
      const clientMsgId = typeof item.client_msg_id === 'string' ? item.client_msg_id.trim() : ''
      if (!clientMsgId) return []
      const rawStatus = String(item.status || '')
      const status: QueueItem['status'] = rawStatus === 'starting' || rawStatus === 'running'
        ? 'running'
        : rawStatus === 'paused'
          ? 'blocked_indeterminate'
          : ['queued', 'completed', 'blocked_failed', 'blocked_interrupted', 'blocked_indeterminate', 'cancelled'].includes(rawStatus)
            ? rawStatus as QueueItem['status']
            : 'blocked_indeterminate'
      return [{
        client_msg_id: clientMsgId,
        position: typeof item.position === 'number' ? item.position : 0,
        status
      }]
    })
    promptQueues.value = {
      ...promptQueues.value,
      [queueKey(deviceId, sessionId)]: { paused: payload.paused === true, items }
    }
  }

  function noteQueuedPrompt(
    deviceId: string,
    sessionId: string,
    clientMsgId: string,
    queuePosition?: unknown
  ) {
    if (!sessionId || !clientMsgId) return
    const key = queueKey(deviceId, sessionId)
    const current = promptQueues.value[key] || { paused: false, items: [] }
    const position = typeof queuePosition === 'number' && queuePosition > 0
      ? queuePosition
      : current.items.length + 1
    const others = current.items.filter(item => item.client_msg_id !== clientMsgId)
    const newItem: QueueItem = { client_msg_id: clientMsgId, position, status: 'queued' }
    const items: QueueItem[] = [...others, newItem]
      .sort((a, b) => a.position - b.position)
    promptQueues.value = {
      ...promptQueues.value,
      [key]: {
        paused: current.paused,
        items
      }
    }
  }

  function removeQueuedPrompt(deviceId: string, sessionId: string, clientMsgId: string) {
    const key = queueKey(deviceId, sessionId)
    const current = promptQueues.value[key]
    if (!current) return
    promptQueues.value = {
      ...promptQueues.value,
      [key]: {
        ...current,
        items: current.items.filter(item => item.client_msg_id !== clientMsgId)
      }
    }
  }

  function normalizeOutboxItem(value: unknown): OutboxItem | null {
    if (!value || typeof value !== 'object') return null
    const it = value as Partial<OutboxItem>
    if (!it.clientMsgId || !it.deviceId || !it.sessionId || typeof it.prompt !== 'string') {
      return null
    }
    return {
      clientMsgId: it.clientMsgId,
      deviceId: it.deviceId,
      sessionId: it.sessionId,
      prompt: it.prompt,
      attachments: Array.isArray(it.attachments) ? it.attachments : undefined,
      collaborationMode: it.collaborationMode === 'plan' ? 'plan' : undefined,
      status:
        it.status === 'failed'
          ? 'failed'
          : it.status === 'sending'
            ? 'sending'
            : 'queued',
      error: typeof it.error === 'string' ? it.error : undefined,
      retryAllowed: it.retryAllowed !== false,
      createdAt: Number.isFinite(it.createdAt)
        ? Number(it.createdAt)
        : Math.floor(Date.now() / 1000),
      sealedWire: isSealedPromptEnvelope(it.sealedWire, it.clientMsgId)
        ? it.sealedWire
        : undefined
    }
  }

  function storageKey(clientMsgId: string) {
    return `${OUTBOX_STORAGE_KEY_PREFIX}${encodeURIComponent(clientMsgId)}`
  }

  function migrateLegacyOutbox() {
    try {
      const raw = localStorage.getItem(LEGACY_OUTBOX_STORAGE_KEY)
      if (!raw) return
      const arr = JSON.parse(raw) as unknown[]
      if (!Array.isArray(arr)) return
      for (const value of arr) {
        const item = normalizeOutboxItem(value)
        if (!item) continue
        const key = storageKey(item.clientMsgId)
        if (localStorage.getItem(key) === null) {
          localStorage.setItem(key, JSON.stringify(item))
        }
      }
      localStorage.removeItem(LEGACY_OUTBOX_STORAGE_KEY)
    } catch (error) {
      // Leave the legacy array intact unless every item was migrated.
      console.error('[session] failed to migrate legacy prompt outbox:', error)
    }
  }

  function readStoredOutbox(): Map<string, OutboxItem> | null {
    try {
      migrateLegacyOutbox()
      const stored = new Map<string, OutboxItem>()
      for (let index = 0; index < localStorage.length; index++) {
        const key = localStorage.key(index)
        if (!key?.startsWith(OUTBOX_STORAGE_KEY_PREFIX)) continue
        const raw = localStorage.getItem(key)
        if (!raw) continue
        const item = normalizeOutboxItem(JSON.parse(raw))
        if (item) stored.set(item.clientMsgId, item)
      }
      // If migration could not complete, still read valid legacy items without
      // allowing them to overwrite newer per-command records.
      const legacyRaw = localStorage.getItem(LEGACY_OUTBOX_STORAGE_KEY)
      if (legacyRaw) {
        const legacy = JSON.parse(legacyRaw) as unknown[]
        if (Array.isArray(legacy)) {
          for (const value of legacy) {
            const item = normalizeOutboxItem(value)
            if (item && !stored.has(item.clientMsgId)) {
              stored.set(item.clientMsgId, item)
            }
          }
        }
      }
      return stored
    } catch (error) {
      console.error('[session] failed to read prompt outbox:', error)
      return null
    }
  }

  function syncOutboxFromStorage(initialLoad = false) {
    const stored = readStoredOutbox()
    if (!stored) return
    outbox.clear()
    for (const [clientMsgId, item] of stored) {
      if (initialLoad && item.status !== 'failed') item.status = 'queued'
      outbox.set(clientMsgId, item)
    }
    for (const clientMsgId of [...ackTimers.keys()]) {
      if (outbox.get(clientMsgId)?.status !== 'sending') {
        clearAckTimer(clientMsgId)
      }
    }
  }

  function persistOutboxItem(item: OutboxItem): boolean {
    try {
      localStorage.setItem(storageKey(item.clientMsgId), JSON.stringify(item))
      return true
    } catch (error) {
      console.error('[session] failed to persist prompt outbox item:', error)
      return false
    }
  }

  function removePersistedOutboxItem(clientMsgId: string): boolean {
    try {
      localStorage.removeItem(storageKey(clientMsgId))
      return true
    } catch (error) {
      console.error('[session] failed to remove prompt outbox item:', error)
      return false
    }
  }

  syncOutboxFromStorage(true)

  function handleOutboxStorage(event: StorageEvent) {
    if (
      event.storageArea &&
      event.storageArea !== localStorage
    ) return
    if (
      event.key !== null &&
      event.key !== LEGACY_OUTBOX_STORAGE_KEY &&
      !event.key.startsWith(OUTBOX_STORAGE_KEY_PREFIX)
    ) return
    const previousIds = new Set(outbox.keys())
    syncOutboxFromStorage()
    for (const clientMsgId of previousIds) {
      if (!outbox.has(clientMsgId)) {
        clearAckTimer(clientMsgId)
        patchDeliveryMessage(clientMsgId)
      }
    }
    if (currentSession.value) {
      restoreOutboxMessages(currentSession.value.device_id, currentSession.value.id)
    }
  }

  if (typeof window !== 'undefined') {
    window.addEventListener('storage', handleOutboxStorage)
  }

  function inboxKey(deviceId: string, sessionId: string) {
    return `${deviceId}::${sessionId}`
  }

  function flushOutbox() {
    syncOutboxFromStorage()
    const ws = nekoWS()
    if (!ws.isConnected()) return
    for (const it of outbox.values()) {
      if (activeDeviceId && it.deviceId !== activeDeviceId) continue
      if (it.status === 'failed') continue
      sendOutboxItem(it)
    }
  }

  function sendOutboxItem(
    it: OutboxItem,
    _explicitRetry = false,
    scheduleAck = true
  ): boolean {
    const payload: Record<string, unknown> = {
      prompt: it.prompt,
      client_msg_id: it.clientMsgId
    }
    if (it.attachments?.length) payload.attachments = it.attachments
    if (it.collaborationMode === 'plan') payload.collaboration_mode = 'plan'
    const mode = nestTransportMode()
    if (mode === 'sealed') {
      // Persist and reuse the entire original envelope. Re-encrypting a retry
      // changes its nonce/sequence and turns one durable command into two.
      if (it.sealedWire) {
        const sent = nekoWS().send(it.sealedWire)
        it.status = sent ? 'sending' : 'queued'
        delete it.error
        persistOutboxItem(it)
        patchDeliveryMessage(it.clientMsgId, it.status)
        if (sent && scheduleAck) scheduleAckQuery(it.clientMsgId)
        else clearAckTimer(it.clientMsgId)
        return sent
      }
      if (sealingPromises.has(it.clientMsgId)) return true
      // Async seal then send; one client_msg_id has exactly one in-flight
      // encryption and one persisted immutable frame.
      it.status = 'queued'
      persistOutboxItem(it)
      patchDeliveryMessage(it.clientMsgId, 'queued')
      const sealing = (async (): Promise<boolean> => {
        let sealed: Awaited<ReturnType<typeof encryptSessionPayload>> = null
        const timestamp = Math.floor(Date.now() / 1000)
        try {
          sealed = await encryptSessionPayload(
            it.deviceId,
            it.sessionId,
            getPhoneId() || 'phone',
            'send_prompt',
            payload,
            it.clientMsgId,
            timestamp,
            nekoWS().getProtocolVersion()
          )
        } catch {
          // Fail closed below: a sealed nest must never leak a plaintext retry.
        }
        if (!sealed) {
          const current = outbox.get(it.clientMsgId) || it
          current.status = 'failed'
          current.error = tGlobal('errors.sealedPromptKeyUnavailable')
          current.retryAllowed = false
          persistOutboxItem(current)
          patchDeliveryMessage(current.clientMsgId, 'failed', current.error, false)
          lastError.value = current.error
          return false
        }
        const frame: NekoMessage = {
          protocol_version: nekoWS().getProtocolVersion(),
          type: 'send_prompt',
          device_id: it.deviceId,
          session_id: it.sessionId,
          client_msg_id: it.clientMsgId,
          timestamp,
          sealed_payload: sealed
        }
        // Write the immutable frame before it crosses the socket. A reload or
        // failed write can then repeat the exact ciphertext object.
        const current = outbox.get(it.clientMsgId)
        if (!current) return false
        current.sealedWire = frame
        if (!persistOutboxItem(current)) {
          current.status = 'failed'
          current.error = tGlobal('errors.outboxPersist')
          current.retryAllowed = false
          patchDeliveryMessage(current.clientMsgId, 'failed', current.error, false)
          lastError.value = current.error
          return false
        }
        const sent = nekoWS().send(frame)
        current.status = sent ? 'sending' : 'queued'
        delete current.error
        persistOutboxItem(current)
        patchDeliveryMessage(current.clientMsgId, current.status)
        if (sent && scheduleAck) scheduleAckQuery(current.clientMsgId)
        return sent
      })().finally(() => sealingPromises.delete(it.clientMsgId))
      sealingPromises.set(it.clientMsgId, sealing)
      return true
    }

    const sent = nekoWS().send({
      type: 'send_prompt',
      device_id: it.deviceId,
      session_id: it.sessionId,
      client_msg_id: it.clientMsgId,
      timestamp: Math.floor(Date.now() / 1000),
      payload
    })
    it.status = sent ? 'sending' : 'queued'
    delete it.error
    if (!persistOutboxItem(it)) {
      lastError.value = tGlobal('errors.outboxPersist')
    }
    patchDeliveryMessage(it.clientMsgId, it.status)
    if (sent && scheduleAck) {
      scheduleAckQuery(it.clientMsgId)
    } else {
      clearAckTimer(it.clientMsgId)
    }
    return sent
  }

  function scheduleAckQuery(clientMsgId: string) {
    clearAckTimer(clientMsgId)
    const timer = window.setTimeout(() => {
      ackTimers.delete(clientMsgId)
      const item = outbox.get(clientMsgId)
      if (!item || item.status !== 'sending') return
      if (!nekoWS().isConnected()) {
        item.status = 'queued'
        persistOutboxItem(item)
        patchDeliveryMessage(clientMsgId, 'queued')
        return
      }
      // Same id and no retry flag: the server treats this as a status query
      // for a pending/terminal command, never as permission to rerun a failure.
      sendOutboxItem(item, false, false)
    }, PROMPT_ACK_TIMEOUT_MS)
    ackTimers.set(clientMsgId, timer)
  }

  function clearAckTimer(clientMsgId: string) {
    const timer = ackTimers.get(clientMsgId)
    if (timer !== undefined) {
      window.clearTimeout(timer)
      ackTimers.delete(clientMsgId)
    }
  }

  function clearAllAckTimers() {
    for (const timer of ackTimers.values()) {
      window.clearTimeout(timer)
    }
    ackTimers.clear()
  }

  function patchDeliveryMessage(
    clientMsgId: string,
    status?: DeliveryStatus,
    error?: string,
    retryAllowed = true
  ) {
    const idx = messages.value.findIndex(m => m.id === clientMsgId)
    if (idx < 0) return
    const prev = messages.value[idx]
    const metadata = { ...(prev.metadata || {}) }
    if (status) metadata.delivery_status = status
    else delete metadata.delivery_status
    if (error) metadata.delivery_error = error
    else delete metadata.delivery_error
    if (status === 'failed' || status === 'indeterminate' || status === 'not_seen') {
      metadata.delivery_retry_allowed = retryAllowed
    } else {
      delete metadata.delivery_retry_allowed
    }
    messages.value[idx] = { ...prev, metadata }
    messages.value = [...messages.value]
  }

  function clearOutboxCommitted(clientMsgId: string) {
    const cid = clientMsgId.trim()
    if (!cid) return
    clearAckTimer(cid)
    outbox.delete(cid)
    removePersistedOutboxItem(cid)
  }

  function applyPromptCommitted(
    msg: NekoMessage,
    deviceId: string,
    p: {
      prompt?: string
      attachments?: unknown[]
      message_id?: string
      client_msg_id?: string
    }
  ) {
    const cid = (p?.client_msg_id || msg.client_msg_id || '').trim()
    const stableId = (p?.message_id || cid || '').trim()
    if (cid) clearOutboxCommitted(cid)
    if (!currentSession.value || !msg.session_id || msg.session_id !== currentSession.value.id) return
    const prompt = p?.prompt || ''
    const atts = p?.attachments
    const matchIdx = messages.value.findIndex(
      m => m.role === 'user' && (m.id === stableId || (cid && m.id === cid))
    )
    if (matchIdx >= 0) {
      const prev = messages.value[matchIdx]
      const metadata = { ...(prev.metadata || {}) }
      delete metadata.delivery_status
      delete metadata.delivery_error
      delete metadata.delivery_retry_allowed
      if (atts && Array.isArray(atts) && atts.length) {
        metadata.attachments = atts as never
      }
      messages.value[matchIdx] = {
        ...prev,
        id: stableId || prev.id,
        content: prompt || prev.content,
        metadata
      }
      messages.value = [...messages.value]
      startStreamPoll(msg.device_id || deviceId, msg.session_id)
      return
    }
    upsertMessage({
      id: stableId || `user_${Date.now()}`,
      role: 'user',
      content: prompt,
      type: 'text',
      timestamp: Math.floor(Date.now() / 1000),
      metadata: atts ? { attachments: atts as never } : undefined
    })
    startStreamPoll(msg.device_id || deviceId, msg.session_id)
  }

  function restoreOutboxMessages(deviceId: string, sessionId: string) {
    for (const it of outbox.values()) {
      if (it.deviceId !== deviceId || it.sessionId !== sessionId) continue
      const display = it.prompt || tGlobal('outbox.attachmentsOnly', { n: it.attachments?.length || 0 })
      upsertMessage({
        id: it.clientMsgId,
        role: 'user',
        content: display,
        type: 'text',
        timestamp: it.createdAt,
        metadata: {
          ...(it.attachments?.length ? { attachments: it.attachments } : {}),
          delivery_status: it.status,
          ...(it.error ? { delivery_error: it.error } : {}),
          ...(it.status === 'failed' ? { delivery_retry_allowed: it.retryAllowed } : {})
        }
      })
    }
  }

  function subscribeDevice(deviceId: string) {
    const ws = nekoWS()
    if (activeDeviceId !== deviceId) {
      // Clear cross-device leakage immediately.
      sessions.value = []
      startCapabilities.value = null
      catalogStatus.value = 'loading'
      catalogDeviceId.value = deviceId
      // A catalog from the previous device cannot authorize controls or history
      // on the next device, including legacy sessions without device_id.
      setCurrentSession(null)
      activeDeviceId = deviceId
    }
    ws.onStatusChange(HANDLER_ID, (s) => {
      wsStatus.value = s
      if (s === 'connected') {
        ws.whenReady(() => flushOutbox())
      }
    })

    ws.removeHandler(HANDLER_ID)
    ws.addHandler(HANDLER_ID, (msg) => {
      if (msg.device_id && activeDeviceId && msg.device_id !== activeDeviceId) {
        return
      }
      if (!messagePassesTransportPolicy(msg)) {
        if (
          msg.type === 'thread_starting' || msg.type === 'thread_owned' ||
          msg.type === 'thread_failed' || msg.type === 'thread_indeterminate'
        ) {
          applyStartThreadResult(
            'thread_indeterminate',
            { operation_id: msg.client_msg_id },
            msg.device_id || deviceId
          )
        }
        return
      }
      if (msg.type === 'key_package' && msg.device_id) {
        void ingestKeyPackageMessage(msg.device_id, (msg.payload || {}) as Record<string, unknown>)
        return
      }
      if (
        msg.type === 'thread_starting' ||
        msg.type === 'thread_owned' ||
        msg.type === 'thread_failed' ||
        msg.type === 'thread_indeterminate'
      ) {
        const did = msg.device_id || deviceId
        const applyAuthenticatedResult = (payload: Record<string, unknown> | undefined) => {
          applyStartThreadResult(
            msg.type,
            {
              ...(payload || {}),
              operation_id: payload?.operation_id || msg.client_msg_id,
              session_id: payload?.session_id || msg.session_id
            },
            did
          )
        }
        const applyUnauthenticatedResult = () => {
          applyStartThreadResult(
            'thread_indeterminate',
            { operation_id: msg.client_msg_id },
            did
          )
        }
        if (nestTransportMode() === 'sealed') {
          if (!msg.sealed_payload || !msg.device_id) {
            applyUnauthenticatedResult()
            return
          }
          const sp = msg.sealed_payload
          void decryptSealedPayload(
            msg.device_id,
            msg.session_id,
            sp,
            {
              protocol_version: msg.protocol_version || '1.1',
              transport_mode: 'sealed',
              type: msg.type,
              device_id: msg.device_id,
              session_id: msg.session_id,
              client_msg_id: msg.client_msg_id,
              key_scope: sp.key_scope,
              key_epoch: sp.epoch,
              sender_id: sp.sender_id,
              sequence: sp.sequence,
              timestamp: msg.timestamp
            }
          ).then(plain => {
            if (!plain || typeof plain !== 'object' || Array.isArray(plain)) {
              applyUnauthenticatedResult()
              return
            }
            applyAuthenticatedResult(plain as Record<string, unknown>)
          })
        } else {
          applyAuthenticatedResult((msg.payload || {}) as Record<string, unknown>)
        }
        return
      }
      if (msg.type === 'device_online') {
        // A pending status query can go unanswered while the daemon is offline
        // even though the phone socket stays connected. Query again on return.
        flushOutbox()
      } else if (msg.type === 'session_list' && msg.device_id === deviceId) {
        withAuthenticatedMessagePayload(msg, payload => {
          applySessionList(payload as SessionListPayload, deviceId, msg.protocol_version)
          catalogDeviceId.value = deviceId
          catalogStatus.value = 'ready'
          if (currentSession.value) {
            if (currentSession.value.device_id && currentSession.value.device_id !== deviceId) {
              currentSession.value = null
              messages.value = []
            } else {
              const updated = sessions.value.find(s => s.id === currentSession.value!.id)
              if (updated) {
                currentSession.value = updated
                // Discovery is authoritative for busy state: leave "replying" when idle.
                if (updated.status === 'idle' || updated.status === 'error') {
                  streaming.value = false
                  stopStreamPoll()
                }
              } else if (!currentSession.value.id.startsWith('local_draft_')) {
                // session_list is authoritative for native thread visibility. Clear
                // stale busy/capability state before rendering the hidden deep link.
                setCurrentSession(null)
              }
            }
          }
        })
      } else if (msg.type === 'session_update') {
		const applySessionUpdate = (plain: Record<string, unknown> | undefined) => {
		const raw = (plain?.session ?? plain) as AgentSession | undefined
		if (!raw?.id) return
		const idx = sessions.value.findIndex(s => s.id === raw.id)
		const previous = idx >= 0
		  ? sessions.value[idx]
		  : currentSession.value?.id === raw.id
		    ? currentSession.value
		    : undefined
		// session_update is a patch, not a replacement. Active-turn, approval,
		// and user-input updates intentionally omit the capability table.
		const merged = previous
		  ? {
		      ...previous,
		      ...raw,
		      capabilities: raw.capabilities === undefined ? previous.capabilities : raw.capabilities
		    }
		  : raw
		const updated = normalizeSessionCapabilities(merged, msg.protocol_version)
		if (!updated) return
        if (updated.agent_type === 'kilo') {
          sessions.value = sessions.value.filter(session => session.id !== updated.id)
          if (currentSession.value?.id === updated.id) setCurrentSession(null)
          return
        }
        if (updated.device_id && updated.device_id !== deviceId) return
        if (idx >= 0) {
          sessions.value[idx] = updated
          if (currentSession.value?.id === updated.id) {
            currentSession.value = sessions.value[idx]
          }
		} else {
		  sessions.value.push({ ...updated, device_id: updated.device_id || deviceId })
		}
		}
		if (msg.sealed_payload && msg.device_id) {
		  const sp = msg.sealed_payload
		  void decryptSealedPayload(msg.device_id, msg.session_id, sp, {
			protocol_version: msg.protocol_version || '1.1',
			transport_mode: 'sealed', type: 'session_update', device_id: msg.device_id,
			session_id: msg.session_id, key_scope: sp.key_scope, key_epoch: sp.epoch,
			sender_id: sp.sender_id, sequence: sp.sequence, timestamp: msg.timestamp
		  }).then(plain => {
			if (plain && typeof plain === 'object' && !Array.isArray(plain)) {
			  applySessionUpdate(plain as Record<string, unknown>)
			}
		  })
		} else {
		  applySessionUpdate((msg.payload || {}) as Record<string, unknown>)
		}
      } else if (msg.type === 'user_input_result') {
		const applyResult = (result: Record<string, unknown>) => {
		  const requestId = String(result.request_id || '')
		  const status = String(result.status || '')
		  const pending = currentSession.value?.pending_user_input
		  if (!pending || pending.request_id !== requestId) return
		  lastUserInputResult.value = { requestId, status }
		  if (status === 'accepted' || status === 'expired' || status === 'stale' || status === 'indeterminate') {
			currentSession.value = { ...currentSession.value!, pending_user_input: undefined }
		  }
		  if (status !== 'accepted') {
			lastError.value = String(result.message || tGlobal('session.userInputFailed'))
		  }
		}
		if (msg.sealed_payload && msg.device_id) {
		  const sp = msg.sealed_payload
		  void decryptSealedPayload(msg.device_id, msg.session_id, sp, {
			protocol_version: msg.protocol_version || '1.1', transport_mode: 'sealed',
			type: 'user_input_result', device_id: msg.device_id, session_id: msg.session_id,
			key_scope: sp.key_scope, key_epoch: sp.epoch, sender_id: sp.sender_id,
			sequence: sp.sequence, timestamp: msg.timestamp
		  }).then(plain => {
			if (plain && typeof plain === 'object' && !Array.isArray(plain)) applyResult(plain as Record<string, unknown>)
		  })
		} else applyResult((msg.payload || {}) as Record<string, unknown>)
		} else if (msg.type === 'queue_update') {
        const applyUpdate = (payload: Record<string, unknown>) => {
          const sid = String(payload.session_id || msg.session_id || '').trim()
          applyQueueUpdate(msg.device_id || deviceId, sid, payload)
        }
        if (msg.sealed_payload && msg.device_id) {
          const sp = msg.sealed_payload
          void decryptSealedPayload(msg.device_id, msg.session_id, sp, {
            protocol_version: msg.protocol_version || '1.1', transport_mode: 'sealed',
            type: 'queue_update', device_id: msg.device_id, session_id: msg.session_id,
            key_scope: sp.key_scope, key_epoch: sp.epoch, sender_id: sp.sender_id,
            sequence: sp.sequence, timestamp: msg.timestamp
          }).then(plain => {
            if (plain && typeof plain === 'object' && !Array.isArray(plain)) applyUpdate(plain as Record<string, unknown>)
          })
        } else applyUpdate((msg.payload || {}) as Record<string, unknown>)
      } else if (msg.type === 'prompt_cancelled') {
        const applyCancelled = (payload: Record<string, unknown>) => {
          const cid = String(payload.client_msg_id || '').trim()
          const sid = String(payload.session_id || msg.session_id || '').trim()
          if (!cid || !sid) return
          syncOutboxFromStorage()
          // A late cancellation must not erase a separately reported failed
          // delivery: that command is no longer a daemon-queued item and may
          // carry the only safe retry/indeterminate state for the user.
          const item = outbox.get(cid)
          if (item?.status !== 'failed') clearOutboxCommitted(cid)
          removeQueuedPrompt(msg.device_id || deviceId, sid, cid)
          if (currentSession.value?.id === sid && item?.status !== 'failed') {
            patchDeliveryMessage(cid, 'cancelled')
          }
        }
        if (msg.sealed_payload && msg.device_id) {
          const sp = msg.sealed_payload
          void decryptSealedPayload(msg.device_id, msg.session_id, sp, {
            protocol_version: msg.protocol_version || '1.1', transport_mode: 'sealed',
            type: 'prompt_cancelled', device_id: msg.device_id, session_id: msg.session_id,
            key_scope: sp.key_scope, key_epoch: sp.epoch, sender_id: sp.sender_id,
            sequence: sp.sequence, timestamp: msg.timestamp
          }).then(plain => {
            if (plain && typeof plain === 'object' && !Array.isArray(plain)) applyCancelled(plain as Record<string, unknown>)
          })
        } else applyCancelled((msg.payload || {}) as Record<string, unknown>)
		} else if (msg.type === 'session_message') {
        const applySessionMessage = (sessionMessage: SessionMessage) => {
          const sid = msg.session_id || (sessionMessage as { session_id?: string }).session_id
          if (!sid) return
          const did = msg.device_id || deviceId
          if (
            currentSession.value?.id === sid &&
            (!currentSession.value.device_id || currentSession.value.device_id === did)
          ) {
            upsertMessage(sessionMessage)
            if (sessionMessage.type === 'error') {
              lastError.value = sessionMessage.content.trim() || tGlobal('errors.agentError')
              importing.value = false
              streaming.value = false
              stopStreamPoll()
            } else {
              markStreamActivity()
            }
          } else {
            pushInbox(did, sid, sessionMessage)
          }
        }

        if (msg.sealed_payload && msg.device_id) {
          const sp = msg.sealed_payload
          void decryptSealedPayload(
            msg.device_id,
            msg.session_id,
            sp,
            {
              protocol_version: msg.protocol_version || '1.1',
              transport_mode: 'sealed',
              type: 'session_message',
              device_id: msg.device_id,
              session_id: msg.session_id,
              key_scope: sp.key_scope,
              key_epoch: sp.epoch,
              sender_id: sp.sender_id,
              sequence: sp.sequence,
              timestamp: msg.timestamp
            }
          ).then(plain => {
            const inner = plain as { message?: SessionMessage } | null
            if (inner?.message) applySessionMessage(inner.message)
          })
          return
        }

        const sessionMessage = msg.payload?.message as SessionMessage
        if (!sessionMessage) return
        applySessionMessage(sessionMessage)
      } else if (msg.type === 'session_history') {
        if (!currentSession.value || !msg.session_id || msg.session_id !== currentSession.value.id) return
        withAuthenticatedMessagePayload(msg, payload => {
          importing.value = false
          const hist = (payload.messages as SessionMessage[]) || []
          mergeHistory(hist)
          stopForTerminalHistoryError(hist)
        })
      } else if (msg.type === 'prompt_queued') {
        withAuthenticatedMessagePayload(msg, payload => {
          const cid = String(payload.client_msg_id || msg.client_msg_id || '').trim()
          if (!cid) return
          clearAckTimer(cid)
          const item = outbox.get(cid)
          if (item) {
            item.status = 'sending'
            persistOutboxItem(item)
          }
          noteQueuedPrompt(
            msg.device_id || deviceId,
            String(payload.session_id || msg.session_id || item?.sessionId || ''),
            cid,
            Number(payload.queue_position) || undefined
          )
          if (currentSession.value && msg.session_id === currentSession.value.id) {
            patchDeliveryMessage(cid, 'accepted')
          }
        })
      } else if (msg.type === 'prompt_accepted') {
        withAuthenticatedMessagePayload(msg, payload => {
          const cid = String(payload.client_msg_id || msg.client_msg_id || '').trim()
          if (!cid) return
          clearAckTimer(cid)
          const item = outbox.get(cid)
          if (item) {
            item.status = 'sending'
            persistOutboxItem(item)
          }
          if (currentSession.value && msg.session_id === currentSession.value.id) {
            patchDeliveryMessage(cid, 'accepted')
          }
          // Keep outbox until prompt_committed (at-most-once durability).
          // Older daemons used prompt_accepted{queued:true}; preserve that
          // shape without confusing admission with native turn acceptance.
          if (payload.queued === true) {
            noteQueuedPrompt(
              msg.device_id || deviceId,
              String(msg.session_id || item?.sessionId || ''),
              cid,
              Number(payload.queue_position) || undefined
            )
          } else {
            scheduleAckQuery(cid)
          }
        })
      } else if (msg.type === 'prompt_committed' || msg.type === 'prompt_sent') {
        // prompt_sent is deprecated; treat as committed for transition.
        applyPromptCommitted(msg, deviceId, (msg.payload || {}) as {
          prompt?: string
          attachments?: unknown[]
          message_id?: string
          client_msg_id?: string
        })
      } else if (msg.type === 'prompt_not_seen') {
        const p = msg.payload as { client_msg_id?: string }
        const cid = String(p?.client_msg_id || msg.client_msg_id || '').trim()
        if (!cid) return
        clearAckTimer(cid)
        const item = outbox.get(cid)
        if (item) {
          item.status = 'failed'
          item.error = tGlobal('outbox.notSeen')
          item.retryAllowed = true
          persistOutboxItem(item)
        }
        if (currentSession.value && msg.session_id === currentSession.value.id) {
          patchDeliveryMessage(cid, 'not_seen', tGlobal('outbox.notSeen'), true)
        }
      } else if (msg.type === 'prompt_failed') {
        const applyFailure = (payload: Record<string, unknown>) => {
          const cid = String(payload.client_msg_id || msg.client_msg_id || '').trim()
          const outcome = String(payload.outcome || msg.outcome || '')
          const retryAllowed = (payload.retry_allowed ?? msg.retry_allowed) !== false && outcome !== 'indeterminate'
          const failure = promptFailureMessage(payload, retryAllowed)
          if (cid) syncOutboxFromStorage()
          const item = cid ? outbox.get(cid) : undefined
          if (cid) clearAckTimer(cid)
          if (item) {
            item.status = 'failed'
            item.error = failure
            item.retryAllowed = retryAllowed
            persistOutboxItem(item)
          }
          const failedSessionId = msg.session_id || item?.sessionId
          if (currentSession.value && failedSessionId === currentSession.value.id) {
            lastError.value = failure
            importing.value = false
            streaming.value = false
            stopStreamPoll()
            if (cid) patchDeliveryMessage(cid, 'failed', failure, retryAllowed)
          }
        }
        if (!msg.sealed_payload && nestTransportMode() === 'sealed') {
          applyFailure({
            client_msg_id: msg.client_msg_id,
            outcome: msg.outcome,
            retry_allowed: msg.retry_allowed
          })
        } else {
          withAuthenticatedMessagePayload(msg, applyFailure)
        }
      } else if (msg.type === 'error') {
        const m = (msg.payload as { message?: string })?.message || 'unknown error'
        const matchesCurrent =
          !!currentSession.value &&
          !!msg.session_id &&
          msg.session_id === currentSession.value.id
        if (!msg.session_id || matchesCurrent) {
          lastError.value = m
        }
        if (matchesCurrent) {
          importing.value = false
          streaming.value = false
          stopStreamPoll()
          upsertMessage({
            id: `err_${Date.now()}`,
            role: 'system',
            content: m,
            type: 'error',
            timestamp: Math.floor(Date.now() / 1000)
          })
        }
      }
    })

    const forceRefresh = catalogStatus.value === 'loading' && ws.isConnected() &&
      ws.getSubscribedDevice() === deviceId
    ws.subscribe(deviceId, forceRefresh ? { force: true } : undefined)
  }

  function applySessionList(payload: SessionListPayload, deviceId: string, producerVersion?: string) {
    catalogProducerVersion.value = producerVersion || null
    sessions.value = (payload.sessions || []).map(session =>
      normalizeSessionCapabilities(session, producerVersion)
    ).filter(
      (s): s is AgentSession => !!s && s.agent_type !== 'kilo' && (!s.device_id || s.device_id === deviceId)
    )
    // REST snapshots omit this field. Do not wipe a WS-probed spawn catalog.
    if (Array.isArray(payload.start_capabilities)) {
      startCapabilities.value = payload.start_capabilities.filter(
        capability => capability.agent_type !== 'kilo'
      )
    }
  }

  function normalizeSessionCapabilities(session: AgentSession | undefined, producerVersion?: string): AgentSession | undefined {
    if (!session) return undefined
    const match = /^(\d+)\.(\d+)$/.exec(producerVersion || '')
    const legacy = !!match && Number(match[1]) === 1 && Number(match[2]) <= 1
    const caps = session.capabilities
    if (!caps) {
      return { ...session, capabilities: { send: legacy, interrupt: legacy, attachment_mode: 'unsupported' } }
    }
    return {
      ...session,
      capabilities: {
        ...caps,
        send: typeof caps.send === 'boolean' ? caps.send : legacy,
        interrupt: typeof caps.interrupt === 'boolean' ? caps.interrupt : legacy,
        approve: caps.approve === true,
        deny: caps.deny === true,
        steer: caps.steer === true,
        queue: caps.queue === true,
        spawn: caps.spawn === true,
        user_input: caps.user_input === true || (legacy && !!session.pending_user_input),
        attachment_mode: caps.attachment_mode || 'unsupported'
      }
    }
  }

  function pushInbox(deviceId: string, sessionId: string, msg: SessionMessage) {
    const key = inboxKey(deviceId, sessionId)
    const list = upsertMessageList(inbox.get(key) || [], msg)
    if (list.length) {
      inbox.set(key, list.length > 200 ? list.slice(-200) : list)
    } else {
      inbox.delete(key)
    }
  }

  /** Insert or patch message by id (streaming). */
  function upsertMessage(msg: SessionMessage) {
    messages.value = upsertMessageList(messages.value, msg)
  }

  function mergeHistory(hist: SessionMessage[]) {
    messages.value = mergeHistoryLists(messages.value, hist)
  }

  function promptFailureMessage(
    payload: {
      message?: string
      error?: string
      reason?: string
    },
    retryAllowed: boolean
  ) {
    if (!retryAllowed) {
      return tGlobal('errors.ambiguousNoRetry')
    }
    const raw = payload.message || payload.error || payload.reason || ''
    if (/already running|still running/i.test(raw)) {
      return tGlobal('errors.busyRetry')
    }
    return raw || tGlobal('errors.promptRejected')
  }

  function stopForTerminalHistoryError(hist: SessionMessage[]) {
    if (!streaming.value) return
    const latestError = hist
      .filter(message => message.type === 'error')
      .reduce<SessionMessage | null>(
        (latest, message) => !latest || message.timestamp >= latest.timestamp ? message : latest,
        null
      )
    if (!latestError) return
    const latestUserTimestamp = messages.value.reduce(
      (latest, message) => message.role === 'user'
        ? Math.max(latest, message.timestamp)
        : latest,
      0
    )
    if (latestError.timestamp <= latestUserTimestamp) return
    lastError.value = latestError.content.trim() || tGlobal('errors.agentError')
    importing.value = false
    streaming.value = false
    stopStreamPoll()
  }

  function setCurrentSession(session: AgentSession | null) {
    historyGeneration++
    const gen = historyGeneration
    stopStreamPoll()
    streaming.value = false
    currentSession.value = session
    lastError.value = null
    messages.value = []
    loading.value = false
    importing.value = false
    if (session) {
      syncOutboxFromStorage()
      if (session.device_id && activeDeviceId && session.device_id !== activeDeviceId) {
        // Refuse to open a session from another device while subscribed elsewhere.
        lastError.value = tGlobal('errors.wrongDevice')
        currentSession.value = null
        return
      }
      const did = session.device_id || activeDeviceId || ''
      // Phone-only drafts have no nest/native history until start_thread succeeds.
      if (session.id.startsWith('local_draft_')) {
        restoreOutboxMessages(did, session.id)
        return
      }
      const buffered = inbox.get(inboxKey(did, session.id))
      if (buffered?.length) {
        messages.value = mergeHistoryLists([], buffered)
        inbox.delete(inboxKey(did, session.id))
      }
      restoreOutboxMessages(did, session.id)
      void loadSessionMessages(did, session.id, gen)
    }
  }

  async function loadSessionMessages(deviceId: string, sessionId: string, gen: number) {
    loading.value = true
    try {
      const res = await apiFetch(
        `/api/messages?device_id=${encodeURIComponent(deviceId)}&session_id=${encodeURIComponent(sessionId)}&limit=200`
      )
      if (gen !== historyGeneration) return
      if (res.ok) {
        const data = await res.json()
        if (gen !== historyGeneration) return
        const history = (data.messages as SessionMessage[]) || []
        mergeHistory(history)
      } else {
        lastError.value = tGlobal('errors.historyStatus', { status: res.status })
      }
    } catch (err) {
      console.warn('[session] rest history failed:', err)
    } finally {
      if (gen === historyGeneration) loading.value = false
    }

    if (gen === historyGeneration && currentSession.value?.id === sessionId) {
      requestNativeHistory(deviceId, sessionId)
    }
  }

  function requestNativeHistory(deviceId: string, sessionId: string) {
    importing.value = true
    const ok = sendApplicationCommand('fetch_history', deviceId, sessionId, { limit: 40 })
    if (!ok) {
      importing.value = false
      return
    }
    window.setTimeout(() => {
      if (importing.value && currentSession.value?.id === sessionId) {
        importing.value = false
      }
    }, 20000)
  }

  function clearMessages() {
    messages.value = []
  }

  function markStreamActivity() {
    streaming.value = true
    if (streamIdleTimer) window.clearTimeout(streamIdleTimer)
    // Shorter idle: once frames stop, leave "replying" quickly.
    streamIdleTimer = window.setTimeout(() => {
      streaming.value = false
      stopStreamPoll()
    }, 8000)
  }

  function startStreamPoll(deviceId: string, sessionId: string) {
    stopStreamPoll()
    streaming.value = true
    let ticks = 0
    streamPollTimer = window.setInterval(() => {
      ticks++
      if (!currentSession.value || currentSession.value.id !== sessionId) {
        stopStreamPoll()
        return
      }
      // Safety net: REST + native history while waiting for WS frames
      void pollLiveHistory(deviceId, sessionId)
      if (ticks % 3 === 0) {
        requestNativeHistoryQuiet(deviceId, sessionId)
      }
      if (ticks > 90) {
        // ~3 min max
        stopStreamPoll()
        streaming.value = false
      }
    }, 2000)
    markStreamActivity()
  }

  function stopStreamPoll() {
    if (streamPollTimer) {
      window.clearInterval(streamPollTimer)
      streamPollTimer = null
    }
    if (streamIdleTimer) {
      window.clearTimeout(streamIdleTimer)
      streamIdleTimer = null
    }
  }

  async function pollLiveHistory(deviceId: string, sessionId: string) {
    try {
      const res = await apiFetch(
        `/api/messages?device_id=${encodeURIComponent(deviceId)}&session_id=${encodeURIComponent(sessionId)}&limit=100`
      )
      if (!res.ok) return
      if (currentSession.value?.id !== sessionId) return
      const data = await res.json()
      const history = (data.messages as SessionMessage[]) || []
      if (history.length) {
        mergeHistory(history)
        stopForTerminalHistoryError(history)
      }
    } catch {
      /* ignore */
    }
  }

  function requestNativeHistoryQuiet(deviceId: string, sessionId: string) {
    sendApplicationCommand('fetch_history', deviceId, sessionId, { limit: 30 })
  }

  function sendPrompt(
    deviceId: string,
    sessionId: string,
    prompt: string,
    attachments?: Array<{ id?: string; url: string; name?: string; mime?: string; size?: number }>,
    collaborationMode?: 'plan'
  ): boolean {
    lastError.value = null
	const capabilitySession = sessionForCapability(deviceId, sessionId)
    if (capabilitySession?.capabilities?.send !== true) {
      lastError.value = tGlobal('session.sendUnavailable')
      return false
    }
    const atts = attachments?.filter(a => a?.url) || []
	if (atts.length > 0 && !attachmentsAllowed(capabilitySession.capabilities?.attachment_mode, atts)) {
	  lastError.value = tGlobal('session.attachmentUnavailable')
	  return false
	}
    if (!prompt.trim() && atts.length === 0) {
      lastError.value = tGlobal('errors.emptyPrompt')
      return false
    }
    const isCurrentSessionBusy =
      currentSession.value?.id === sessionId &&
      (
        currentSession.value.status === 'running' ||
        currentSession.value.status === 'waiting_approval' ||
        currentSession.value.status === 'waiting_user' ||
        streaming.value
      )
    const queueEnabled = currentSession.value?.id === sessionId && currentSession.value.capabilities?.queue === true
    if (isCurrentSessionBusy && nekoWS().isConnected() && !queueEnabled) {
      lastError.value = tGlobal('errors.busySend')
      return false
    }
    syncOutboxFromStorage()
    if (outbox.size >= MAX_OUTBOX) {
      lastError.value = tGlobal('errors.outboxFull', { n: MAX_OUTBOX })
      return false
    }
    // Stable wire id (msg_*) — server persists it; not "optimistic-only" prefix.
    const clientMsgId = newPromptClientMsgId()
    const trimmed = prompt.trim()
    const item: OutboxItem = {
      clientMsgId,
      deviceId,
      sessionId,
      prompt: trimmed,
      attachments: atts.length ? atts : undefined,
      collaborationMode: collaborationMode === 'plan' ? 'plan' : undefined,
      status: 'queued',
      retryAllowed: true,
      createdAt: Math.floor(Date.now() / 1000)
    }
    if (!persistOutboxItem(item)) {
      lastError.value = tGlobal('errors.outboxStorage')
      return false
    }
    outbox.set(clientMsgId, item)

    const display = trimmed || tGlobal('outbox.attachmentsOnly', { n: atts.length })
    upsertMessage({
      id: clientMsgId,
      role: 'user',
      content: display,
      type: 'text',
      timestamp: item.createdAt,
      metadata: {
        ...(atts.length ? { attachments: atts } : {}),
        delivery_status: 'queued'
      }
    })
    const sent = sendOutboxItem(item)
    if (!sent) {
      lastError.value = tGlobal('errors.channelQueued')
    }
    return true
  }

  function isPending(clientMsgId: string) {
    syncOutboxFromStorage()
    return outbox.has(clientMsgId)
  }

  function retryPrompt(clientMsgId: string): boolean {
    syncOutboxFromStorage()
    const item = outbox.get(clientMsgId)
    if (!item || item.status !== 'failed') return false
    if (!item.retryAllowed) {
      lastError.value = tGlobal('errors.ambiguousNoRetry')
      return false
    }
    if (!nekoWS().isConnected()) {
      lastError.value = tGlobal('errors.channelRetry')
      return false
    }
    const capabilitySession = sessionForCapability(item.deviceId, item.sessionId)
    if (capabilitySession?.capabilities?.send !== true) {
      lastError.value = tGlobal('session.sendUnavailable')
      return false
    }
    if (!attachmentsAllowed(capabilitySession.capabilities?.attachment_mode, item.attachments || [])) {
      lastError.value = tGlobal('session.attachmentUnavailable')
      return false
    }
    // A definitive retry is a new command. One client_msg_id always keeps one
    // immutable envelope; the failed ID remains terminal in server durability.
    const retryClientMsgId = newPromptClientMsgId()
    const retryItem: OutboxItem = {
      ...item,
      clientMsgId: retryClientMsgId,
      status: 'queued',
      retryAllowed: true,
      createdAt: Math.floor(Date.now() / 1000),
      sealedWire: undefined
    }
    delete retryItem.error
    clearAckTimer(clientMsgId)
    sealingPromises.delete(clientMsgId)
    removePersistedOutboxItem(clientMsgId)
    outbox.delete(clientMsgId)
    outbox.set(retryClientMsgId, retryItem)
    lastError.value = null
    const delivery = messages.value.find(message => message.id === clientMsgId)
    if (delivery) delivery.id = retryClientMsgId
    patchDeliveryMessage(retryClientMsgId, 'queued')
    persistOutboxItem(retryItem)
    const sent = sendOutboxItem(retryItem)
    if (!sent) {
      const failure = tGlobal('errors.channelDropped')
      retryItem.status = 'failed'
      retryItem.error = failure
      persistOutboxItem(retryItem)
      patchDeliveryMessage(retryClientMsgId, 'failed', failure)
      lastError.value = failure
    }
    return sent
  }

  function approve(deviceId: string, sessionId: string, approvalId: string): boolean {
	if (!hasSessionCapability(deviceId, sessionId, 'approve')) {
	  lastError.value = tGlobal('session.controlUnavailable')
	  return false
	}
    const ok = sendApplicationCommand('approve', deviceId, sessionId, { approval_id: approvalId })
    if (!ok) lastError.value = tGlobal('errors.channelApprove')
    return ok
  }

  function deny(deviceId: string, sessionId: string, approvalId: string): boolean {
	if (!hasSessionCapability(deviceId, sessionId, 'deny')) {
	  lastError.value = tGlobal('session.controlUnavailable')
	  return false
	}
    const ok = sendApplicationCommand('deny', deviceId, sessionId, { approval_id: approvalId })
    if (!ok) lastError.value = tGlobal('errors.channelReject')
    return ok
  }

  function newPromptClientMsgId(): string {
    return `msg_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 10)}`
  }

  function sendApplicationCommand(
    type: MessageType,
    deviceId: string,
    sessionId: string,
    payload: Record<string, unknown>,
    clientMsgId?: string
  ): boolean {
    const timestamp = Math.floor(Date.now() / 1000)
    // Queue and interrupt controls carry the target prompt id in their body.
    // Bind that same id into the outer envelope/AAD in sealed mode; the daemon
    // rejects an unbound inner client_msg_id to prevent target substitution.
    const payloadClientMsgId = typeof payload.client_msg_id === 'string'
      ? payload.client_msg_id.trim()
      : ''
    const wireClientMsgId = clientMsgId?.trim() || payloadClientMsgId || undefined
    if (nestTransportMode() === 'open') {
      return nekoWS().send({
        type, device_id: deviceId, session_id: sessionId,
        client_msg_id: wireClientMsgId, timestamp, payload
      })
    }
    void (async () => {
      let sealed: Awaited<ReturnType<typeof encryptSessionPayload>> = null
      try {
        sealed = await encryptSessionPayload(
          deviceId, sessionId, getPhoneId() || 'phone', type, payload,
          wireClientMsgId, timestamp, nekoWS().getProtocolVersion()
        )
      } catch {
        // Fall through to the fail-closed error below.
      }
      if (!sealed) {
        lastError.value = tGlobal('session.queueKeyUnavailable')
        return
      }
      if (!nekoWS().send({
        type, device_id: deviceId, session_id: sessionId,
        client_msg_id: wireClientMsgId, timestamp, sealed_payload: sealed
      })) lastError.value = tGlobal('errors.channelQueued')
    })()
    return true
  }

  async function respondUserInput(
	deviceId: string,
	sessionId: string,
	pending: PendingUserInput,
	answers: Record<string, string[]>
  ): Promise<boolean> {
	if (!hasSessionCapability(deviceId, sessionId, 'user_input')) {
	  lastError.value = tGlobal('session.controlUnavailable')
	  return false
	}
	const payload: Record<string, unknown> = { request_id: pending.request_id, answers }
	const timestamp = Math.floor(Date.now() / 1000)
	if (nestTransportMode() === 'sealed') {
	  let sealed: Awaited<ReturnType<typeof encryptSessionPayload>> = null
	  try {
		sealed = await encryptSessionPayload(
		  deviceId, sessionId, getPhoneId() || 'phone', 'respond_user_input', payload,
		  undefined, timestamp, nekoWS().getProtocolVersion()
		)
	  } catch {
		// Treat local crypto failures as definitive so the form can be retried.
	  }
	  if (!sealed) {
		lastError.value = tGlobal('session.userInputKeyUnavailable')
		return false
	  }
	  const sent = nekoWS().send({
		type: 'respond_user_input', device_id: deviceId, session_id: sessionId,
		timestamp, sealed_payload: sealed
	  })
	  if (!sent) lastError.value = tGlobal('errors.channelQueued')
	  return sent
	}
	const sent = nekoWS().send({
	  type: 'respond_user_input', device_id: deviceId, session_id: sessionId,
	  timestamp, payload
	})
	if (!sent) lastError.value = tGlobal('errors.channelQueued')
	return sent
  }

  function interrupt(deviceId: string, sessionId: string): boolean {
	const session = sessionForCapability(deviceId, sessionId)
	const binding = session?.active_turn
	if (!canInterrupt(deviceId, sessionId)) {
	  lastError.value = tGlobal('session.controlUnavailable')
	  return false
	}
	const payload = binding && validActiveTurnBinding(binding)
	  ? { generation: binding.generation, client_msg_id: binding.client_msg_id }
	  : {}
	const ok = sendApplicationCommand('interrupt', deviceId, sessionId, payload)
    if (!ok) lastError.value = tGlobal('errors.channelInterrupt')
    return ok
  }

  function steer(deviceId: string, sessionId: string, text: string): boolean {
    lastError.value = null
    const trimmed = text.trim()
    if (!trimmed) {
      lastError.value = tGlobal('errors.emptySteer')
      return false
    }
	if (!hasSessionCapability(deviceId, sessionId, 'steer')) {
	  lastError.value = tGlobal('session.controlUnavailable')
	  return false
	}
    const ok = sendApplicationCommand('steer', deviceId, sessionId, { text: trimmed })
    if (!ok) lastError.value = tGlobal('errors.channelSteer')
    return ok
  }

  function sendQueueControl(
    type: 'cancel_prompt' | 'resume_prompt_queue' | 'skip_prompt_queue_item',
    deviceId: string,
    sessionId: string,
    payload: Record<string, unknown>
  ): boolean {
	if (!hasSessionCapability(deviceId, sessionId, 'queue')) {
	  lastError.value = tGlobal('session.controlUnavailable')
	  return false
	}
    return sendApplicationCommand(type, deviceId, sessionId, payload)
  }

  function cancelPrompt(deviceId: string, sessionId: string, clientMsgId: string): boolean {
    const cid = clientMsgId.trim()
    if (!cid) return false
    const ok = sendQueueControl('cancel_prompt', deviceId, sessionId, { client_msg_id: cid })
    if (!ok) lastError.value = tGlobal('errors.channelQueueControl')
    return ok
  }

  function resumePromptQueue(deviceId: string, sessionId: string): boolean {
    const ok = sendQueueControl('resume_prompt_queue', deviceId, sessionId, {})
    if (!ok) lastError.value = tGlobal('errors.channelQueueControl')
    return ok
  }

  function skipPromptQueueItem(deviceId: string, sessionId: string, clientMsgId: string): boolean {
    const cid = clientMsgId.trim()
    if (!cid) return false
    const ok = sendQueueControl('skip_prompt_queue_item', deviceId, sessionId, { client_msg_id: cid })
    if (!ok) lastError.value = tGlobal('errors.channelQueueControl')
    return ok
  }

  /** Pending native start_thread operations keyed by operation_id. */
  const startOps = ref<
    Record<
      string,
      {
        deviceId: string
        agentType: AgentType
        cwd: string
        firstPrompt?: string
        status: 'starting' | 'owned' | 'failed' | 'indeterminate'
        sessionId?: string
        promptAccepted?: boolean
        error?: string
      }
    >
  >({})

  function startThread(
    deviceId: string,
    agentType: AgentType,
    cwd: string,
    firstPrompt = '',
    boundOperationId = '',
    attachments?: AttachmentRef[]
  ): { ok: boolean; operationId: string } {
    const operationId = boundOperationId.trim() || `local_start_${Date.now()}_${Math.random().toString(36).slice(2, 8)}`
	const projectDir = cwd.trim()
	const legacyCodexSession = startCapabilities.value === null && agentType === 'codex'
	  ? [currentSession.value, ...sessions.value].find(session =>
	      session?.device_id === deviceId &&
	      session.agent_type === 'codex' &&
	      session.capabilities?.spawn === true
	    )
	  : undefined
	const capability: AgentStartCapability | undefined =
	  startCapabilities.value?.find(item => item.agent_type === agentType) ??
	  (legacyCodexSession
	    ? {
	        agent_type: 'codex',
	        available: true,
	        spawn: true,
	        attachment_mode: legacyCodexSession.capabilities?.attachment_mode
	      }
	    : undefined)
	const refs = attachments?.filter(item => item?.url) || []
	if (!projectDir || !capability || capability.available !== true || capability.spawn !== true ||
	  !attachmentsAllowed(capability.attachment_mode, refs)) {
	  lastError.value = refs.length > 0 && capability?.attachment_mode === 'unsupported'
	    ? tGlobal('session.attachmentUnavailable')
	    : tGlobal('deviceDetail.startThreadUnavailable')
	  return { ok: false, operationId }
	}
    const firstPromptText = firstPrompt.trim()
    startOps.value = {
      ...startOps.value,
      [operationId]: {
		deviceId, agentType, cwd: projectDir, firstPrompt: firstPromptText, status: 'starting'
      }
    }
    const timestamp = Math.floor(Date.now() / 1000)
    const payload = {
      operation_id: operationId,
		project_dir: projectDir,
      agent_type: agentType,
      prompt: firstPrompt,
      ...(refs.length ? { attachments: refs } : {})
    }
    if (nestTransportMode() === 'sealed') {
      void (async () => {
        let sealed: Awaited<ReturnType<typeof encryptSessionPayload>> = null
        try {
          sealed = await encryptSessionPayload(
            deviceId,
            '',
            getPhoneId() || 'phone',
            'start_thread',
            payload,
            operationId,
            timestamp,
            nekoWS().getProtocolVersion()
          )
        } catch {
          // No ciphertext was produced or sent; this is a definitive local failure.
        }
        const sent = sealed
          ? nekoWS().send({
              type: 'start_thread',
              device_id: deviceId,
              client_msg_id: operationId,
              timestamp,
              sealed_payload: sealed
            })
          : false
        if (!sent) {
          startOps.value = {
            ...startOps.value,
            [operationId]: {
              deviceId,
              agentType,
              cwd,
              firstPrompt: firstPromptText,
              status: 'failed',
              error: sealed ? tGlobal('errors.channelDropped') : tGlobal('errors.sealedKeyMissing')
            }
          }
          lastError.value = sealed ? tGlobal('errors.channelDropped') : tGlobal('errors.sealedKeyMissing')
        }
      })()
      return { ok: true, operationId }
    }

    const ok = nekoWS().send({
      type: 'start_thread',
      device_id: deviceId,
      client_msg_id: operationId,
      timestamp,
      payload
    })
    if (!ok) {
      startOps.value = {
        ...startOps.value,
        [operationId]: {
          deviceId,
          agentType,
          cwd,
          firstPrompt: firstPromptText,
          status: 'failed',
          error: tGlobal('errors.channelDropped')
        }
      }
      lastError.value = tGlobal('errors.channelDropped')
    }
    return { ok, operationId }
  }

	function sessionForCapability(deviceId: string, sessionId: string): AgentSession | undefined {
	  if (currentSession.value?.id === sessionId && currentSession.value.device_id === deviceId) {
	    return currentSession.value
	  }
	  return sessions.value.find(session => session.id === sessionId && session.device_id === deviceId)
	}

	function hasSessionCapability(
	  deviceId: string,
	  sessionId: string,
	  capability: 'approve' | 'deny' | 'interrupt' | 'steer' | 'queue' | 'user_input'
	): boolean {
	  return sessionForCapability(deviceId, sessionId)?.capabilities?.[capability] === true
	}

	function validActiveTurnBinding(binding: AgentSession['active_turn']): boolean {
	  return !!binding && Number.isSafeInteger(binding.generation) && binding.generation > 0 &&
	    !!binding.client_msg_id?.trim()
	}

	function legacyCatalogProducer(): boolean {
	  const match = /^(\d+)\.(\d+)$/.exec(catalogProducerVersion.value || '')
	  return !!match && Number(match[1]) === 1 && Number(match[2]) <= 1
	}

	function canInterrupt(deviceId: string, sessionId: string): boolean {
	  const session = sessionForCapability(deviceId, sessionId)
	  return session?.capabilities?.interrupt === true &&
	    (validActiveTurnBinding(session.active_turn) || legacyCatalogProducer())
	}

	function attachmentsAllowed(
	  mode: AttachmentMode | undefined,
	  attachments: Array<{ mime?: string }>
	): boolean {
	  if (attachments.length === 0) return true
	  if (mode === 'native_image_and_file' || mode === 'path_best_effort') return true
	  return mode === 'native_image' && attachments.every(item => item.mime?.toLowerCase().startsWith('image/'))
	}

  function applyStartThreadResult(
    type: string,
    payload: Record<string, unknown> | undefined,
    deviceId: string
  ) {
    const opId = String(payload?.operation_id || '').trim()
    if (!opId) return
    const prev = startOps.value[opId]
    if (!prev || prev.deviceId !== deviceId) {
      // Still record if we initiated from this phone earlier this session.
      if (!prev) return
    }
    const ownedSession = payload?.session as Partial<AgentSession> | undefined
    const sessionId = String(
      payload?.session_id || payload?.thread_id || ownedSession?.id || ''
    ).trim()
    const error = String(payload?.error || payload?.message || payload?.reason || '').trim()
    const promptAccepted = payload?.prompt_accepted === true
    let status: 'starting' | 'owned' | 'failed' | 'indeterminate' = 'starting'
    if (type === 'thread_owned') {
      // Enforce the v1 invariant even when an older daemon emits an invalid
      // owned result without both required pieces of positive evidence.
      status = promptAccepted && sessionId ? 'owned' : 'indeterminate'
    }
    else if (type === 'thread_failed') status = 'failed'
    else if (type === 'thread_indeterminate') status = 'indeterminate'
    else if (type === 'thread_starting') status = 'starting'
    startOps.value = {
      ...startOps.value,
      [opId]: {
        deviceId,
        agentType: prev?.agentType || String(payload?.agent_type || ownedSession?.agent_type || 'unknown'),
        cwd: prev?.cwd || String(payload?.cwd || ''),
        firstPrompt: prev?.firstPrompt,
        status,
        sessionId: sessionId || undefined,
        promptAccepted,
        error: error || undefined
      }
    }
    // Only thread_owned may surface the first prompt as owned-thread history.
    // thread_indeterminate must not synthesize a native session bubble/inbox row.
    if (status === 'owned' && promptAccepted && sessionId && prev?.firstPrompt) {
      const message: SessionMessage = {
        id: `msg_${opId}`,
        role: 'user',
        content: prev.firstPrompt,
        type: 'text',
        timestamp: Math.floor(Date.now() / 1000)
      }
      if (currentSession.value?.id === sessionId) {
        upsertMessage(message)
      } else {
        pushInbox(deviceId, sessionId, message)
      }
    }
    if (status === 'failed' && error) {
      lastError.value = error
    }
  }

  function clearStartOp(operationId: string) {
    const next = { ...startOps.value }
    delete next[operationId]
    startOps.value = next
  }

  function cleanup() {
    stopStreamPoll()
    clearAllAckTimers()
    if (typeof window !== 'undefined') {
      window.removeEventListener('storage', handleOutboxStorage)
    }
    nekoWS().removeHandler(HANDLER_ID)
    nekoWS().removeStatusHandler(HANDLER_ID)
  }

  return {
    sessions, startCapabilities, catalogStatus, catalogDeviceId, catalogProducerVersion, currentSession, currentSessionCatalogVisible, messages, loading, importing, streaming, lastError, lastUserInputResult, wsStatus,
    promptQueues, currentPromptQueue,
    startOps,
    subscribeDevice, applySessionList, setCurrentSession, clearMessages, requestNativeHistory,
    sendPrompt, retryPrompt, approve, deny, respondUserInput, canInterrupt, interrupt, steer, cancelPrompt, resumePromptQueue, skipPromptQueueItem, startThread, clearStartOp,
    isPending, cleanup
  }
})
