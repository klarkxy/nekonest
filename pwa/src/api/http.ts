/** Authenticated fetch helpers for NekoNest REST APIs */

const SECRET_KEY = 'nekonest_phone_secret'

export function getPhoneSecret(): string {
  return localStorage.getItem(SECRET_KEY) || ''
}

export function setPhoneSecret(secret: string) {
  localStorage.setItem(SECRET_KEY, secret)
}

export function clearPhoneSecret() {
  localStorage.removeItem(SECRET_KEY)
}

export function authHeaders(extra?: HeadersInit): Headers {
  const h = new Headers(extra)
  const secret = getPhoneSecret()
  if (secret) {
    h.set('Authorization', `Bearer ${secret}`)
    h.set('X-Neko-Secret', secret)
  }
  if (!h.has('Content-Type')) {
    h.set('Content-Type', 'application/json')
  }
  return h
}

export async function apiFetch(path: string, init: RequestInit = {}): Promise<Response> {
  const headers = authHeaders(init.headers)
  return fetch(path, { ...init, headers })
}
