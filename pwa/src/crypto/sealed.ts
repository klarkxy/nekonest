/**
 * NekoNest v1 sealed-transport crypto helpers (Web Crypto).
 * Algorithms: AES-256-GCM, HKDF-SHA-256. X25519/Ed25519 require a later polyfill
 * where Web Crypto lacks support; AES/HKDF paths are covered by golden vectors.
 */

const te = new TextEncoder()

export const ALG_AES_256_GCM = 'aes-256-gcm'
export const SEALED_FORMAT_VERSION = 1

function b64urlEncode(buf: ArrayBuffer | Uint8Array): string {
  const bytes = buf instanceof Uint8Array ? buf : new Uint8Array(buf)
  let s = ''
  for (let i = 0; i < bytes.length; i++) s += String.fromCharCode(bytes[i])
  return btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

function b64urlDecode(s: string): Uint8Array {
  const pad = s.length % 4 === 0 ? '' : '='.repeat(4 - (s.length % 4))
  const b64 = s.replace(/-/g, '+').replace(/_/g, '/') + pad
  const bin = atob(b64)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}

export async function importAesKey(raw: Uint8Array): Promise<CryptoKey> {
  return crypto.subtle.importKey('raw', raw, { name: 'AES-GCM' }, false, ['encrypt', 'decrypt'])
}

export async function sealAesGcm(
  keyRaw: Uint8Array,
  nonce: Uint8Array,
  plaintext: Uint8Array,
  aad: Uint8Array
): Promise<Uint8Array> {
  const key = await importAesKey(keyRaw)
  const ct = await crypto.subtle.encrypt(
    { name: 'AES-GCM', iv: nonce, additionalData: aad, tagLength: 128 },
    key,
    plaintext
  )
  return new Uint8Array(ct)
}

export async function openAesGcm(
  keyRaw: Uint8Array,
  nonce: Uint8Array,
  ciphertext: Uint8Array,
  aad: Uint8Array
): Promise<Uint8Array> {
  const key = await importAesKey(keyRaw)
  const pt = await crypto.subtle.decrypt(
    { name: 'AES-GCM', iv: nonce, additionalData: aad, tagLength: 128 },
    key,
    ciphertext
  )
  return new Uint8Array(pt)
}

export async function hkdfSha256(
  secret: Uint8Array,
  salt: Uint8Array,
  info: string,
  length = 32
): Promise<Uint8Array> {
  const base = await crypto.subtle.importKey('raw', secret, 'HKDF', false, ['deriveBits'])
  const bits = await crypto.subtle.deriveBits(
    {
      name: 'HKDF',
      hash: 'SHA-256',
      salt,
      info: te.encode(info)
    },
    base,
    length * 8
  )
  return new Uint8Array(bits)
}

export type AADFields = {
  protocol_version: string
  transport_mode: string
  type: string
  device_id: string
  session_id?: string
  client_msg_id?: string
  key_scope: string
  key_epoch: number
  sender_id: string
  sequence: number
  timestamp: number
}

/** Stable JSON for AAD — field order matches Go struct json tags. */
export function encodeAAD(f: AADFields): Uint8Array {
  const obj: Record<string, unknown> = {
    protocol_version: f.protocol_version,
    transport_mode: f.transport_mode,
    type: f.type,
    device_id: f.device_id
  }
  if (f.session_id) obj.session_id = f.session_id
  if (f.client_msg_id) obj.client_msg_id = f.client_msg_id
  obj.key_scope = f.key_scope
  obj.key_epoch = f.key_epoch
  obj.sender_id = f.sender_id
  obj.sequence = f.sequence
  obj.timestamp = f.timestamp
  return te.encode(JSON.stringify(obj))
}

export { b64urlEncode, b64urlDecode }
