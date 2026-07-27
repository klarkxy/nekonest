import { ref } from 'vue'
import { defineStore } from 'pinia'
import type { AgentSession, SessionMessage } from '@/types/protocol'
import { nekoWS } from '@/api/websocket'
import { apiFetch } from '@/api/http'
import { mergeHistoryLists, upsertMessageList } from '@/utils/messageMerge'

export const MAX_OUTBOX = 40
export const PROMPT_ACK_TIMEOUT_MS = 15_000
export const LEGACY_OUTBOX_STORAGE_KEY = 'nekonest_prompt_outbox'
export const OUTBOX_STORAGE_KEY_PREFIX = 'nekonest_prompt_outbox:item:'

export const useSessionStore = defineStore('sessions', () => {
  const sessions = ref<AgentSession[]>([])
  const currentSession = ref<AgentSession | null>(null)
  const messages = ref<SessionMessage[]>([])
  const loading = ref(false)
  const importing = ref(false)
  const streaming = ref(false)
  const lastError = ref<string | null>(null)
  const wsStatus = ref<'connecting' | 'connected' | 'disconnected' | 'auth_error'>('disconnected')

  const HANDLER_ID = 'session-store'
  let historyGeneration = 0
  let streamPollTimer: number | null = null
  let streamIdleTimer: number | null = null
  let activeDeviceId: string | null = null
  /** Buffered live messages: key = deviceId::sessionId */
  const inbox = new Map<string, SessionMessage[]>()
  /** Unacked send_prompt outbox keyed by client_msg_id (stable across reconnect). */
  type OutboxItem = {
    clientMsgId: string
    deviceId: string
    sessionId: string
    prompt: string
    attachments?: Array<{ id?: string; url: string; name?: string; mime?: string; size?: number }>
    status: 'queued' | 'sending' | 'failed'
    error?: string
    retryAllowed: boolean
    createdAt: number
  }
  const outbox = new Map<string, OutboxItem>()
  const ackTimers = new Map<string, number>()

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
        : Math.floor(Date.now() / 1000)
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
    explicitRetry = false,
    scheduleAck = true
  ): boolean {
    const payload: Record<string, unknown> = {
      prompt: it.prompt,
      client_msg_id: it.clientMsgId
    }
    if (it.attachments?.length) payload.attachments = it.attachments
    if (explicitRetry) payload.retry = true
    const sent = nekoWS().send({
      type: 'send_prompt',
      device_id: it.deviceId,
      session_id: it.sessionId,
      timestamp: Math.floor(Date.now() / 1000),
      payload
    })
    it.status = sent ? 'sending' : 'queued'
    delete it.error
    if (!persistOutboxItem(it)) {
      lastError.value = '待发送消息状态无法保存，请勿关闭页面'
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
    status?: 'queued' | 'sending' | 'failed',
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
    if (status === 'failed') metadata.delivery_retry_allowed = retryAllowed
    else delete metadata.delivery_retry_allowed
    messages.value[idx] = { ...prev, metadata }
    messages.value = [...messages.value]
  }

  function restoreOutboxMessages(deviceId: string, sessionId: string) {
    for (const it of outbox.values()) {
      if (it.deviceId !== deviceId || it.sessionId !== sessionId) continue
      const display = it.prompt || `(发送了 ${it.attachments?.length || 0} 个附件)`
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
      if (currentSession.value && currentSession.value.device_id !== deviceId) {
        currentSession.value = null
        messages.value = []
        importing.value = false
        streaming.value = false
        stopStreamPoll()
      }
      activeDeviceId = deviceId
    }
    ws.subscribe(deviceId)

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
      if (msg.type === 'device_online') {
        // A pending status query can go unanswered while the daemon is offline
        // even though the phone socket stays connected. Query again on return.
        flushOutbox()
      } else if (msg.type === 'session_list' && msg.device_id === deviceId) {
        sessions.value = ((msg.payload?.sessions as AgentSession[]) || []).filter(
          s => !s.device_id || s.device_id === deviceId
        )
        if (currentSession.value) {
          if (currentSession.value.device_id && currentSession.value.device_id !== deviceId) {
            currentSession.value = null
            messages.value = []
          } else {
            const updated = sessions.value.find(s => s.id === currentSession.value!.id)
            if (updated) currentSession.value = updated
          }
        }
      } else if (msg.type === 'session_update') {
        const raw = msg.payload?.session ?? msg.payload
        const updated = raw as AgentSession | undefined
        if (!updated?.id) return
        if (updated.device_id && updated.device_id !== deviceId) return
        const idx = sessions.value.findIndex(s => s.id === updated.id)
        if (idx >= 0) {
          sessions.value[idx] = { ...sessions.value[idx], ...updated }
          if (currentSession.value?.id === updated.id) {
            currentSession.value = sessions.value[idx]
          }
        } else {
          sessions.value.push({ ...updated, device_id: updated.device_id || deviceId })
        }
      } else if (msg.type === 'session_message') {
        const sessionMessage = msg.payload?.message as SessionMessage
        if (!sessionMessage) return
        const sid = msg.session_id || (sessionMessage as { session_id?: string }).session_id
        if (!sid) return
        const did = msg.device_id || deviceId
        if (
          currentSession.value?.id === sid &&
          (!currentSession.value.device_id || currentSession.value.device_id === did)
        ) {
          upsertMessage(sessionMessage)
          markStreamActivity()
        } else {
          pushInbox(did, sid, sessionMessage)
        }
      } else if (msg.type === 'session_history') {
        if (!currentSession.value || !msg.session_id || msg.session_id !== currentSession.value.id) return
        importing.value = false
        const hist = (msg.payload?.messages as SessionMessage[]) || []
        mergeHistory(hist)
      } else if (msg.type === 'prompt_sent') {
        const p = msg.payload as {
          prompt?: string
          attachments?: unknown[]
          message_id?: string
          client_msg_id?: string
        }
        const cid = (p?.client_msg_id || '').trim()
        const stableId = (p?.message_id || cid || '').trim()
        if (cid) {
          clearAckTimer(cid)
          outbox.delete(cid)
          removePersistedOutboxItem(cid)
        }
        if (!currentSession.value || !msg.session_id || msg.session_id !== currentSession.value.id) return
        const prompt = p?.prompt || ''
        const atts = p?.attachments
        // Match only by stable id — never content-only (multi-phone / "继续" collisions).
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
      } else if (msg.type === 'prompt_failed') {
        const p = msg.payload as {
          client_msg_id?: string
          message?: string
          error?: string
          reason?: string
          outcome?: string
          retry_allowed?: boolean
        }
        const cid = (p?.client_msg_id || '').trim()
        const retryAllowed = p?.retry_allowed !== false && p?.outcome !== 'indeterminate'
        const failure = retryAllowed
          ? p?.message || p?.error || p?.reason || 'Agent 未接受该指令'
          : '执行结果不确定，为避免重复已禁用重试'
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
  }

  function pushInbox(deviceId: string, sessionId: string, msg: SessionMessage) {
    const key = inboxKey(deviceId, sessionId)
    const list = inbox.get(key) || []
    const i = list.findIndex(m => m.id === msg.id)
    if (i >= 0) list[i] = { ...list[i], ...msg }
    else list.push(msg)
    if (list.length > 200) list.splice(0, list.length - 200)
    inbox.set(key, list)
  }

  /** Insert or patch message by id (streaming). */
  function upsertMessage(msg: SessionMessage) {
    messages.value = upsertMessageList(messages.value, msg)
  }

  function mergeHistory(hist: SessionMessage[]) {
    messages.value = mergeHistoryLists(messages.value, hist)
  }

  function setCurrentSession(session: AgentSession | null) {
    historyGeneration++
    const gen = historyGeneration
    stopStreamPoll()
    streaming.value = false
    currentSession.value = session
    lastError.value = null
    messages.value = []
    importing.value = false
    if (session) {
      syncOutboxFromStorage()
      if (session.device_id && activeDeviceId && session.device_id !== activeDeviceId) {
        // Refuse to open a session from another device while subscribed elsewhere.
        lastError.value = '会话属于其他设备，请先切换设备'
        currentSession.value = null
        return
      }
      const did = session.device_id || activeDeviceId || ''
      const buffered = inbox.get(inboxKey(did, session.id))
      if (buffered?.length) {
        messages.value = [...buffered]
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
        lastError.value = `加载历史失败 (${res.status})`
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
    const ok = nekoWS().send({
      type: 'fetch_history',
      device_id: deviceId,
      session_id: sessionId,
      timestamp: Math.floor(Date.now() / 1000),
      payload: { limit: 50 }
    })
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
    streamIdleTimer = window.setTimeout(() => {
      streaming.value = false
      stopStreamPoll()
    }, 25000)
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
      if (history.length) mergeHistory(history)
    } catch {
      /* ignore */
    }
  }

  function requestNativeHistoryQuiet(deviceId: string, sessionId: string) {
    nekoWS().send({
      type: 'fetch_history',
      device_id: deviceId,
      session_id: sessionId,
      timestamp: Math.floor(Date.now() / 1000),
      payload: { limit: 30 }
    })
  }

  function sendPrompt(
    deviceId: string,
    sessionId: string,
    prompt: string,
    attachments?: Array<{ id?: string; url: string; name?: string; mime?: string; size?: number }>
  ): boolean {
    lastError.value = null
    const atts = attachments?.filter(a => a?.url) || []
    if (!prompt.trim() && atts.length === 0) {
      lastError.value = '请输入内容或添加附件'
      return false
    }
    syncOutboxFromStorage()
    if (outbox.size >= MAX_OUTBOX) {
      lastError.value = `待发送消息已达上限（${MAX_OUTBOX} 条），请等待现有消息确认后再发送`
      return false
    }
    // Stable wire id (msg_*) — server persists it; not "optimistic-only" prefix.
    const clientMsgId = `msg_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 10)}`
    const trimmed = prompt.trim()
    const item: OutboxItem = {
      clientMsgId,
      deviceId,
      sessionId,
      prompt: trimmed,
      attachments: atts.length ? atts : undefined,
      status: 'queued',
      retryAllowed: true,
      createdAt: Math.floor(Date.now() / 1000)
    }
    if (!persistOutboxItem(item)) {
      lastError.value = '无法安全保存待发送消息，请检查浏览器存储空间后重试'
      return false
    }
    outbox.set(clientMsgId, item)

    const display = trimmed || `(发送了 ${atts.length} 个附件)`
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
      lastError.value = '未连接，消息已排队，恢复连接后自动发送'
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
      lastError.value = '执行结果不确定，为避免重复已禁用重试'
      return false
    }
    if (!nekoWS().isConnected()) {
      lastError.value = '未连接，请恢复连接后再重试'
      return false
    }
    item.status = 'queued'
    item.retryAllowed = true
    delete item.error
    lastError.value = null
    patchDeliveryMessage(clientMsgId, 'queued')
    persistOutboxItem(item)
    const sent = sendOutboxItem(item, true)
    if (!sent) {
      const failure = '连接已中断，请恢复连接后再次重试'
      item.status = 'failed'
      item.error = failure
      persistOutboxItem(item)
      patchDeliveryMessage(clientMsgId, 'failed', failure)
      lastError.value = failure
    }
    return sent
  }

  function approve(deviceId: string, sessionId: string, approvalId: string): boolean {
    const ok = nekoWS().send({
      type: 'approve',
      device_id: deviceId,
      session_id: sessionId,
      timestamp: Math.floor(Date.now() / 1000),
      payload: { approval_id: approvalId }
    })
    if (!ok) lastError.value = '未连接，无法发送审批'
    return ok
  }

  function deny(deviceId: string, sessionId: string, approvalId: string): boolean {
    const ok = nekoWS().send({
      type: 'deny',
      device_id: deviceId,
      session_id: sessionId,
      timestamp: Math.floor(Date.now() / 1000),
      payload: { approval_id: approvalId }
    })
    if (!ok) lastError.value = '未连接，无法发送拒绝'
    return ok
  }

  function interrupt(deviceId: string, sessionId: string): boolean {
    const ok = nekoWS().send({
      type: 'interrupt',
      device_id: deviceId,
      session_id: sessionId,
      timestamp: Math.floor(Date.now() / 1000),
      payload: {}
    })
    if (!ok) lastError.value = '未连接，无法中断'
    return ok
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
    sessions, currentSession, messages, loading, importing, streaming, lastError, wsStatus,
    subscribeDevice, setCurrentSession, clearMessages, requestNativeHistory,
    sendPrompt, retryPrompt, approve, deny, interrupt, isPending, cleanup
  }
})
