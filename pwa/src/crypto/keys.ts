/**
 * Phone-side content key store (catalog + session) after pair unwrap.
 */
import { b64urlDecode, b64urlEncode, openAesGcm, sealAesGcm, encodeAAD, type AADFields } from './sealed'
import {
  buildPairTranscript,
  derivePairWrapKey,
  loadOrCreatePhoneIdentity,
  type PairQRPayload
} from './identity'
import type { SealedPayload } from '@/types/protocol'
import { apiFetch } from '@/api/http'

const IDB_NAME = 'nekonest-crypto'
const IDB_STORE = 'content_keys'

export type StoredContentKey = {
  deviceId: string
  scope: 'device_catalog' | 'session'
  sessionId: string
  epoch: number
  keyB64: string
}

function openDB(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(IDB_NAME, 2)
    req.onupgradeneeded = () => {
      const db = req.result
      if (!db.objectStoreNames.contains('identity')) {
        db.createObjectStore('identity')
      }
      if (!db.objectStoreNames.contains(IDB_STORE)) {
        db.createObjectStore(IDB_STORE)
      }
    }
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => reject(req.error)
  })
}

function keyId(k: Pick<StoredContentKey, 'deviceId' | 'scope' | 'sessionId' | 'epoch'>): string {
  return `${k.deviceId}|${k.scope}|${k.sessionId || ''}|${k.epoch}`
}

export async function saveContentKey(k: StoredContentKey): Promise<void> {
  const db = await openDB()
  await new Promise<void>((resolve, reject) => {
    const tx = db.transaction(IDB_STORE, 'readwrite')
    tx.objectStore(IDB_STORE).put(k, keyId(k))
    tx.oncomplete = () => resolve()
    tx.onerror = () => reject(tx.error)
  })
  db.close()
}

export async function getContentKey(
  deviceId: string,
  scope: 'device_catalog' | 'session',
  sessionId = '',
  epoch?: number
): Promise<Uint8Array | null> {
  const match = await getContentKeyRecord(deviceId, scope, sessionId, epoch)
  return match ? b64urlDecode(match.keyB64) : null
}

async function getContentKeyRecord(
  deviceId: string,
  scope: 'device_catalog' | 'session',
  sessionId = '',
  epoch?: number
): Promise<StoredContentKey | null> {
  const db = await openDB()
  const all = await new Promise<StoredContentKey[]>((resolve, reject) => {
    const tx = db.transaction(IDB_STORE, 'readonly')
    const req = tx.objectStore(IDB_STORE).getAll()
    req.onsuccess = () => resolve((req.result || []) as StoredContentKey[])
    req.onerror = () => reject(req.error)
  })
  db.close()
  const matches = all.filter(
    k => k.deviceId === deviceId && k.scope === scope && (k.sessionId || '') === (sessionId || '')
  )
  if (!matches.length) return null
  matches.sort((a, b) => b.epoch - a.epoch)
  return epoch != null ? matches.find(m => m.epoch === epoch) || null : matches[0]
}

async function unwrapPackage(
  wrapKey: Uint8Array,
  nonceB64: string,
  wrappedB64: string
): Promise<Uint8Array> {
  const nonce = b64urlDecode(nonceB64)
  const ct = b64urlDecode(wrappedB64)
  const aad = new TextEncoder().encode('nekonest-v1-key-package')
  return openAesGcm(wrapKey, nonce, ct, aad)
}

/** After pair: derive wrap key from QR + phone identity, fetch packages, unwrap. */
export async function completePairKeySetup(opts: {
  code: string
  deviceId: string
  daemonEd25519: string
  daemonX25519: string
  qr?: PairQRPayload | null
}): Promise<void> {
  const phone = await loadOrCreatePhoneIdentity()
  const transcript = buildPairTranscript({
    code: opts.code,
    device_id: opts.deviceId,
    daemon_ed25519: opts.daemonEd25519,
    daemon_x25519: opts.daemonX25519,
    phone_ed25519: phone.ed25519_public,
    phone_x25519: phone.x25519_public
  })
  const wrapKey = await derivePairWrapKey(phone.x25519_private, opts.daemonX25519, transcript)

  // Store wrap key as a special catalog helper (epoch 0) for later session wraps.
  await saveContentKey({
    deviceId: opts.deviceId,
    scope: 'device_catalog',
    sessionId: '__wrap__',
    epoch: 0,
    keyB64: b64urlEncode(wrapKey)
  })

  // Fetch packages (daemon may upload slightly later — retry a few times).
  for (let i = 0; i < 5; i++) {
    const res = await apiFetch(`/api/keys?device_id=${encodeURIComponent(opts.deviceId)}`)
    if (res.ok) {
      const data = (await res.json()) as {
        packages?: Array<{
          scope: string
          session_id?: string
          epoch: number
          wrapped_key: string
          nonce: string
        }>
      }
      for (const p of data.packages || []) {
        try {
          const raw = await unwrapPackage(wrapKey, p.nonce, p.wrapped_key)
          await saveContentKey({
            deviceId: opts.deviceId,
            scope: (p.scope as 'device_catalog' | 'session') || 'device_catalog',
            sessionId: p.session_id || '',
            epoch: p.epoch,
            keyB64: b64urlEncode(raw)
          })
        } catch {
          /* skip bad package */
        }
      }
      if ((data.packages || []).length) return
    }
    await new Promise(r => setTimeout(r, 400 * (i + 1)))
  }
}

