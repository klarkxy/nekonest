/** Authenticated fetch helpers for NekoNest REST APIs */

import { apiURL, endpointOrigin, isManagedRuntime } from '@/config/runtimeEndpoint'

const ADMIN_SECRET_KEY = 'nekonest_phone_secret' // legacy key name; stores admin secret
const PHONE_TOKEN_KEY = 'nekonest_phone_token'
const PHONE_ID_KEY = 'nekonest_phone_id'
const ROUTE_HANDLE_KEY = 'nekonest_route_handle'

function scopedKey(key: string): string {
  return `${key}:${encodeURIComponent(endpointOrigin())}`
}

function readScoped(key: string): string {
  const scoped = localStorage.getItem(scopedKey(key)) || ''
  if (scoped) return scoped
  // One-way compatibility bridge for existing v0.2.5 same-origin installs.
  // A managed origin never imports an unscoped credential from another nest.
  if (isManagedRuntime()) return ''
  const legacy = localStorage.getItem(key) || ''
  if (legacy) localStorage.setItem(scopedKey(key), legacy)
  return legacy
}

function writeScoped(key: string, value: string) {
  if (value) localStorage.setItem(scopedKey(key), value)
  else localStorage.removeItem(scopedKey(key))
}

/** Nest admin secret (bootstrap). Prefer phone token for day-to-day API calls. */
export function getPhoneSecret(): string {
  return readScoped(ADMIN_SECRET_KEY)
}

export function setPhoneSecret(secret: string) {
  writeScoped(ADMIN_SECRET_KEY, secret)
}

export function clearPhoneSecret() {
  localStorage.removeItem(scopedKey(ADMIN_SECRET_KEY))
  localStorage.removeItem(ADMIN_SECRET_KEY)
}

/** Independent phone identity token (v1). */
export function getPhoneToken(): string {
  return readScoped(PHONE_TOKEN_KEY)
}

export function setPhoneToken(token: string) {
  writeScoped(PHONE_TOKEN_KEY, token)
}

export function getPhoneId(): string {
  return readScoped(PHONE_ID_KEY)
}

export function setPhoneId(id: string) {
  writeScoped(PHONE_ID_KEY, id)
}

export function getRouteHandle(): string {
  return readScoped(ROUTE_HANDLE_KEY)
}

export function setRouteHandle(handle: string) {
  writeScoped(ROUTE_HANDLE_KEY, handle)
}

export function clearPhoneIdentity() {
  localStorage.removeItem(scopedKey(PHONE_TOKEN_KEY))
  localStorage.removeItem(scopedKey(PHONE_ID_KEY))
  localStorage.removeItem(scopedKey(ROUTE_HANDLE_KEY))
  localStorage.removeItem(PHONE_TOKEN_KEY)
  localStorage.removeItem(PHONE_ID_KEY)
  localStorage.removeItem(ROUTE_HANDLE_KEY)
}

/** Credential used for Authorization: prefer phone token, fall back to admin secret. */
export function getAuthCredential(): string {
  return getPhoneToken() || getPhoneSecret()
}

export function authHeaders(extra?: HeadersInit): Headers {
  const h = new Headers(extra)
  const phoneToken = getPhoneToken()
  const adminSecret = getPhoneSecret()
  const routeHandle = getRouteHandle()
  if (phoneToken) {
    h.set('Authorization', `Bearer ${phoneToken}`)
    h.set('X-Neko-Phone-Token', phoneToken)
  } else if (adminSecret) {
    h.set('Authorization', `Bearer ${adminSecret}`)
    h.set('X-Neko-Secret', adminSecret)
  }
  if (routeHandle) h.set('X-Neko-Route-Handle', routeHandle)
  if (!h.has('Content-Type')) {
    h.set('Content-Type', 'application/json')
  }
  return h
}

export async function apiFetch(path: string, init: RequestInit = {}): Promise<Response> {
  const headers = authHeaders(init.headers)
  return fetch(apiURL(path), { ...init, headers, cache: 'no-store' })
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
