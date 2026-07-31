/**
 * Phone-side long-term E2E identity (Ed25519 + X25519) using @noble/curves.
 */
import { ed25519, x25519 } from '@noble/curves/ed25519.js'
import { sha256 } from '@noble/hashes/sha2.js'
import { b64urlDecode, b64urlEncode, hkdfSha256 } from './sealed'

const IDB_NAME = 'nekonest-crypto'
const IDB_STORE = 'identity'
const IDB_KEY = 'phone_v1'

export type PhoneIdentityPublic = {
  ed25519_public: string
  x25519_public: string
  fingerprint: string
}

export type PhoneIdentity = PhoneIdentityPublic & {
  ed25519_private: string
  x25519_private: string
}

function fingerprint(edPub: Uint8Array, xPub: Uint8Array): string {
  const h = sha256.create()
  h.update(edPub)
  h.update(xPub)
  return Array.from(h.digest())
    .map(b => b.toString(16).padStart(2, '0'))
    .join('')
}

export function generatePhoneIdentity(): PhoneIdentity {
  const ed = ed25519.keygen()
  const x = x25519.keygen()
  return {
    ed25519_private: b64urlEncode(ed.secretKey),
    ed25519_public: b64urlEncode(ed.publicKey),
    x25519_private: b64urlEncode(x.secretKey),
    x25519_public: b64urlEncode(x.publicKey),
    fingerprint: fingerprint(ed.publicKey, x.publicKey)
  }
}

function openDB(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(IDB_NAME, 2)
    req.onupgradeneeded = () => {
      const db = req.result
      if (!db.objectStoreNames.contains(IDB_STORE)) {
        db.createObjectStore(IDB_STORE)
      }
      if (!db.objectStoreNames.contains('content_keys')) {
        db.createObjectStore('content_keys')
      }
    }
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => reject(req.error)
  })
}

export async function loadOrCreatePhoneIdentity(): Promise<PhoneIdentity> {
  try {
    const db = await openDB()
    const existing = await new Promise<PhoneIdentity | undefined>((resolve, reject) => {
      const tx = db.transaction(IDB_STORE, 'readonly')
      const store = tx.objectStore(IDB_STORE)
      const g = store.get(IDB_KEY)
      g.onsuccess = () => resolve(g.result as PhoneIdentity | undefined)
      g.onerror = () => reject(g.error)
    })
    db.close()
    if (existing?.ed25519_private && existing.fingerprint) return existing
  } catch {
    /* fall through */
  }
  const id = generatePhoneIdentity()
  await savePhoneIdentity(id)
  return id
}

export async function savePhoneIdentity(id: PhoneIdentity): Promise<void> {
  const db = await openDB()
  await new Promise<void>((resolve, reject) => {
    const tx = db.transaction(IDB_STORE, 'readwrite')
    tx.objectStore(IDB_STORE).put(id, IDB_KEY)
    tx.oncomplete = () => resolve()
    tx.onerror = () => reject(tx.error)
  })
  db.close()
}

/** Pair QR payload printed by daemon -pair gen */
export type PairQRPayload = {
  v: string
  relay_url: string
  device_id: string
  name?: string
  code: string
  expires_at: number
  ed25519_public: string
  x25519_public: string
  identity_fingerprint: string
  transport_mode?: string
}

export function parsePairQRPayload(raw: string): PairQRPayload | null {
  const t = raw.trim()
  if (!t) return null
  if (/^[0-9a-fA-F]{6}$/.test(t)) return null
  try {
    const obj = JSON.parse(t) as PairQRPayload
    if (!obj.code || !obj.device_id) return null
    return obj
  } catch {
    return null
  }
}

export function shortFingerprint(fp: string, n = 16): string {
  if (!fp) return ''
  return fp.slice(0, n)
}

export async function derivePairWrapKey(
  phoneX25519PrivateB64: string,
  daemonX25519PublicB64: string,
  transcript: Uint8Array
): Promise<Uint8Array> {
  const phonePriv = b64urlDecode(phoneX25519PrivateB64)
  const daemonPub = b64urlDecode(daemonX25519PublicB64)
  const shared = x25519.getSharedSecret(phonePriv, daemonPub)
  const salt = sha256(transcript)
  return hkdfSha256(shared, salt, 'nekonest-v1-pair-wrap')
}

export function buildPairTranscript(parts: {
  code: string
  device_id: string
  daemon_ed25519: string
  daemon_x25519: string
  phone_ed25519: string
  phone_x25519: string
}): Uint8Array {
  const s = [
    'nekonest-pair-v1',
    parts.code,
    parts.device_id,
    parts.daemon_ed25519,
    parts.daemon_x25519,
    parts.phone_ed25519,
    parts.phone_x25519
  ].join('|')
  return new TextEncoder().encode(s)
}

export function signTranscript(ed25519PrivateB64: string, transcript: Uint8Array): string {
  const priv = b64urlDecode(ed25519PrivateB64)
  const sig = ed25519.sign(transcript, priv)
  return b64urlEncode(sig)
}

export function verifyTranscript(
  ed25519PublicB64: string,
  transcript: Uint8Array,
  sigB64: string
): boolean {
  try {
    const pub = b64urlDecode(ed25519PublicB64)
    const sig = b64urlDecode(sigB64)
    return ed25519.verify(sig, transcript, pub)
  } catch {
    return false
  }
}
