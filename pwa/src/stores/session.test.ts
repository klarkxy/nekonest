import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { tGlobal } from '@/i18n'
import type {
  AgentSession,
  AgentType,
  AttachmentMode,
  NekoMessage,
  SessionCapabilities
} from '@/types/protocol'
import { resetTransportModeForTests } from '@/api/transport'
import { apiFetch } from '@/api/http'

const harness = vi.hoisted(() => ({
  connected: false,
  status: 'disconnected' as 'connecting' | 'connected' | 'disconnected' | 'auth_error',
  subscribedDevice: null as string | null,
  protocolVersion: '1.2',
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
    getSubscribedDevice() {
      return harness.subscribedDevice
    },
    isConnected() {
      return harness.connected
    },
    send(msg: Partial<NekoMessage>) {
      if (!harness.connected) return false
      harness.sent.push(structuredClone(msg))
      return true
    },
    getProtocolVersion() {
      return harness.protocolVersion
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

function grantCapabilities(
  store: ReturnType<typeof useSessionStore>,
  capabilities: SessionCapabilities
) {
  if (!store.currentSession) throw new Error('test session required')
  store.currentSession.capabilities = { ...store.currentSession.capabilities, ...capabilities }
}

function grantStartCapability(
  store: ReturnType<typeof useSessionStore>,
  agentType: AgentType,
  attachmentMode: AttachmentMode = 'unsupported'
) {
  store.startCapabilities = [{
    agent_type: agentType,
    available: true,
    spawn: true,
    attachment_mode: attachmentMode
  }]
}

function setSendableSession(store: ReturnType<typeof useSessionStore>, status: AgentSession['status'] = 'idle') {
  store.currentSession = {
    id: 'session-a', device_id: 'device-a', agent_type: 'codex', status,
    summary: '', last_activity: 0, capabilities: { send: true, attachment_mode: 'path_best_effort' }
  }
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
    harness.protocolVersion = '1.2'
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

    grantCapabilities(store, { send: true })
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

    grantCapabilities(store, { send: true })
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

      grantCapabilities(store, { send: true })
      expect(store.sendPrompt('device-a', 'session-a', 'do not overlap')).toBe(false)
      expect(store.lastError).toBe(tGlobal('errors.busySend'))
      expect(harness.sent).toHaveLength(0)
      store.cleanup()
    }
  )

  it('sends a trimmed steer command without adding it to the prompt outbox', async () => {
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')
	store.currentSession = {
	  id: 'session-a', device_id: 'device-a', agent_type: 'codex', status: 'running',
	  summary: '', last_activity: 0, capabilities: { steer: true }
	}

    expect(await store.steer('device-a', 'session-a', '  change direction  ')).toBe(true)
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

  it('rejects an empty steer command before touching the channel', async () => {
    setConnected(true)
    const store = useSessionStore()

    expect(await store.steer('device-a', 'session-a', '   ')).toBe(false)
    expect(store.lastError).toBe(tGlobal('errors.emptySteer'))
    expect(harness.sent).toHaveLength(0)
    store.cleanup()
  })

  it('binds interrupt to the exact advertised active turn', () => {
	setConnected(true)
	const store = useSessionStore()
	store.subscribeDevice('device-a')
	store.currentSession = {
	  id: 'session-a', device_id: 'device-a', agent_type: 'codex', status: 'running',
	  summary: '', last_activity: 0, capabilities: { interrupt: true },
	  active_turn: { generation: 17, client_msg_id: 'prompt-17', native_request_id: 'turn-17' }
	}

	expect(store.interrupt('device-a', 'session-a')).toBe(true)
	expect(harness.sent).toHaveLength(1)
	expect(harness.sent[0]).toMatchObject({
	  type: 'interrupt', device_id: 'device-a', session_id: 'session-a',
	  payload: { generation: 17, client_msg_id: 'prompt-17' }
	})
	store.cleanup()
  })

  it('uses the legacy interrupt command only for a confirmed 1.1 producer', () => {
	setConnected(true)
	const store = useSessionStore()
	store.subscribeDevice('device-a')
	store.applySessionList({ sessions: [{
	  id: 'session-a', device_id: 'device-a', agent_type: 'codex', status: 'running',
	  summary: '', last_activity: 0, capabilities: { interrupt: true }
	}] }, 'device-a', '1.1')
	store.currentSession = store.sessions[0]

	expect(store.canInterrupt('device-a', 'session-a')).toBe(true)
	expect(store.interrupt('device-a', 'session-a')).toBe(true)
	expect(harness.sent).toHaveLength(1)
	expect(harness.sent[0]).toMatchObject({
	  type: 'interrupt', device_id: 'device-a', session_id: 'session-a', payload: {}
	})
	store.cleanup()
  })


  it('rejects interrupt when no active turn binding is advertised', () => {
	setConnected(true)
	const store = useSessionStore()
	store.currentSession = {
	  id: 'session-a', device_id: 'device-a', agent_type: 'codex', status: 'running',
	  summary: '', last_activity: 0, capabilities: { interrupt: true }
	}

	expect(store.interrupt('device-a', 'session-a')).toBe(false)
	expect(harness.sent).toHaveLength(0)
	store.cleanup()
  })

  it('fails closed every direct control when the session capability is absent', async () => {
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.currentSession = {
      id: 'session-a', device_id: 'device-a', agent_type: 'claude_code', status: 'idle',
      summary: '', last_activity: 0, capabilities: {}
    }
    const pending = {
      request_id: 'request-a', item_id: 'item-a',
      questions: [{ id: 'answer', header: 'Answer', question: 'Continue?' }]
    }

    expect(store.sendPrompt('device-a', 'session-a', 'prompt')).toBe(false)
    expect(store.approve('device-a', 'session-a', 'approval-a')).toBe(false)
    expect(store.deny('device-a', 'session-a', 'approval-a')).toBe(false)
    expect(store.interrupt('device-a', 'session-a')).toBe(false)
    expect(await store.steer('device-a', 'session-a', 'change direction')).toBe(false)
    expect(await store.respondUserInput('device-a', 'session-a', pending, { answer: ['yes'] })).toBe(false)
    expect(store.cancelPrompt('device-a', 'session-a', 'prompt-a')).toBe(false)
    expect(store.resumePromptQueue('device-a', 'session-a')).toBe(false)
    expect(store.skipPromptQueueItem('device-a', 'session-a', 'prompt-a')).toBe(false)
    expect(harness.sent).toHaveLength(0)
    store.cleanup()
  })

  it('rejects attachments when send is enabled but attachment support is absent', () => {
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.currentSession = {
      id: 'session-a', device_id: 'device-a', agent_type: 'claude_code', status: 'idle',
      summary: '', last_activity: 0, capabilities: { send: true }
    }

    expect(store.sendPrompt('device-a', 'session-a', '', [{
      url: '/api/attachments/a', name: 'a.png', mime: 'image/png'
    }])).toBe(false)
    expect(store.lastError).toBe(tGlobal('session.attachmentUnavailable'))
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

    grantCapabilities(store, { send: true })
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
    grantCapabilities(store, { send: true })
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

  it('fails closed when retry capability was revoked after the original failure', () => {
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.currentSession = {
      id: 'session-a', device_id: 'device-a', agent_type: 'claude_code', status: 'idle',
      summary: '', last_activity: 0, capabilities: { send: true, attachment_mode: 'path_best_effort' }
    }
    expect(store.sendPrompt('device-a', 'session-a', 'retry me')).toBe(true)
    const clientMsgId = store.messages[0].id
    emit({
      type: 'prompt_failed', device_id: 'device-a', session_id: 'session-a', timestamp: 1,
      payload: { client_msg_id: clientMsgId, message: 'failed' }
    })
    setConnected(true)
    store.currentSession.capabilities = { send: false, attachment_mode: 'path_best_effort' }

    expect(store.retryPrompt(clientMsgId)).toBe(false)
    expect(store.lastError).toBe(tGlobal('session.sendUnavailable'))
    expect(harness.sent).toHaveLength(0)
    expect(store.messages[0].id).toBe(clientMsgId)
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
    grantCapabilities(store, { send: true })
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
      agent_type: 'grok_build',
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
          id: 'grok-error',
          role: 'system',
          content: 'MessageAbortedError',
          type: 'error',
          timestamp: 2
        }
      }
    })

    expect(store.messages).toContainEqual(expect.objectContaining({
      id: 'grok-error',
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
      agent_type: 'grok_build',
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
          id: 'grok-error',
          role: 'system',
          content: 'Grok execution failed: Aborted',
          type: 'error',
          timestamp: promptTime + 1
        }]
      }
    })

    expect(store.lastError).toBe('Grok execution failed: Aborted')
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
      agent_type: 'grok_build',
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
          id: 'old-grok-error',
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
    setSendableSession(store)
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
    setSendableSession(store)

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

    grantCapabilities(store, { send: true })
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
    setSendableSession(store)
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
    grantCapabilities(store, { send: true })
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
    grantCapabilities(store, { send: true })
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
    setSendableSession(tabA)
    setActivePinia(createPinia())
    const tabB = useSessionStore()
    setSendableSession(tabB)

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
	grantStartCapability(store, 'kimi_cli')

    const { ok, operationId } = store.startThread('device-a', 'kimi_cli', 'D:\\repo', '  ping ×19  ')
    expect(ok).toBe(true)
    expect(harness.sent).toHaveLength(1)
    expect(harness.sent[0]).toMatchObject({
      type: 'start_thread',
      device_id: 'device-a',
      payload: {
        operation_id: operationId,
        agent_type: 'kimi_cli',
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
      agent_type: 'kimi_cli',
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
	grantStartCapability(store, 'grok_build')

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
	grantStartCapability(store, 'codex', 'native_image_and_file')

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
      summary: '', last_activity: 0, capabilities: { send: true, queue: true }
    }

    expect(store.sendPrompt('device-a', 'session-a', 'next in FIFO', undefined, 'plan')).toBe(true)
    expect(store.messages).toHaveLength(1)
    expect(sentPrompts()).toHaveLength(1)
    expect(sentPrompts()[0].payload).toMatchObject({ collaboration_mode: 'plan' })
    store.cleanup()
  })

  it('tracks daemon queue snapshots, keeps queued ACKs durable, and sends cancel/resume controls', () => {
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.currentSession = {
      id: 'session-a', device_id: 'device-a', agent_type: 'codex', status: 'running',
      summary: '', last_activity: 0, capabilities: { send: true, queue: true }
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
      paused: true, items: [{ client_msg_id: cid, position: 1, status: 'blocked_indeterminate' }]
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
    harness.protocolVersion = '1.1'
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
      summary: '', last_activity: 0, capabilities: { send: true, queue: true, user_input: true },
      pending_user_input: {
        request_id: 'request-a', item_id: 'item-a',
        questions: [{ id: 'answer', header: 'Answer', question: 'Continue?' }]
      }
    }

    expect(store.sendPrompt('device-a', 'session-a', 'sealed prompt', undefined, 'plan')).toBe(true)
    vi.setSystemTime(new Date(3_500))
    await Promise.resolve()
    await Promise.resolve()
    expect(vi.mocked(encryptSessionPayload).mock.calls[0][6]).toBe(harness.sent[0].timestamp)
    expect(vi.mocked(encryptSessionPayload).mock.calls[0][7]).toBe('1.1')
    expect(harness.sent[0].protocol_version).toBe('1.1')
    expect(vi.mocked(encryptSessionPayload).mock.calls[0][4]).toMatchObject({ collaboration_mode: 'plan' })

    const response = store.respondUserInput(
      'device-a', 'session-a', store.currentSession.pending_user_input!, { answer: ['yes'] }
    )
    vi.setSystemTime(new Date(5_500))
    expect(await response).toBe(true)
    expect(vi.mocked(encryptSessionPayload).mock.calls[1][6]).toBe(harness.sent[1].timestamp)
    expect(vi.mocked(encryptSessionPayload).mock.calls[1][7]).toBe('1.1')

    expect(store.cancelPrompt('device-a', 'session-a', 'prompt-a')).toBe(true)
    vi.setSystemTime(new Date(7_500))
    await Promise.resolve()
    await Promise.resolve()
    expect(vi.mocked(encryptSessionPayload).mock.calls[2][6]).toBe(harness.sent[2].timestamp)
    expect(vi.mocked(encryptSessionPayload).mock.calls[2][7]).toBe('1.1')
    expect(vi.mocked(encryptSessionPayload).mock.calls[2][5]).toBe('prompt-a')
    expect(harness.sent[2].client_msg_id).toBe('prompt-a')
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
      summary: '', last_activity: 0, capabilities: { user_input: true }, pending_user_input: pending
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
      id: 'session-a', device_id: 'device-a', agent_type: 'codex', status: 'idle', summary: '', last_activity: 0,
      capabilities: { send: true }
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
	grantStartCapability(store, 'kimi_cli')
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
	grantStartCapability(store, 'kimi_cli')

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
	grantStartCapability(store, 'claude_code')

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
	grantStartCapability(store, 'grok_build')

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
          { agent_type: 'grok_build', available: false, spawn: false, reason: 'CLI missing' }
        ]
      }
    })

    expect(store.startCapabilities).toEqual([
      expect.objectContaining({ agent_type: 'claude_code', spawn: true }),
      expect.objectContaining({ agent_type: 'grok_build', available: false, reason: 'CLI missing' })
    ])
    store.applySessionList({ sessions: [] }, 'device-a')
    expect(store.startCapabilities).toEqual([
      expect.objectContaining({ agent_type: 'claude_code', spawn: true }),
      expect.objectContaining({ agent_type: 'grok_build', available: false, reason: 'CLI missing' })
    ])
    expect(store.catalogStatus).toBe('ready')
    store.cleanup()
  })

  it('does not mark the catalog ready from a REST list that already contains the target', () => {
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.applySessionList({
      sessions: [{
        id: 'session-a', device_id: 'device-a', agent_type: 'codex',
        status: 'idle', summary: 'cached', last_activity: 1,
        capabilities: { interrupt: true }
      }]
    }, 'device-a')
    store.currentSession = store.sessions[0]

    expect(store.catalogStatus).toBe('loading')
    expect(store.currentSessionCatalogVisible).toBe(false)
    expect(store.sessions[0].id).toBe('session-a')

    emit({
      protocol_version: '1.1',
      transport_mode: 'open',
      type: 'session_list',
      device_id: 'device-a',
      timestamp: 2,
      payload: { sessions: [] }
    })
    expect(store.catalogStatus).toBe('ready')
    expect(store.currentSession).toBeNull()
    expect(store.currentSessionCatalogVisible).toBe(false)
    store.cleanup()
  })

  it('hides retired Kilo sessions and start capabilities from legacy daemon catalogs', () => {
    const store = useSessionStore()
    store.applySessionList({
      sessions: [
        {
          id: 'legacy-kilo', device_id: 'device-a', agent_type: 'kilo', status: 'idle',
          summary: 'legacy', last_activity: 1
        },
        {
          id: 'codex-session', device_id: 'device-a', agent_type: 'codex', status: 'idle',
          summary: 'active', last_activity: 2
        }
      ],
      start_capabilities: [
        { agent_type: 'kilo', available: true, spawn: true },
        { agent_type: 'codex', available: true, spawn: true }
      ]
    }, 'device-a', '1.1')

    expect(store.sessions.map(session => session.id)).toEqual(['codex-session'])
    expect(store.startCapabilities?.map(capability => capability.agent_type)).toEqual(['codex'])

    emit({
      type: 'session_update',
      protocol_version: '1.1',
      device_id: 'device-a',
      session_id: 'legacy-kilo-update',
      timestamp: 3,
      payload: {
        session: {
          id: 'legacy-kilo-update', device_id: 'device-a', agent_type: 'kilo', status: 'running',
          summary: 'legacy update', last_activity: 3
        }
      }
    })
    expect(store.sessions.map(session => session.id)).toEqual(['codex-session'])
    store.cleanup()
  })

  it('infers legacy send and interrupt only for a confirmed 1.1 daemon producer', () => {
    const store = useSessionStore()
    const session = {
      id: 'session-a', device_id: 'device-a', agent_type: 'claude_code' as const,
      status: 'idle' as const, summary: '', last_activity: 1, capabilities: {}
    }

    store.applySessionList({ sessions: [session] }, 'device-a', '1.1')
    expect(store.sessions[0].capabilities).toMatchObject({ send: true, interrupt: true })

    store.applySessionList({ sessions: [session] }, 'device-a', '1.2')
    expect(store.sessions[0].capabilities).toMatchObject({ send: false, interrupt: false })

    store.applySessionList({ sessions: [session] }, 'device-a')
    expect(store.sessions[0].capabilities).toMatchObject({ send: false, interrupt: false })
    expect(store.catalogProducerVersion).toBeNull()
    store.cleanup()
  })

  it('does not let an empty REST snapshot wipe a WS catalog', () => {
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.applySessionList({
      sessions: [{
        id: 'session-a', device_id: 'device-a', agent_type: 'codex',
        status: 'idle', summary: 'live', last_activity: 1
      }]
    }, 'device-a', '1.3', 'ws')
    store.applySessionList({ sessions: [] }, 'device-a', undefined, 'rest')
    expect(store.sessions).toHaveLength(1)
    expect(store.sessions[0].id).toBe('session-a')
    store.cleanup()
  })

  it('uses the legacy per-session spawn fallback only for explicit Codex capability and an absent catalog', () => {
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.currentSession = {
      id: 'codex-session', device_id: 'device-a', agent_type: 'codex', status: 'idle',
      summary: '', last_activity: 0, capabilities: { spawn: true }
    }

    expect(store.startCapabilities).toBeNull()
    expect(store.startThread('device-a', 'codex', 'D:\\repo', 'ping').ok).toBe(true)
    expect(harness.sent[harness.sent.length - 1]?.type).toBe('start_thread')

    store.startCapabilities = []
    expect(store.startThread('device-a', 'codex', 'D:\\repo', 'blocked').ok).toBe(false)
    expect(harness.sent).toHaveLength(1)
    store.cleanup()
  })

  it('does not apply the legacy start fallback to non-Codex or missing spawn capability', () => {
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.currentSession = {
      id: 'compat-session', device_id: 'device-a', agent_type: 'grok_build', status: 'idle',
      summary: '', last_activity: 0, capabilities: { spawn: true }
    }
    expect(store.startThread('device-a', 'grok_build', 'D:\\repo', 'ping').ok).toBe(false)

    store.currentSession = {
      id: 'codex-session', device_id: 'device-a', agent_type: 'codex', status: 'idle',
      summary: '', last_activity: 0, capabilities: {}
    }
    expect(store.startThread('device-a', 'codex', 'D:\\repo', 'ping').ok).toBe(false)
    expect(harness.sent).toHaveLength(0)
    store.cleanup()
  })

  it('rejects start attachments when the catalog does not advertise an attachment mode', () => {
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    grantStartCapability(store, 'codex')

    expect(store.startThread('device-a', 'codex', 'D:\\repo', 'inspect', '', [{
      url: '/api/attachments/a', name: 'a.png', mime: 'image/png'
    }]).ok).toBe(false)
    expect(store.lastError).toBe(tGlobal('session.attachmentUnavailable'))
    expect(harness.sent).toHaveLength(0)
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

  it('drops the previous device inbox, prompt queue, and start ops on switch', () => {
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    grantStartCapability(store, 'codex', 'path_best_effort')
    expect(store.startThread('device-a', 'codex', 'D:\\repo', 'hello').ok).toBe(true)
    expect(Object.keys(store.startOps)).toHaveLength(1)

    emit({
      type: 'queue_update', device_id: 'device-a', session_id: 'session-a', timestamp: 1,
      payload: { paused: false, items: [{ client_msg_id: 'prompt-a', position: 1, status: 'queued' }] }
    })
    expect(store.promptQueues['device-a::session-a']?.items).toHaveLength(1)

    emit({
      type: 'session_message', device_id: 'device-a', session_id: 'session-other', timestamp: 2,
      payload: {
        message: {
          id: 'msg-other', role: 'assistant', content: 'buffered', type: 'text', timestamp: 2
        }
      }
    })

    store.subscribeDevice('device-b')
    expect(store.promptQueues).toEqual({})
    expect(store.startOps).toEqual({})

    store.subscribeDevice('device-a')
    store.setCurrentSession({
      id: 'session-other', device_id: 'device-a', agent_type: 'codex',
      status: 'idle', summary: '', last_activity: 2
    })
    expect(store.messages.some(message => message.id === 'msg-other')).toBe(false)
    store.cleanup()
  })

  it('waits for sealed steer encryption before reporting success', async () => {
    resetTransportModeForTests('sealed')
    setConnected(true)
    let release: (value: NonNullable<NekoMessage['sealed_payload']>) => void
    const pendingEncrypt = new Promise<NonNullable<NekoMessage['sealed_payload']>>(resolve => {
      release = resolve
    })
    vi.mocked(encryptSessionPayload).mockImplementation(() => pendingEncrypt)
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.currentSession = {
      id: 'session-a', device_id: 'device-a', agent_type: 'codex', status: 'running',
      summary: '', last_activity: 0, capabilities: { steer: true }
    }

    const pending = store.steer('device-a', 'session-a', 'change direction')
    expect(harness.sent).toHaveLength(0)
    release!({
      alg: 'aes-256-gcm', version: 1, key_scope: 'session', epoch: 1,
      sender_id: 'phone', recipient_id: 'device-a', sequence: 1,
      nonce: 'nonce', ciphertext: 'ciphertext'
    })
    expect(await pending).toBe(true)
    expect(harness.sent).toHaveLength(1)
    expect(harness.sent[0]).toMatchObject({
      type: 'steer',
      device_id: 'device-a',
      session_id: 'session-a',
      sealed_payload: { ciphertext: 'ciphertext' }
    })
    expect(harness.sent[0].payload).toBeUndefined()
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

  it('clears an advertised active turn from a session update', () => {
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.applySessionList({ sessions: [{
      id: 'session-a', device_id: 'device-a', agent_type: 'codex', status: 'running',
      summary: '', last_activity: 1, capabilities: { interrupt: true },
      active_turn: { generation: 4, client_msg_id: 'message-4' }
    }] }, 'device-a', '1.2')
    store.currentSession = store.sessions[0]

    emit({
      protocol_version: '1.2', transport_mode: 'open', type: 'session_update',
      device_id: 'device-a', session_id: 'session-a', timestamp: 2,
      payload: { session: {
        id: 'session-a', device_id: 'device-a', agent_type: 'codex', status: 'idle',
        last_activity: 2, active_turn: null
      } }
    })

    expect(store.currentSession?.active_turn).toBeNull()
    expect(store.interrupt('device-a', 'session-a')).toBe(false)
    store.cleanup()
  })

  it('preserves the authoritative capability table across a partial active-turn update', () => {
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.applySessionList({ sessions: [{
      id: 'session-a', device_id: 'device-a', agent_type: 'codex', status: 'idle',
      summary: '', last_activity: 1,
      capabilities: {
        control_mode: 'app_server', send: true, approve: true, deny: true,
        interrupt: true, steer: true, queue: true, user_input: true,
        attachment_mode: 'native_image_and_file'
      }
    }] }, 'device-a', '1.2')
    store.currentSession = store.sessions[0]

    emit({
      protocol_version: '1.2', transport_mode: 'open', type: 'session_update',
      device_id: 'device-a', session_id: 'session-a', timestamp: 2,
      payload: { session: {
        id: 'session-a', device_id: 'device-a', agent_type: 'codex', status: 'running',
        last_activity: 2, active_turn: { generation: 8, client_msg_id: 'message-8' }
      } }
    })

    expect(store.currentSession?.capabilities).toMatchObject({
      send: true, approve: true, deny: true, interrupt: true, steer: true,
      queue: true, user_input: true, attachment_mode: 'native_image_and_file'
    })
    expect(store.canInterrupt('device-a', 'session-a')).toBe(true)
    expect(store.interrupt('device-a', 'session-a')).toBe(true)
    expect(harness.sent[0]).toMatchObject({
      type: 'interrupt', payload: { generation: 8, client_msg_id: 'message-8' }
    })
    store.cleanup()
  })

  it('applies a structured Codex question from a partial session update', () => {
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.applySessionList({ sessions: [{
      id: 'session-a', device_id: 'device-a', agent_type: 'codex', status: 'running',
      summary: '', last_activity: 1,
      capabilities: { send: true, queue: true, user_input: true }
    }] }, 'device-a', '1.2')
    store.currentSession = store.sessions[0]

    emit({
      protocol_version: '1.2', transport_mode: 'open', type: 'session_update',
      device_id: 'device-a', session_id: 'session-a', timestamp: 2,
      payload: { session: {
        id: 'session-a', device_id: 'device-a', agent_type: 'codex',
        status: 'waiting_user', last_activity: 2,
        pending_user_input: {
          request_id: 'request-1', item_id: 'item-1',
          questions: [{
            id: 'choice', header: 'Choice', question: 'Pick one',
            options: [{ label: 'Alpha', description: 'First option' }],
            is_other: true
          }]
        }
      } }
    })

    expect(store.currentSession?.status).toBe('waiting_user')
    expect(store.currentSession?.pending_user_input).toMatchObject({
      request_id: 'request-1',
      questions: [{ id: 'choice', question: 'Pick one', is_other: true }]
    })
    store.cleanup()
  })

  it('starts catch-up polling when the open session becomes running without a local prompt', async () => {
    vi.mocked(apiFetch).mockResolvedValue(
      new Response(JSON.stringify({ messages: [] }), { status: 200 })
    )
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.applySessionList({ sessions: [{
      id: 'session-a', device_id: 'device-a', agent_type: 'grok_build', status: 'idle',
      summary: '', last_activity: 1, capabilities: { send: true }
    }] }, 'device-a')
    store.currentSession = store.sessions[0]
    vi.mocked(apiFetch).mockClear()

    emit({
      protocol_version: '1.2', transport_mode: 'open', type: 'session_update',
      device_id: 'device-a', session_id: 'session-a', timestamp: 2,
      payload: { session: {
        id: 'session-a', device_id: 'device-a', agent_type: 'grok_build',
        status: 'running', last_activity: 2
      } }
    })

    expect(store.currentSession?.status).toBe('running')
    expect(store.streaming).toBe(true)
    await vi.advanceTimersByTimeAsync(2000)
    expect(apiFetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/messages?device_id=device-a&session_id=session-a')
    )
    store.cleanup()
  })

  it('suppresses duplicate running updates from creating extra catch-up timers', async () => {
    vi.mocked(apiFetch).mockResolvedValue(
      new Response(JSON.stringify({ messages: [] }), { status: 200 })
    )
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.applySessionList({ sessions: [{
      id: 'session-a', device_id: 'device-a', agent_type: 'grok_build', status: 'idle',
      summary: '', last_activity: 1, capabilities: { send: true }
    }] }, 'device-a')
    store.currentSession = store.sessions[0]
    vi.mocked(apiFetch).mockClear()

    const runningUpdate = {
      protocol_version: '1.2', transport_mode: 'open' as const, type: 'session_update' as const,
      device_id: 'device-a', session_id: 'session-a', timestamp: 2,
      payload: { session: {
        id: 'session-a', device_id: 'device-a', agent_type: 'grok_build' as const,
        status: 'running' as const, last_activity: 2
      } }
    }
    emit(runningUpdate)
    emit({ ...runningUpdate, timestamp: 3 })
    await vi.advanceTimersByTimeAsync(2000)
    expect(apiFetch).toHaveBeenCalledTimes(1)
    store.cleanup()
  })

  it('starts catch-up polling after reopening a running session history load', async () => {
    vi.mocked(apiFetch).mockResolvedValue(
      new Response(JSON.stringify({
        messages: [{
          id: 'assistant-1', role: 'assistant', content: 'partial',
          type: 'text', timestamp: 1
        }]
      }), { status: 200 })
    )
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.applySessionList({ sessions: [{
      id: 'session-a', device_id: 'device-a', agent_type: 'grok_build', status: 'running',
      summary: '', last_activity: 1, capabilities: { send: true }
    }] }, 'device-a')

    store.setCurrentSession(store.sessions[0])
    await Promise.resolve()
    await Promise.resolve()
    await vi.advanceTimersByTimeAsync(0)
    expect(store.messages.some(message => message.id === 'assistant-1')).toBe(true)
    expect(store.streaming).toBe(true)

    vi.mocked(apiFetch).mockClear()
    await vi.advanceTimersByTimeAsync(2000)
    expect(apiFetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/messages?device_id=device-a&session_id=session-a')
    )
    store.cleanup()
  })

  it('restarts catch-up polling on disconnect and resubscribe for the open running session', async () => {
    vi.mocked(apiFetch).mockResolvedValue(
      new Response(JSON.stringify({ messages: [] }), { status: 200 })
    )
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.applySessionList({ sessions: [{
      id: 'session-a', device_id: 'device-a', agent_type: 'grok_build', status: 'running',
      summary: '', last_activity: 1, capabilities: { send: true }
    }] }, 'device-a')
    store.currentSession = store.sessions[0]

    emit({
      protocol_version: '1.2', transport_mode: 'open', type: 'session_update',
      device_id: 'device-a', session_id: 'session-a', timestamp: 2,
      payload: { session: {
        id: 'session-a', device_id: 'device-a', agent_type: 'grok_build',
        status: 'running', last_activity: 2
      } }
    })
    expect(store.streaming).toBe(true)

    setConnected(false)
    store.cleanup()
    expect(store.streaming).toBe(false)

    store.subscribeDevice('device-a')
    store.currentSession = {
      id: 'session-a', device_id: 'device-a', agent_type: 'grok_build', status: 'running',
      summary: '', last_activity: 2, capabilities: { send: true }
    }
    vi.mocked(apiFetch).mockClear()
    setConnected(true)
    await vi.advanceTimersByTimeAsync(2000)
    expect(apiFetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/messages?device_id=device-a&session_id=session-a')
    )
    store.cleanup()
  })

  it('keeps catch-up polling past the 8s stream-idle timer while the open session stays running', async () => {
    vi.mocked(apiFetch).mockResolvedValue(
      new Response(JSON.stringify({ messages: [] }), { status: 200 })
    )
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.applySessionList({ sessions: [{
      id: 'session-a', device_id: 'device-a', agent_type: 'grok_build', status: 'idle',
      summary: '', last_activity: 1, capabilities: { send: true }
    }] }, 'device-a')
    store.currentSession = store.sessions[0]

    emit({
      protocol_version: '1.2', transport_mode: 'open', type: 'session_update',
      device_id: 'device-a', session_id: 'session-a', timestamp: 2,
      payload: { session: {
        id: 'session-a', device_id: 'device-a', agent_type: 'grok_build',
        status: 'running', last_activity: 2
      } }
    })
    expect(store.streaming).toBe(true)

    vi.mocked(apiFetch).mockClear()
    await vi.advanceTimersByTimeAsync(8000)
    expect(store.streaming).toBe(false)
    await vi.advanceTimersByTimeAsync(2000)
    expect(apiFetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/messages?device_id=device-a&session_id=session-a')
    )
    store.cleanup()
  })

  it.each(['waiting_user', 'waiting_approval'] as const)(
    'clears replying state when a running session enters %s',
    async status => {
      vi.mocked(apiFetch).mockResolvedValue(
        new Response(JSON.stringify({ messages: [] }), { status: 200 })
      )
      setConnected(true)
      const store = useSessionStore()
      store.subscribeDevice('device-a')
      store.applySessionList({ sessions: [{
        id: 'session-a', device_id: 'device-a', agent_type: 'grok_build', status: 'running',
        summary: '', last_activity: 1, capabilities: { send: true }
      }] }, 'device-a')
      store.currentSession = store.sessions[0]

      emit({
        protocol_version: '1.2', transport_mode: 'open', type: 'session_update',
        device_id: 'device-a', session_id: 'session-a', timestamp: 2,
        payload: { session: {
          id: 'session-a', device_id: 'device-a', agent_type: 'grok_build',
          status: 'running', last_activity: 2
        } }
      })
      expect(store.streaming).toBe(true)

      emit({
        protocol_version: '1.2', transport_mode: 'open', type: 'session_update',
        device_id: 'device-a', session_id: 'session-a', timestamp: 3,
        payload: { session: {
          id: 'session-a', device_id: 'device-a', agent_type: 'grok_build',
          status, last_activity: 3
        } }
      })
      expect(store.streaming).toBe(false)
      vi.mocked(apiFetch).mockClear()
      await vi.advanceTimersByTimeAsync(4000)
      expect(apiFetch).not.toHaveBeenCalled()
      store.cleanup()
    }
  )

  it('stops catch-up polling on session switch and terminal idle updates', async () => {
    vi.mocked(apiFetch).mockResolvedValue(
      new Response(JSON.stringify({ messages: [] }), { status: 200 })
    )
    setConnected(true)
    const store = useSessionStore()
    store.subscribeDevice('device-a')
    store.applySessionList({ sessions: [
      {
        id: 'session-a', device_id: 'device-a', agent_type: 'grok_build', status: 'running',
        summary: '', last_activity: 1, capabilities: { send: true }
      },
      {
        id: 'session-b', device_id: 'device-a', agent_type: 'codex', status: 'idle',
        summary: '', last_activity: 1, capabilities: { send: true }
      }
    ] }, 'device-a')
    store.currentSession = store.sessions[0]
    emit({
      protocol_version: '1.2', transport_mode: 'open', type: 'session_update',
      device_id: 'device-a', session_id: 'session-a', timestamp: 2,
      payload: { session: {
        id: 'session-a', device_id: 'device-a', agent_type: 'grok_build',
        status: 'running', last_activity: 2
      } }
    })
    expect(store.streaming).toBe(true)

    store.setCurrentSession(store.sessions[1])
    expect(store.streaming).toBe(false)
    vi.mocked(apiFetch).mockClear()
    await vi.advanceTimersByTimeAsync(4000)
    expect(apiFetch).not.toHaveBeenCalledWith(
      expect.stringContaining('session_id=session-a')
    )

    store.currentSession = {
      id: 'session-a', device_id: 'device-a', agent_type: 'grok_build', status: 'running',
      summary: '', last_activity: 3, capabilities: { send: true }
    }
    emit({
      protocol_version: '1.2', transport_mode: 'open', type: 'session_update',
      device_id: 'device-a', session_id: 'session-a', timestamp: 4,
      payload: { session: {
        id: 'session-a', device_id: 'device-a', agent_type: 'grok_build',
        status: 'running', last_activity: 4
      } }
    })
    expect(store.streaming).toBe(true)
    emit({
      protocol_version: '1.2', transport_mode: 'open', type: 'session_update',
      device_id: 'device-a', session_id: 'session-a', timestamp: 5,
      payload: { session: {
        id: 'session-a', device_id: 'device-a', agent_type: 'grok_build',
        status: 'idle', last_activity: 5
      } }
    })
    expect(store.streaming).toBe(false)
    vi.mocked(apiFetch).mockClear()
    await vi.advanceTimersByTimeAsync(4000)
    expect(apiFetch).not.toHaveBeenCalled()
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
