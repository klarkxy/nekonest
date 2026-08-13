import { loadOrCreatePhoneIdentity } from '@/crypto/identity'
import { apiURL, handoffExchangePath } from '@/config/runtimeEndpoint'
import { setPhoneId, setPhoneToken, setRouteHandle } from './http'

const HANDOFF_FRAGMENT_KEY = 'handoff'
const HANDOFF_FAILURE_KEY = 'nekonest_handoff_failure'
const TICKET_PATTERN = /^[A-Za-z0-9._~-]{20,512}$/

export type HandoffFailure = {
  message: string
  action_url?: string
}

export type HandoffResult = {
  state: 'none' | 'exchanged'
  phoneId?: string
}

type HandoffResponse = {
  phone_id?: string
  phone_token?: string
  route_handle?: string
  message?: string
  action_url?: string
  retryable?: boolean
}

export function takeHandoffTicketFromFragment(): string {
  const raw = location.hash.startsWith('#') ? location.hash.slice(1) : location.hash
  if (!raw) return ''
  const params = new URLSearchParams(raw)
  const ticket = (params.get(HANDOFF_FRAGMENT_KEY) || '').trim()
  if (!ticket) return ''

  // Remove the capability before identity lookup or any network request.
  history.replaceState(history.state, '', `${location.pathname}${location.search}`)
  if (!TICKET_PATTERN.test(ticket)) {
    throw new Error('The Cloud handoff link is invalid or incomplete.')
  }
  return ticket
}

function safeActionURL(value: unknown): string | undefined {
  if (typeof value !== 'string' || !value.trim()) return undefined
  try {
    const parsed = new URL(value)
    return parsed.protocol === 'https:' ? parsed.toString() : undefined
  } catch {
    return undefined
  }
}

export function saveHandoffFailure(error: unknown) {
  const failure: HandoffFailure = {
    message: error instanceof Error ? error.message : 'Cloud handoff failed.',
    action_url: safeActionURL((error as { action_url?: unknown } | null)?.action_url)
  }
  sessionStorage.setItem(HANDOFF_FAILURE_KEY, JSON.stringify(failure))
}

export function readHandoffFailure(): HandoffFailure | null {
  try {
    const raw = sessionStorage.getItem(HANDOFF_FAILURE_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as HandoffFailure
    return parsed && typeof parsed.message === 'string' ? parsed : null
  } catch {
    return null
  }
}

export function clearHandoffFailure() {
  sessionStorage.removeItem(HANDOFF_FAILURE_KEY)
}

export async function exchangeHandoffTicket(ticket: string): Promise<HandoffResult> {
  if (!ticket) return { state: 'none' }

  const identity = await loadOrCreatePhoneIdentity()
  const requestBody = JSON.stringify({
    ticket,
    pwa_origin: location.origin,
    name: 'Cloud PWA',
    phone_ed25519_public: identity.ed25519_public,
    phone_x25519_public: identity.x25519_public,
    identity_fingerprint: identity.fingerprint
  })
  let body: HandoffResponse = {}
  for (let attempt = 0; attempt < 3; attempt += 1) {
    if (attempt > 0) {
      await new Promise(resolve => setTimeout(resolve, attempt === 1 ? 50 : 150))
    }
    try {
      const response = await fetch(apiURL(handoffExchangePath()), {
        method: 'POST',
        cache: 'no-store',
        headers: { 'Content-Type': 'application/json' },
        body: requestBody
      })
      body = await response.json().catch(() => ({})) as HandoffResponse
      if (!response.ok) {
        const error = new Error(body.message || `Cloud handoff failed (HTTP ${response.status}).`)
        const actionURL = safeActionURL(body.action_url)
        Object.assign(error, {
          action_url: actionURL,
          retryable: body.retryable === true || response.status >= 500
        })
        throw error
      }
      break
    } catch (error) {
      const retryable = error instanceof TypeError || (error as { retryable?: unknown } | null)?.retryable === true
      if (!retryable || attempt === 2) throw error
    }
  }
  if (!body.phone_id || !body.phone_token || !body.route_handle) {
    throw new Error('Cloud handoff returned incomplete phone credentials.')
  }

  setPhoneId(body.phone_id)
  setPhoneToken(body.phone_token)
  setRouteHandle(body.route_handle)
  localStorage.setItem('nekonest_setup_done', '1')
  clearHandoffFailure()
  return { state: 'exchanged', phoneId: body.phone_id }
}

export async function exchangeHandoffFromFragment(): Promise<HandoffResult> {
  return exchangeHandoffTicket(takeHandoffTicketFromFragment())
}
