import { ref } from 'vue'
import { defineStore } from 'pinia'
import type { AgentSession, SessionMessage } from '@/types/protocol'
import { nekoWS } from '@/api/websocket'
import { apiFetch } from '@/api/http'

export const useSessionStore = defineStore('sessions', () => {
  const sessions = ref<AgentSession[]>([])
  const currentSession = ref<AgentSession | null>(null)
  const messages = ref<SessionMessage[]>([])
  const loading = ref(false)
  const lastError = ref<string | null>(null)

  const HANDLER_ID = 'session-store'
  /** Bumps on each setCurrentSession so late history fetches cannot clobber newer state. */
  let historyGeneration = 0

  function subscribeDevice(deviceId: string) {
    const ws = nekoWS()
    ws.subscribe(deviceId)

    ws.removeHandler(HANDLER_ID)
    ws.addHandler(HANDLER_ID, (msg) => {
      if (msg.type === 'session_list' && msg.device_id === deviceId) {
        sessions.value = (msg.payload?.sessions as AgentSession[]) || []
        if (currentSession.value) {
          const updated = sessions.value.find(s => s.id === currentSession.value!.id)
          if (updated) currentSession.value = updated
        }
      } else if (msg.type === 'session_update') {
        // Accept both nested { session } and bare session object shapes.
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
          sessions.value.push(updated)
        }
      } else if (msg.type === 'session_message') {
        const sessionMessage = msg.payload?.message as SessionMessage
        if (!sessionMessage || !msg.session_id) return
        // Only append when a session is actively viewed AND ids match.
        if (!currentSession.value || msg.session_id !== currentSession.value.id) return
        messages.value.push(sessionMessage)
        if (messages.value.length > 500) {
          messages.value = messages.value.slice(-500)
        }
      } else if (msg.type === 'prompt_sent') {
        if (!currentSession.value || !msg.session_id || msg.session_id !== currentSession.value.id) return
        const userMsg: SessionMessage = {
          id: `user_${Date.now()}`,
          role: 'user',
          content: (msg.payload as { prompt?: string })?.prompt || '',
          type: 'text',
          timestamp: Math.floor(Date.now() / 1000)
        }
        messages.value.push(userMsg)
      } else if (msg.type === 'error') {
        const m = (msg.payload as { message?: string })?.message || 'unknown error'
        lastError.value = m
        if (currentSession.value && (!msg.session_id || msg.session_id === currentSession.value.id)) {
          messages.value.push({
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

  function setCurrentSession(session: AgentSession | null) {
    historyGeneration++
    const gen = historyGeneration
    currentSession.value = session
    lastError.value = null
    messages.value = []
    if (session) {
      fetchMessageHistory(session.device_id, session.id, gen)
    }
  }

  async function fetchMessageHistory(deviceId: string, sessionId: string, gen: number) {
    try {
      const res = await apiFetch(
        `/api/messages?device_id=${encodeURIComponent(deviceId)}&session_id=${encodeURIComponent(sessionId)}&limit=200`
      )
      if (!res.ok) return
      // Drop late responses after user navigated away / switched session
      if (gen !== historyGeneration) return
      if (currentSession.value?.id !== sessionId) return
      const data = await res.json()
      if (gen !== historyGeneration) return
      messages.value = (data.messages as SessionMessage[]) || []
    } catch (err) {
      console.warn('[session] failed to fetch message history:', err)
    }
  }

  function clearMessages() {
    messages.value = []
  }

  function sendPrompt(deviceId: string, sessionId: string, prompt: string) {
    lastError.value = null
    nekoWS().send({
      type: 'send_prompt',
      device_id: deviceId,
      session_id: sessionId,
      timestamp: Math.floor(Date.now() / 1000),
      payload: { prompt }
    })
  }

  function approve(deviceId: string, sessionId: string, approvalId: string) {
    nekoWS().send({
      type: 'approve',
      device_id: deviceId,
      session_id: sessionId,
      timestamp: Math.floor(Date.now() / 1000),
      payload: { approval_id: approvalId }
    })
  }

  function deny(deviceId: string, sessionId: string, approvalId: string) {
    nekoWS().send({
      type: 'deny',
      device_id: deviceId,
      session_id: sessionId,
      timestamp: Math.floor(Date.now() / 1000),
      payload: { approval_id: approvalId }
    })
  }

  function cleanup() {
    nekoWS().removeHandler(HANDLER_ID)
  }

  return {
    sessions, currentSession, messages, loading, lastError,
    subscribeDevice, setCurrentSession, clearMessages,
    sendPrompt, approve, deny, cleanup
  }
})
