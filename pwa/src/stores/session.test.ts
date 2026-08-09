import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { tGlobal } from '@/i18n'
import type { NekoMessage } from '@/types/protocol'
import { resetTransportModeForTests } from '@/api/transport'
import { apiFetch } from '@/api/http'

const harness = vi.hoisted(() => ({
  connected: false,
  status: 'disconnected' as 'connecting' | 'connected' | 'disconnected' | 'auth_error',
  subscribedDevice: null as string | null,
  decrypted: null as unknown,
  encrypted: null as NonNullable<NekoMessage['sealed_payload']> | null,
  sent: [] as Array<Partial<NekoMessage>>,
  handlers: new Map<string, (msg: NekoMessage) => void>(),
  statusHandlers: new Map<string, (status: 'connecting' | 'connected' | 'disconnected' | 'auth_error') => void>()
}))

vi.mock('@/api/websocket', () => {
  const socket = {
    subscribe(deviceId: string) {
      harness.subscribedDevice = deviceId
    },
    send(msg: Partial<NekoMessage>) {
      if (!harness.connected) return false
      harness.sent.push(structuredClone(msg))
      return true
    },
    isConnected() {
      return harness.connected
    },
    whenReady(cb: () => void) {
      if (harness.connected) cb()
    },
    addHandler(id: string, handler: (msg: NekoMessage) => void) {
      harness.handlers.set(id, handler)
    },
    removeHandler(id: string) {
      harness.handlers.delete(id)
    },
    onStatusChange(
      id: string,
      handler: (status: 'connecting' | 'connected' | 'disconnected' | 'auth_error') => void
    ) {
      harness.statusHandlers.set(id, handler)
      handler(harness.status)
    },
    removeStatusHandler(id: string) {
      harness.statusHandlers.delete(id)
    }
  }
  return { nekoWS: () => socket }
})

vi.mock('@/api/http', () => ({
  apiFetch: vi.fn(async () => new Response('', { status: 503 })),
  getPhoneId: vi.fn(() => 'phone-test')
}))

vi.mock('@/crypto/keys', async () => {
  const actual = await vi.importActual<typeof import('@/crypto/keys')>('@/crypto/keys')
  return {
    ...actual,
    decryptSealedPayload: vi.fn(async () => harness.decrypted),
    encryptSessionPayload: vi.fn(async () => harness.encrypted)
  }
})

import {
  LEGACY_OUTBOX_STORAGE_KEY,
  MAX_OUTBOX,
  OUTBOX_STORAGE_KEY_PREFIX,
  PROMPT_ACK_TIMEOUT_MS,
  useSessionStore
} from './session'
import { encryptSessionPayload } from '@/crypto/keys'

function emit(msg: NekoMessage) {
  for (const handler of harness.handlers.values()) handler(msg)
}

function setConnected(connected: boolean) {
  harness.connected = connected
  harness.status = connected ? 'connected' : 'disconnected'
  for (const handler of harness.statusHandlers.values()) handler(harness.status)
}

function sentPrompts() {
  return harness.sent.filter(message => message.type === 'send_prompt')
}

function persistedOutboxItems() {
  const items: Array<Record<string, unknown>> = []
  for (let index = 0; index < localStorage.length; index++) {
    const key = localStorage.key(index)
    if (!key?.startsWith(OUTBOX_STORAGE_KEY_PREFIX)) continue
    const raw = localStorage.getItem(key)
    if (raw) items.push(JSON.parse(raw))
  }
  return items
}