export async function ingestKeyPackageMessage(deviceId: string, payload: Record<string, unknown>): Promise<void> {
  const wrap = await getContentKey(deviceId, 'device_catalog', '__wrap__', 0)
  if (!wrap) return
  const scope = String(payload.scope || 'device_catalog') as 'device_catalog' | 'session'
  const sessionId = String(payload.session_id || '')
  const epoch = Number(payload.epoch || 1)
  const wrapped = String(payload.wrapped_key || '')
  const nonce = String(payload.nonce || '')
  if (!wrapped || !nonce) return
  try {
    const raw = await unwrapPackage(wrap, nonce, wrapped)
    await saveContentKey({
      deviceId,
      scope,
      sessionId,
      epoch,
      keyB64: b64urlEncode(raw)
    })
  } catch {
    /* ignore */
  }
}

export async function decryptSealedPayload(
  deviceId: string,
  sessionId: string | undefined,
  sealed: SealedPayload,
  aad: AADFields
): Promise<unknown | null> {
  const scope = sealed.key_scope === 'session' ? 'session' : 'device_catalog'
  const key = await getContentKey(deviceId, scope, scope === 'session' ? sessionId || '' : '', sealed.epoch)
  if (!key) return null
  try {
    const nonce = b64urlDecode(sealed.nonce)
    const ct = b64urlDecode(sealed.ciphertext)
    const aadBytes = encodeAAD(aad)
    const pt = await openAesGcm(key, nonce, ct, aadBytes)
    return JSON.parse(new TextDecoder().decode(pt))
  } catch {
    return null
  }
}

export async function encryptSessionPayload(
  deviceId: string,
  sessionId: string,
  senderId: string,
  type: string,
  payload: unknown,
  clientMsgId?: string,
  timestamp?: number
): Promise<SealedPayload | null> {
  const sessionKey = await getContentKeyRecord(deviceId, 'session', sessionId)
  if (!sessionKey) {
    // Fall back to catalog key if session key not yet distributed.
    const cat = await getContentKeyRecord(deviceId, 'device_catalog', '')
    if (!cat) return null
    return sealWithKey(b64urlDecode(cat.keyB64), 'device_catalog', cat.epoch, deviceId, sessionId, senderId, type, payload, clientMsgId, timestamp)
  }
  return sealWithKey(b64urlDecode(sessionKey.keyB64), 'session', sessionKey.epoch, deviceId, sessionId, senderId, type, payload, clientMsgId, timestamp)
}

async function sealWithKey(
  key: Uint8Array,
  scope: 'device_catalog' | 'session',
  epoch: number,
  deviceId: string,
  sessionId: string,
  senderId: string,
  type: string,
  payload: unknown,
  clientMsgId?: string,
  timestamp?: number
): Promise<SealedPayload> {
  const seq = Date.now() % 1_000_000_000
  const ts = timestamp ?? Math.floor(Date.now() / 1000)
  const aad: AADFields = {
    protocol_version: '1.1',
    transport_mode: 'sealed',
    type,
    device_id: deviceId,
    session_id: sessionId || undefined,
    client_msg_id: clientMsgId,
    key_scope: scope,
    key_epoch: epoch,
    sender_id: senderId,
    sequence: seq,
    timestamp: ts
  }
  const nonce = crypto.getRandomValues(new Uint8Array(12))
  const pt = new TextEncoder().encode(JSON.stringify(payload))
  const ct = await sealAesGcm(key, nonce, pt, encodeAAD(aad))
  return {
    alg: 'aes-256-gcm',
    version: 1,
    key_scope: scope,
    epoch,
    sender_id: senderId,
    recipient_id: deviceId,
    sequence: seq,
    nonce: b64urlEncode(nonce),
    ciphertext: b64urlEncode(ct)
  }
}
