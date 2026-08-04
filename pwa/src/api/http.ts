/** Authenticated fetch helpers for NekoNest REST APIs */

const ADMIN_SECRET_KEY = 'nekonest_phone_secret' // legacy key name; stores admin secret
const PHONE_TOKEN_KEY = 'nekonest_phone_token'
const PHONE_ID_KEY = 'nekonest_phone_id'

/** Nest admin secret (bootstrap). Prefer phone token for day-to-day API calls. */
export function getPhoneSecret(): string {
  return localStorage.getItem(ADMIN_SECRET_KEY) || ''
}

export function setPhoneSecret(secret: string) {
  localStorage.setItem(ADMIN_SECRET_KEY, secret)
}

export function clearPhoneSecret() {
  localStorage.removeItem(ADMIN_SECRET_KEY)
}

/** Independent phone identity token (v1). */
export function getPhoneToken(): string {
  return localStorage.getItem(PHONE_TOKEN_KEY) || ''
}

export function setPhoneToken(token: string) {
  if (token) localStorage.setItem(PHONE_TOKEN_KEY, token)
  else localStorage.removeItem(PHONE_TOKEN_KEY)
}

export function getPhoneId(): string {
  return localStorage.getItem(PHONE_ID_KEY) || ''
}

export function setPhoneId(id: string) {
  if (id) localStorage.setItem(PHONE_ID_KEY, id)
  else localStorage.removeItem(PHONE_ID_KEY)
}

export function clearPhoneIdentity() {
  localStorage.removeItem(PHONE_TOKEN_KEY)
  localStorage.removeItem(PHONE_ID_KEY)
}

/** Credential used for Authorization: prefer phone token, fall back to admin secret. */
export function getAuthCredential(): string {
  return getPhoneToken() || getPhoneSecret()
}

export function authHeaders(extra?: HeadersInit): Headers {
  const h = new Headers(extra)
  const phoneToken = getPhoneToken()
  const adminSecret = getPhoneSecret()
  if (phoneToken) {
    h.set('Authorization', `Bearer ${phoneToken}`)
    h.set('X-Neko-Phone-Token', phoneToken)
  } else if (adminSecret) {
    h.set('Authorization', `Bearer ${adminSecret}`)
    h.set('X-Neko-Secret', adminSecret)
  }
  if (!h.has('Content-Type')) {
    h.set('Content-Type', 'application/json')
  }
  return h
}

export async function apiFetch(path: string, init: RequestInit = {}): Promise<Response> {
  const headers = authHeaders(init.headers)
  return fetch(path, { ...init, headers, cache: 'no-store' })
}

/** Bootstrap a phone identity using the admin nest secret. */
export async function bootstrapPhoneIdentity(name = 'Phone'): Promise<{
  phone_id: string
  token: string
}> {
  const res = await apiFetch('/api/phones/bootstrap', {
    method: 'POST',
    body: JSON.stringify({ name })
  })
  if (!res.ok) {
    throw new Error(`bootstrap failed ${res.status}`)
  }
  const data = (await res.json()) as { phone_id: string; token: string }
  setPhoneId(data.phone_id)
  setPhoneToken(data.token)
  return data
}