describe('session prompt outbox', () => {
  beforeEach(() => {
    resetTransportModeForTests('open')
    vi.useFakeTimers()
    harness.decrypted = null
    harness.encrypted = null
    vi.mocked(encryptSessionPayload).mockClear()
    vi.mocked(encryptSessionPayload).mockImplementation(async () => harness.encrypted)
    localStorage.clear()
    harness.connected = false
    harness.status = 'disconnected'
    harness.subscribedDevice = null
    harness.sent = []
    harness.handlers.clear()
    harness.statusHandlers.clear()
    setActivePinia(createPinia())
  })

  afterEach(() => {
    resetTransportModeForTests()
    vi.clearAllTimers()
    vi.useRealTimers()
    vi.restoreAllMocks()
    vi.unstubAllEnvs()
  })

  it('keeps an offline prompt as one optimistic queued message', () => {
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.currentSession = {
      id: 'session-a',
      device_id: 'device-a',
      agent_type: 'codex',
      status: 'idle',
      summary: '',
      last_activity: 0
    }

    expect(store.sendPrompt('device-a', 'session-a', '继续')).toBe(true)
    expect(store.messages).toHaveLength(1)
    expect(store.messages[0].content).toBe('继续')
    expect(store.messages[0].metadata?.delivery_status).toBe('queued')
    expect(store.isPending(store.messages[0].id)).toBe(true)
    expect(harness.sent).toHaveLength(0)
  })

  it('prevents a second prompt while the connected session is still running', () => {
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.currentSession = {
      id: 'session-a',
      device_id: 'device-a',
      agent_type: 'codex',
      status: 'running',
      summary: '',
      last_activity: 0
    }

    expect(store.sendPrompt('device-a', 'session-a', '再来一条')).toBe(false)
    expect(store.lastError).toBe(tGlobal('errors.busySend'))
    expect(store.messages).toHaveLength(0)
    expect(harness.sent).toHaveLength(0)
    store.cleanup()
  })

  it.each(['waiting_approval', 'waiting_user'] as const)(
    'prevents a new prompt while the session is %s',
    status => {
      setConnected(true)
      const store = useSessionStore()
      store.subscribeDevice('device-a')
      store.currentSession = {
        id: 'session-a',
        device_id: 'device-a',
        agent_type: 'codex',
        status,
        summary: '',
        last_activity: 0
      }

      expect(store.sendPrompt('device-a', 'session-a', 'do not overlap')).toBe(false)
      expect(store.lastError).toBe(tGlobal('errors.busySend'))
      expect(harness.sent).toHaveLength(0)
      store.cleanup()
    }
  )

  it('sends a trimmed steer command without adding it to the prompt outbox', () => {
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')

    expect(store.steer('device-a', 'session-a', '  change direction  ')).toBe(true)
    expect(harness.sent).toHaveLength(1)
    expect(harness.sent[0]).toMatchObject({
      type: 'steer',
      device_id: 'device-a',
      session_id: 'session-a',
      payload: { text: 'change direction' }
    })
    expect(store.messages).toHaveLength(0)
    expect(persistedOutboxItems()).toHaveLength(0)
    store.cleanup()
  })

  it('rejects an empty steer command before touching the channel', () => {
    setConnected(true)
    const store = useSessionStore()

    expect(store.steer('device-a', 'session-a', '   ')).toBe(false)
    expect(store.lastError).toBe(tGlobal('errors.emptySteer'))
    expect(harness.sent).toHaveLength(0)
    store.cleanup()
  })

  it('still queues a prompt offline when the last known session status is running', () => {
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.currentSession = {
      id: 'session-a',
      device_id: 'device-a',
      agent_type: 'codex',
      status: 'running',
      summary: '',
      last_activity: 0
    }

    expect(store.sendPrompt('device-a', 'session-a', '回来后继续')).toBe(true)
    expect(store.messages[0].metadata?.delivery_status).toBe('queued')
    expect(harness.sent).toHaveLength(0)
    store.cleanup()
  })

  it('freezes a failed prompt and explicitly retries with a new client id', () => {
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.currentSession = {
      id: 'session-a',
      device_id: 'device-a',
      agent_type: 'codex',
      status: 'idle',
      summary: '',
      last_activity: 0
    }
    store.sendPrompt('device-a', 'session-a', '修复它')
    const clientMsgId = store.messages[0].id

    emit({
      type: 'prompt_failed',
      device_id: 'device-a',
      session_id: 'session-a',
      timestamp: 1,
      payload: { client_msg_id: clientMsgId, message: '启动失败' }
    })
    expect(store.messages[0].metadata?.delivery_status).toBe('failed')
    expect(store.messages[0].metadata?.delivery_error).toBe('启动失败')
    expect(store.messages[0].metadata?.delivery_retry_allowed).toBe(true)

    // Reconnecting must not automatically resend failed items.
    expect(store.retryPrompt(clientMsgId)).toBe(false)
    expect(store.messages[0].metadata?.delivery_status).toBe('failed')
    setConnected(true)
    expect(harness.sent).toHaveLength(0)

    expect(store.retryPrompt(clientMsgId)).toBe(true)
    expect(harness.sent).toHaveLength(1)
    const retryClientMsgId = store.messages[0].id
    expect(retryClientMsgId).not.toBe(clientMsgId)
    expect(harness.sent[0].payload?.client_msg_id).toBe(retryClientMsgId)
    expect(harness.sent[0].payload?.retry).toBeUndefined()
    expect(store.messages[0].metadata?.delivery_status).toBe('sending')

    emit({
      type: 'prompt_sent',
      device_id: 'device-a',
      session_id: 'session-a',
      timestamp: 2,
      payload: { client_msg_id: retryClientMsgId, message_id: retryClientMsgId, prompt: '修复它' }
    })
    expect(store.isPending(retryClientMsgId)).toBe(false)
    expect(store.messages[0].metadata?.delivery_status).toBeUndefined()
    store.cleanup()
  })

  it('turns an already-running daemon rejection into a useful recovery message', () => {
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.currentSession = {
      id: 'session-a',
      device_id: 'device-a',
      agent_type: 'codex',
      status: 'idle',
      summary: '',
      last_activity: 0
    }
    store.sendPrompt('device-a', 'session-a', '碰巧撞车')
    const clientMsgId = store.messages[0].id

    emit({
      type: 'prompt_failed',
      device_id: 'device-a',
      session_id: 'session-a',
      timestamp: 1,
      payload: {
        client_msg_id: clientMsgId,
        message: 'agent session is already running; wait for it to finish'
      }
    })

    expect(store.lastError).toBe(tGlobal('errors.busyRetry'))
    expect(store.messages[0].metadata?.delivery_error).toBe(tGlobal('errors.busyRetry'))
    store.cleanup()
  })

  it('does not stop current streaming for an error from another session', () => {
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.currentSession = {
      id: 'session-a',
      device_id: 'device-a',
      agent_type: 'codex',
      status: 'running',
      summary: '',
      last_activity: 0
    }
    store.streaming = true
    store.importing = true

    emit({
      type: 'error',
      device_id: 'device-a',
      session_id: 'session-b',
      timestamp: 1,
      payload: { message: 'other failed' }
    })
    expect(store.streaming).toBe(true)
    expect(store.importing).toBe(true)

    emit({
      type: 'error',
      device_id: 'device-a',
      session_id: 'session-a',
      timestamp: 2,
      payload: { message: 'current failed' }
    })
    expect(store.streaming).toBe(false)
    expect(store.importing).toBe(false)
  })

  it('filters legacy Codex diagnostics buffered for a background session', () => {
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.currentSession = {
      id: 'session-a',
      device_id: 'device-a',
      agent_type: 'codex',
      status: 'idle',
      summary: '',
      last_activity: 0
    }

    emit({
      type: 'session_message',
      device_id: 'device-a',
      session_id: 'session-b',
      timestamp: 1,
      payload: {
        message: {
          id: 'legacy-warning',
          role: 'assistant',
          content:
            '2026-07-29T04:48:01.778780Z WARN ' +
            'codex_core_skills::loader: ignoring invalid icon',
          type: 'text',
          timestamp: 1,
          metadata: { agent_type: 'codex', stream: true }
        }
      }
    })
    emit({
      type: 'session_message',
      device_id: 'device-a',
      session_id: 'session-b',
      timestamp: 2,
      payload: {
        message: {
          id: 'assistant-reply',
          role: 'assistant',
          content: '正常回复',
          type: 'assistant',
          timestamp: 2,
          metadata: { agent_type: 'codex', stream: true }
        }
      }
    })

    store.setCurrentSession({
      id: 'session-b',
      device_id: 'device-a',
      agent_type: 'codex',
      status: 'idle',
      summary: '',
      last_activity: 0
    })

    expect(store.messages.map(message => message.id)).toEqual(['assistant-reply'])
    store.cleanup()
  })

  it('shows a session error message and stops streaming polls', () => {
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.currentSession = {
      id: 'session-a',
      device_id: 'device-a',
      agent_type: 'kilo',
      status: 'running',
      summary: '',
      last_activity: 0
    }
    store.importing = true

    emit({
      type: 'prompt_sent',
      device_id: 'device-a',
      session_id: 'session-a',
      timestamp: 1,
      payload: { message_id: 'prompt-a', prompt: 'ping' }
    })
    expect(store.streaming).toBe(true)

    emit({
      type: 'session_message',
      device_id: 'device-a',
      session_id: 'session-a',
      timestamp: 2,
      payload: {
        message: {
          id: 'kilo-error',
          role: 'system',
          content: 'MessageAbortedError',
          type: 'error',
          timestamp: 2
        }
      }
    })

    expect(store.messages).toContainEqual(expect.objectContaining({
      id: 'kilo-error',
      type: 'error',
      content: 'MessageAbortedError'
    }))
    expect(store.lastError).toBe('MessageAbortedError')
    expect(store.streaming).toBe(false)
    expect(store.importing).toBe(false)

    vi.advanceTimersByTime(6000)
    expect(harness.sent.filter(message => message.type === 'fetch_history')).toHaveLength(0)
    store.cleanup()
  })

  it('stops streaming when native history supplies a missed error frame', () => {
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.currentSession = {
      id: 'session-a',
      device_id: 'device-a',
      agent_type: 'kilo',
      status: 'running',
      summary: '',
      last_activity: 0
    }
    const promptTime = Math.floor(Date.now() / 1000)

    emit({
      type: 'prompt_sent',
      device_id: 'device-a',
      session_id: 'session-a',
      timestamp: promptTime,
      payload: { message_id: 'prompt-a', prompt: 'ping' }
    })
    expect(store.streaming).toBe(true)

    emit({
      type: 'session_history',
      device_id: 'device-a',
      session_id: 'session-a',
      timestamp: promptTime + 1,
      payload: {
        messages: [{
          id: 'kilo-error',
          role: 'system',
          content: 'Kilo execution failed: Aborted',
          type: 'error',
          timestamp: promptTime + 1
        }]
      }
    })

    expect(store.lastError).toBe('Kilo execution failed: Aborted')
    expect(store.streaming).toBe(false)
    vi.advanceTimersByTime(6000)
    expect(harness.sent.filter(message => message.type === 'fetch_history')).toHaveLength(0)
    store.cleanup()
  })

  it('does not apply an old same-second history error to a new prompt', () => {
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.currentSession = {
      id: 'session-a',
      device_id: 'device-a',
      agent_type: 'kilo',
      status: 'running',
      summary: '',
      last_activity: 0
    }
    const promptTime = Math.floor(Date.now() / 1000)

    emit({
      type: 'prompt_sent',
      device_id: 'device-a',
      session_id: 'session-a',
      timestamp: promptTime,
      payload: { message_id: 'prompt-a', prompt: 'retry' }
    })
    emit({
      type: 'session_history',
      device_id: 'device-a',
      session_id: 'session-a',
      timestamp: promptTime,
      payload: {
        messages: [{
          id: 'old-kilo-error',
          role: 'system',
          content: 'old failure',
          type: 'error',
          timestamp: promptTime
        }]
      }
    })

    expect(store.lastError).toBeNull()
    expect(store.streaming).toBe(true)
    store.cleanup()
  })

  it('persists the complete bounded outbox across a store reload', () => {
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    const ids: string[] = []
    for (let i = 0; i < MAX_OUTBOX; i++) {
      expect(store.sendPrompt('device-a', 'session-a', `queued-${i}`)).toBe(true)
      ids.push(store.messages[store.messages.length - 1].id)
    }

    const persisted = persistedOutboxItems()
    expect(persisted).toHaveLength(MAX_OUTBOX)
    expect(store.sendPrompt('device-a', 'session-a', 'one-too-many')).toBe(false)
    expect(store.messages).toHaveLength(MAX_OUTBOX)

    setActivePinia(createPinia())
    const reloaded = useSessionStore()
    expect(ids.every(id => reloaded.isPending(id))).toBe(true)
  })

  it('refuses a new prompt when durable outbox persistence fails', () => {
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new DOMException('quota exceeded', 'QuotaExceededError')
    })
    const store = useSessionStore()
    store.subscribeDevice('device-a')

    expect(store.sendPrompt('device-a', 'session-a', 'must not disappear')).toBe(false)
    expect(store.messages).toHaveLength(0)
    expect(store.lastError).toBe(tGlobal('errors.outboxStorage'))
    expect(errorSpy).toHaveBeenCalled()
  })

  it('queries an unacknowledged command once with the same id and no retry flag', () => {
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.currentSession = {
      id: 'session-a',
      device_id: 'device-a',
      agent_type: 'codex',
      status: 'idle',
      summary: '',
      last_activity: 0
    }

    store.sendPrompt('device-a', 'session-a', 'will-ack')
    const acknowledgedId = store.messages[0].id
    emit({
      type: 'prompt_sent',
      device_id: 'device-a',
      session_id: 'session-a',
      timestamp: 1,
      payload: {
        client_msg_id: acknowledgedId,
        message_id: acknowledgedId,
        prompt: 'will-ack'
      }
    })
    vi.advanceTimersByTime(PROMPT_ACK_TIMEOUT_MS)
    expect(sentPrompts()).toHaveLength(1)

    // This test exercises ACK queries, not the connected busy-session guard.
    store.streaming = false
    store.sendPrompt('device-a', 'session-a', 'still-pending')
    const pendingId = store.messages[store.messages.length - 1].id
    vi.advanceTimersByTime(PROMPT_ACK_TIMEOUT_MS - 1)
    expect(sentPrompts()).toHaveLength(2)
    vi.advanceTimersByTime(1)
    expect(sentPrompts()).toHaveLength(3)
    expect(sentPrompts()[2].payload?.client_msg_id).toBe(pendingId)
    expect(sentPrompts()[2].payload?.retry).toBeUndefined()

    vi.advanceTimersByTime(PROMPT_ACK_TIMEOUT_MS * 2)
    expect(sentPrompts()).toHaveLength(3)
    store.cleanup()
  })

  it('queries pending commands again when the active daemon comes online', () => {
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.sendPrompt('device-a', 'session-a', 'wait for daemon')
    const clientMsgId = store.messages[0].id

    vi.advanceTimersByTime(PROMPT_ACK_TIMEOUT_MS)
    expect(sentPrompts()).toHaveLength(2)

    emit({
      type: 'device_online',
      device_id: 'device-a',
      timestamp: 1,
      payload: { device_id: 'device-a' }
    })
    expect(sentPrompts()).toHaveLength(3)
    expect(sentPrompts()[2].payload?.client_msg_id).toBe(clientMsgId)
    expect(sentPrompts()[2].payload?.retry).toBeUndefined()
    store.cleanup()
    vi.advanceTimersByTime(PROMPT_ACK_TIMEOUT_MS)
    expect(sentPrompts()).toHaveLength(3)
  })

  it('cancels the ACK timer when the daemon rejects a prompt', () => {
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.currentSession = {
      id: 'session-a',
      device_id: 'device-a',
      agent_type: 'codex',
      status: 'idle',
      summary: '',
      last_activity: 0
    }
    store.sendPrompt('device-a', 'session-a', 'will fail')
    const clientMsgId = store.messages[0].id
    emit({
      type: 'prompt_failed',
      device_id: 'device-a',
      session_id: 'session-a',
      timestamp: 1,
      payload: { client_msg_id: clientMsgId, message: 'daemon offline' }
    })

    vi.advanceTimersByTime(PROMPT_ACK_TIMEOUT_MS)
    expect(sentPrompts()).toHaveLength(1)
  })

  it('persists an indeterminate outcome and never exposes ordinary retry', () => {
    setConnected(true)
    const session = {
      id: 'session-a',
      device_id: 'device-a',
      agent_type: 'codex' as const,
      status: 'idle' as const,
      summary: '',
      last_activity: 0
    }
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.currentSession = session
    store.sendPrompt('device-a', 'session-a', 'unsafe to repeat')
    const clientMsgId = store.messages[0].id

    emit({
      type: 'prompt_failed',
      device_id: 'device-a',
      session_id: 'session-a',
      timestamp: 1,
      payload: {
        client_msg_id: clientMsgId,
        message: 'daemon restarted during execution',
        outcome: 'indeterminate',
        retry_allowed: false
      }
    })
    expect(store.messages[0].metadata?.delivery_status).toBe('failed')
    expect(store.messages[0].metadata?.delivery_retry_allowed).toBe(false)
    expect(store.messages[0].metadata?.delivery_error).toBe(tGlobal('errors.ambiguousNoRetry'))
    expect(store.retryPrompt(clientMsgId)).toBe(false)
    expect(sentPrompts()).toHaveLength(1)

    const persisted = persistedOutboxItems()
    expect(persisted.find(item => item.clientMsgId === clientMsgId)?.retryAllowed).toBe(false)

    setActivePinia(createPinia())
    const reloaded = useSessionStore()
    reloaded.subscribeDevice('device-a')
    reloaded.setCurrentSession(session)
    expect(reloaded.messages[0].metadata?.delivery_retry_allowed).toBe(false)
    expect(reloaded.retryPrompt(clientMsgId)).toBe(false)
    expect(sentPrompts()).toHaveLength(1)
    reloaded.cleanup()
  })

  it('does not overwrite another tab when independent stores enqueue offline', () => {
    setActivePinia(createPinia())
    const tabA = useSessionStore()
    setActivePinia(createPinia())
    const tabB = useSessionStore()

    expect(tabA.sendPrompt('device-a', 'session-a', 'from tab A')).toBe(true)
    const idA = tabA.messages[0].id
    expect(tabB.sendPrompt('device-a', 'session-a', 'from tab B')).toBe(true)
    const idB = tabB.messages[0].id

    expect(persistedOutboxItems()).toHaveLength(2)
    setActivePinia(createPinia())
    const reloaded = useSessionStore()
    expect(reloaded.isPending(idA)).toBe(true)
    expect(reloaded.isPending(idB)).toBe(true)
    reloaded.cleanup()
  })

  it('shows the first start_thread prompt in the owned native thread without resending it', () => {
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')

    const { ok, operationId } = store.startThread('device-a', 'kilo', 'D:\\repo', '  ping ×19  ')
    expect(ok).toBe(true)
    expect(harness.sent).toHaveLength(1)
    expect(harness.sent[0]).toMatchObject({
      type: 'start_thread',
      device_id: 'device-a',
      payload: {
        operation_id: operationId,
        agent_type: 'kilo',
        project_dir: 'D:\\repo',
        prompt: '  ping ×19  '
      }
    })

    emit({
      type: 'thread_starting',
      device_id: 'device-a',
      timestamp: 1,
      payload: { operation_id: operationId }
    })
    emit({
      type: 'thread_owned',
      device_id: 'device-a',
      timestamp: 2,
      payload: { operation_id: operationId, session_id: 'native-thread-a', prompt_accepted: true }
    })
    store.setCurrentSession({
      id: 'native-thread-a',
      device_id: 'device-a',
      agent_type: 'kilo',
      status: 'running',
      summary: '',
      last_activity: 1
    })

    expect(store.messages).toEqual([
      expect.objectContaining({
        id: `msg_${operationId}`,
        role: 'user',
        content: 'ping ×19'
      })
    ])
    expect(sentPrompts()).toHaveLength(0)
    store.cleanup()
  })

  it('does not synthesize native history for thread_indeterminate', () => {
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')

    const { ok, operationId } = store.startThread('device-a', 'grok_build', 'D:\\repo', 'ping ×19')
    expect(ok).toBe(true)

    emit({
      type: 'thread_starting',
      device_id: 'device-a',
      timestamp: 1,
      payload: { operation_id: operationId }
    })
    emit({
      type: 'thread_indeterminate',
      device_id: 'device-a',
      timestamp: 2,
      payload: { operation_id: operationId, session_id: 'native-thread-a' }
    })

    expect(store.startOps[operationId]).toMatchObject({
      status: 'indeterminate',
      agentType: 'grok_build',
      sessionId: 'native-thread-a',
      firstPrompt: 'ping ×19'
    })
    expect(store.messages).toEqual([])
    store.setCurrentSession({
      id: 'native-thread-a',
      device_id: 'device-a',
      agent_type: 'codex',
      status: 'idle',
      summary: '',
      last_activity: 1
    })
    expect(store.messages).toEqual([])
    expect(sentPrompts()).toHaveLength(0)
    store.cleanup()
  })

  it('includes first-turn attachments in start_thread rather than creating a follow-up prompt', () => {
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')

    const attachment = {
      id: 'upload-a', url: '/api/attachments/upload-a', name: 'diagram.png', mime: 'image/png', size: 42
    }
    const { ok, operationId } = store.startThread(
      'device-a', 'codex', 'D:\\repo', 'inspect the image', 'start-with-file', [attachment]
    )
    expect(ok).toBe(true)
    expect(harness.sent).toHaveLength(1)
    expect(harness.sent[0]).toMatchObject({
      type: 'start_thread',
      payload: {
        operation_id: operationId,
        prompt: 'inspect the image',
        attachments: [attachment]
      }
    })
    expect(sentPrompts()).toHaveLength(0)
    store.cleanup()
  })

  it('allows a busy Codex session to enqueue when the daemon advertises queue support', () => {
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.currentSession = {
      id: 'session-a', device_id: 'device-a', agent_type: 'codex', status: 'running',
      summary: '', last_activity: 0, capabilities: { queue: true }
    }

    expect(store.sendPrompt('device-a', 'session-a', 'next in FIFO')).toBe(true)
    expect(store.messages).toHaveLength(1)
    expect(sentPrompts()).toHaveLength(1)
    store.cleanup()
  })

  it('tracks daemon queue snapshots, keeps queued ACKs durable, and sends cancel/resume controls', () => {
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.currentSession = {
      id: 'session-a', device_id: 'device-a', agent_type: 'codex', status: 'running',
      summary: '', last_activity: 0, capabilities: { queue: true }
    }
    store.sendPrompt('device-a', 'session-a', 'queued work')
    const cid = store.messages[0].id

    emit({
      type: 'prompt_queued', device_id: 'device-a', session_id: 'session-a', timestamp: 1,
      payload: { client_msg_id: cid, queue_position: 2 }
    })
    expect(store.isPending(cid)).toBe(true)
    expect(store.currentPromptQueue?.items).toEqual([
      { client_msg_id: cid, position: 2, status: 'queued' }
    ])
    vi.advanceTimersByTime(PROMPT_ACK_TIMEOUT_MS)
    expect(sentPrompts()).toHaveLength(1)

    emit({
      type: 'queue_update', device_id: 'device-a', session_id: 'session-a', timestamp: 2,
      payload: { paused: true, items: [{ client_msg_id: cid, position: 1, status: 'paused' }] }
    })
    expect(store.currentPromptQueue).toEqual({
      paused: true, items: [{ client_msg_id: cid, position: 1, status: 'paused' }]
    })
    expect(store.cancelPrompt('device-a', 'session-a', cid)).toBe(true)
    expect(store.resumePromptQueue('device-a', 'session-a')).toBe(true)
    expect(harness.sent.slice(-2)).toMatchObject([
      { type: 'cancel_prompt', payload: { client_msg_id: cid } },
      { type: 'resume_prompt_queue', payload: {} }
    ])
    store.cleanup()
  })

  it('binds sealed command AAD and outer frames to one timestamp across async encryption', async () => {
    resetTransportModeForTests('sealed')
    setConnected(true)
    vi.setSystemTime(new Date(1_500))
    harness.encrypted = {
      alg: 'aes-256-gcm', version: 1, key_scope: 'session', epoch: 1,
      sender_id: 'phone', recipient_id: 'device-a', sequence: 1,
      nonce: 'nonce', ciphertext: 'ciphertext'
    }
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.currentSession = {
      id: 'session-a', device_id: 'device-a', agent_type: 'codex', status: 'running',
      summary: '', last_activity: 0, capabilities: { queue: true },
      pending_user_input: {
        request_id: 'request-a', item_id: 'item-a',
        questions: [{ id: 'answer', header: 'Answer', question: 'Continue?' }]
      }
    }

    expect(store.sendPrompt('device-a', 'session-a', 'sealed prompt')).toBe(true)
    vi.setSystemTime(new Date(3_500))
    await Promise.resolve()
    await Promise.resolve()
    expect(vi.mocked(encryptSessionPayload).mock.calls[0][6]).toBe(harness.sent[0].timestamp)

    const response = store.respondUserInput(
      'device-a', 'session-a', store.currentSession.pending_user_input!, { answer: ['yes'] }
    )
    vi.setSystemTime(new Date(5_500))
    expect(await response).toBe(true)
    expect(vi.mocked(encryptSessionPayload).mock.calls[1][6]).toBe(harness.sent[1].timestamp)

    expect(store.cancelPrompt('device-a', 'session-a', 'prompt-a')).toBe(true)
    vi.setSystemTime(new Date(7_500))
    await Promise.resolve()
    await Promise.resolve()
    expect(vi.mocked(encryptSessionPayload).mock.calls[2][6]).toBe(harness.sent[2].timestamp)
    store.cleanup()
  })

  it('reports definitive structured-input send failures and exposes rejected results for retry', async () => {
    resetTransportModeForTests('sealed')
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    const pending = {
      request_id: 'request-a', item_id: 'item-a',
      questions: [{ id: 'answer', header: 'Answer', question: 'Continue?' }]
    }
    store.currentSession = {
      id: 'session-a', device_id: 'device-a', agent_type: 'codex', status: 'running',
      summary: '', last_activity: 0, pending_user_input: pending
    }

    expect(await store.respondUserInput('device-a', 'session-a', pending, { answer: ['yes'] })).toBe(false)
    expect(harness.sent).toHaveLength(0)

    harness.decrypted = { request_id: 'request-a', status: 'rejected', message: 'try again' }
    emit({
      protocol_version: '1.1', transport_mode: 'sealed',
      type: 'user_input_result', device_id: 'device-a', session_id: 'session-a', timestamp: 2,
      sealed_payload: {
        alg: 'aes-256-gcm', version: 1, key_scope: 'session', epoch: 1,
        sender_id: 'device-a', recipient_id: 'phone-test', sequence: 2,
        nonce: 'nonce', ciphertext: 'ciphertext'
      }
    })
    await Promise.resolve()
    await Promise.resolve()
    expect(store.currentSession.pending_user_input?.request_id).toBe('request-a')
    expect(store.lastUserInputResult).toEqual({ requestId: 'request-a', status: 'rejected' })
    store.cleanup()
  })

  it('marks a confirmed cancellation without erasing a prior delivery failure', () => {
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.currentSession = {
      id: 'session-a', device_id: 'device-a', agent_type: 'codex', status: 'idle', summary: '', last_activity: 0
    }
    store.sendPrompt('device-a', 'session-a', 'cancel me')
    const cancelled = store.messages[0].id
    emit({
      type: 'prompt_cancelled', device_id: 'device-a', session_id: 'session-a', timestamp: 1,
      payload: { client_msg_id: cancelled }
    })
    expect(store.isPending(cancelled)).toBe(false)
    expect(store.messages[0].metadata?.delivery_status).toBe('cancelled')

    store.sendPrompt('device-a', 'session-a', 'already failed')
    const failed = store.messages[1].id
    emit({
      type: 'prompt_failed', device_id: 'device-a', session_id: 'session-a', timestamp: 2,
      payload: { client_msg_id: failed, message: 'daemon stopped' }
    })
    emit({
      type: 'prompt_cancelled', device_id: 'device-a', session_id: 'session-a', timestamp: 3,
      payload: { client_msg_id: failed }
    })
    expect(store.isPending(failed)).toBe(true)
    expect(store.messages[1].metadata?.delivery_status).toBe('failed')
    store.cleanup()
  })

  it('downgrades an owned result without prompt acknowledgement to indeterminate', () => {
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    const { operationId } = store.startThread('device-a', 'kimi_cli', 'D:\\repo', 'keep me')

    emit({
      type: 'thread_owned',
      device_id: 'device-a',
      timestamp: 2,
      payload: {
        operation_id: operationId,
        session_id: 'kimi_cli:native-thread',
        prompt_accepted: false
      }
    })

    expect(store.startOps[operationId]).toMatchObject({
      status: 'indeterminate',
      promptAccepted: false,
      sessionId: 'kimi_cli:native-thread'
    })
    expect(store.messages).toEqual([])
    store.cleanup()
  })

  it('decrypts sealed thread results without requiring plaintext application details', async () => {
    resetTransportModeForTests('sealed')
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')

    const { ok, operationId } = store.startThread('device-a', 'kimi_cli', 'D:\\repo', 'ping')
    expect(ok).toBe(true)
    harness.decrypted = {
      operation_id: operationId,
      agent_type: 'kimi_cli',
      error: 'private native error'
    }

    emit({
      protocol_version: '1.0',
      transport_mode: 'sealed',
      type: 'thread_failed',
      device_id: 'device-a',
      client_msg_id: operationId,
      timestamp: 2,
      sealed_payload: {
        alg: 'aes-256-gcm',
        version: 1,
        key_scope: 'device_catalog',
        epoch: 1,
        sender_id: 'device-a',
        recipient_id: 'phones',
        sequence: 1,
        nonce: 'opaque',
        ciphertext: 'opaque'
      }
    })
    await Promise.resolve()
    await Promise.resolve()

    expect(store.startOps[operationId]).toMatchObject({
      status: 'failed',
      agentType: 'kimi_cli',
      error: 'private native error'
    })
    store.cleanup()
  })

  it('downgrades an unauthenticated sealed owned result to indeterminate', async () => {
    resetTransportModeForTests('sealed')
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')

    const { ok, operationId } = store.startThread('device-a', 'claude_code', 'D:\\repo', 'ping')
    expect(ok).toBe(true)
    harness.decrypted = null

    emit({
      protocol_version: '1.0',
      transport_mode: 'sealed',
      type: 'thread_owned',
      device_id: 'device-a',
      session_id: 'untrusted-native-session',
      client_msg_id: operationId,
      timestamp: 2,
      sealed_payload: {
        alg: 'aes-256-gcm',
        version: 1,
        key_scope: 'device_catalog',
        epoch: 1,
        sender_id: 'device-a',
        recipient_id: 'phones',
        sequence: 1,
        nonce: 'tampered',
        ciphertext: 'tampered'
      }
    })
    await Promise.resolve()
    await Promise.resolve()

    expect(store.startOps[operationId]).toMatchObject({
      status: 'indeterminate',
      agentType: 'claude_code'
    })
    expect(store.startOps[operationId].sessionId).toBeUndefined()
    expect(store.messages).toEqual([])
    store.cleanup()
  })

  it('rejects plaintext thread results when the local nest mode is sealed', () => {
    resetTransportModeForTests('sealed')
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')

    const { ok, operationId } = store.startThread('device-a', 'grok_build', 'D:\\repo', 'ping')
    expect(ok).toBe(true)
    emit({
      protocol_version: '1.0',
      transport_mode: 'open',
      type: 'thread_owned',
      device_id: 'device-a',
      session_id: 'untrusted-native-session',
      client_msg_id: operationId,
      timestamp: 2,
      payload: {
        operation_id: operationId,
        session_id: 'untrusted-native-session'
      }
    })

    expect(store.startOps[operationId]).toMatchObject({
      status: 'indeterminate',
      agentType: 'grok_build'
    })
    expect(store.startOps[operationId].sessionId).toBeUndefined()
    expect(store.messages).toEqual([])
    store.cleanup()
  })

  it('keeps a present start-capability catalog distinct from legacy session capabilities', () => {
    const store = useSessionStore()
    store.subscribeDevice('device-a')

    emit({
      type: 'session_list',
      device_id: 'device-a',
      timestamp: 1,
      payload: {
        sessions: [],
        start_capabilities: [
          { agent_type: 'claude_code', available: true, spawn: true },
          { agent_type: 'kilo', available: false, spawn: false, reason: 'CLI missing' }
        ]
      }
    })

    expect(store.startCapabilities).toEqual([
      expect.objectContaining({ agent_type: 'claude_code', spawn: true }),
      expect.objectContaining({ agent_type: 'kilo', available: false, reason: 'CLI missing' })
    ])
    store.applySessionList({ sessions: [] }, 'device-a')
    expect(store.startCapabilities).toBeNull()
    store.cleanup()
  })

  it('marks the per-device catalog ready only after an authenticated session list and resets on switch', () => {
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    expect(store.catalogDeviceId).toBe('device-a')
    expect(store.catalogStatus).toBe('loading')

    emit({
      protocol_version: '1.1',
      transport_mode: 'open',
      type: 'session_list',
      device_id: 'device-a',
      timestamp: 1,
      payload: { sessions: [] }
    })
    expect(store.catalogStatus).toBe('ready')

    store.subscribeDevice('device-b')
    expect(store.catalogDeviceId).toBe('device-b')
    expect(store.catalogStatus).toBe('loading')
    expect(store.sessions).toEqual([])
    store.cleanup()
  })

  it('does not request history for a catalog-missing thread and can bind after it reappears', () => {
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    emit({
      protocol_version: '1.1',
      transport_mode: 'open',
      type: 'session_list',
      device_id: 'device-a',
      timestamp: 1,
      payload: { sessions: [] }
    })

    store.setCurrentSession(null)
    expect(apiFetch).not.toHaveBeenCalled()
    emit({
      protocol_version: '1.1',
      transport_mode: 'open',
      type: 'session_list',
      device_id: 'device-a',
      timestamp: 2,
      payload: {
        sessions: [{
          id: 'session-old', device_id: 'device-a', agent_type: 'codex',
          status: 'idle', summary: 'active again', last_activity: 2
        }]
      }
    })
    const reappeared = store.sessions.find(session => session.id === 'session-old')
    expect(reappeared?.summary).toBe('active again')
    store.setCurrentSession(reappeared || null)
    expect(apiFetch).toHaveBeenCalledTimes(1)
    store.cleanup()
  })

  it('neutralizes stale session controls while loading and after an authoritative removal', () => {
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.currentSession = {
      id: 'session-stale', device_id: 'device-a', agent_type: 'codex',
      status: 'running', summary: 'stale', last_activity: 1,
      capabilities: { interrupt: true }
    }
    store.streaming = true
    expect(store.currentSessionCatalogVisible).toBe(false)

    emit({
      protocol_version: '1.1', transport_mode: 'open', type: 'session_list',
      device_id: 'device-a', timestamp: 1, payload: { sessions: [] }
    })

    expect(store.catalogStatus).toBe('ready')
    expect(store.currentSession).toBeNull()
    expect(store.currentSessionCatalogVisible).toBe(false)
    expect(store.streaming).toBe(false)
    expect(store.loading).toBe(false)
    expect(store.messages).toEqual([])
    store.cleanup()
  })

  it('applies an immediate app-server capability downgrade to the open session', () => {
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.applySessionList({ sessions: [{
      id: 'session-a',
      device_id: 'device-a',
      agent_type: 'codex',
      status: 'running',
      summary: '',
      last_activity: 1,
      capabilities: {
        control_mode: 'app_server', approve: true, deny: true,
        interrupt: true, steer: true, queue: true
      }
    }] }, 'device-a')
    store.currentSession = store.sessions[0]

    emit({
      protocol_version: '1.1',
      transport_mode: 'open',
      type: 'session_update',
      device_id: 'device-a',
      session_id: 'session-a',
      timestamp: 2,
      payload: {
        session: {
          id: 'session-a',
          device_id: 'device-a',
          agent_type: 'codex',
          status: 'error',
          summary: '',
          last_activity: 2,
          capabilities: {
            control_mode: 'exec_resume', approve: false, deny: false,
            interrupt: false, steer: false, queue: false
          }
        }
      }
    })

    expect(store.currentSession).toMatchObject({
      status: 'error',
      capabilities: {
        control_mode: 'exec_resume', approve: false, deny: false,
        interrupt: false, steer: false, queue: false
      }
    })
    store.cleanup()
  })

  it('migrates the legacy outbox array into per-command records', () => {
    localStorage.setItem(LEGACY_OUTBOX_STORAGE_KEY, JSON.stringify([{
      clientMsgId: 'legacy-message',
      deviceId: 'device-a',
      sessionId: 'session-a',
      prompt: 'legacy prompt',
      status: 'queued',
      createdAt: 1
    }]))

    setActivePinia(createPinia())
    const store = useSessionStore()
    expect(store.isPending('legacy-message')).toBe(true)
    expect(localStorage.getItem(LEGACY_OUTBOX_STORAGE_KEY)).toBeNull()
    expect(persistedOutboxItems()).toEqual([
      expect.objectContaining({
        clientMsgId: 'legacy-message',
        prompt: 'legacy prompt',
        retryAllowed: true
      })
    ])
    store.cleanup()
  })
})
