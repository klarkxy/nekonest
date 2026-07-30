import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { tGlobal } from '@/i18n'
import type { NekoMessage } from '@/types/protocol'

const harness = vi.hoisted(() => ({
  connected: false,
  status: 'disconnected' as 'connecting' | 'connected' | 'disconnected' | 'auth_error',
  subscribedDevice: null as string | null,
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
  apiFetch: vi.fn(async () => new Response('', { status: 503 }))
}))

import {
  LEGACY_OUTBOX_STORAGE_KEY,
  MAX_OUTBOX,
  OUTBOX_STORAGE_KEY_PREFIX,
  PROMPT_ACK_TIMEOUT_MS,
  useSessionStore
} from './session'

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
    vi.useFakeTimers()
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
    vi.clearAllTimers()
    vi.useRealTimers()
    vi.restoreAllMocks()
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

  it('freezes a failed prompt and explicitly retries with the same client id', () => {
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
    expect(harness.sent[0].payload?.client_msg_id).toBe(clientMsgId)
    expect(harness.sent[0].payload?.retry).toBe(true)
    expect(store.messages[0].metadata?.delivery_status).toBe('sending')

    emit({
      type: 'prompt_sent',
      device_id: 'device-a',
      session_id: 'session-a',
      timestamp: 2,
      payload: { client_msg_id: clientMsgId, message_id: clientMsgId, prompt: '修复它' }
    })
    expect(store.isPending(clientMsgId)).toBe(false)
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
