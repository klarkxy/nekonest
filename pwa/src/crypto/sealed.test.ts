import { describe, expect, it } from 'vitest'
import {
  b64urlDecode,
  b64urlEncode,
  encodeAAD,
  hkdfSha256,
  openAesGcm,
  sealAesGcm
} from './sealed'

describe('sealed crypto', () => {
  it('round-trips AES-GCM with AAD', async () => {
    const key = new Uint8Array(32).map((_, i) => i + 1)
    const nonce = new Uint8Array(12).map((_, i) => i + 10)
    const aad = encodeAAD({
      protocol_version: '1.0',
      transport_mode: 'sealed',
      type: 'send_prompt',
      device_id: 'dev1',
      session_id: 's1',
      client_msg_id: 'c1',
      key_scope: 'session',
      key_epoch: 1,
      sender_id: 'phone1',
      sequence: 7,
      timestamp: 100
    })
    const pt = new TextEncoder().encode('{"prompt":"hi"}')
    const ct = await sealAesGcm(key, nonce, pt, aad)
    const out = await openAesGcm(key, nonce, ct, aad)
    expect(new TextDecoder().decode(out)).toBe('{"prompt":"hi"}')

    const badAad = encodeAAD({
      protocol_version: '1.0',
      transport_mode: 'sealed',
      type: 'session_message',
      device_id: 'dev1',
      session_id: 's1',
      client_msg_id: 'c1',
      key_scope: 'session',
      key_epoch: 1,
      sender_id: 'phone1',
      sequence: 7,
      timestamp: 100
    })
    await expect(openAesGcm(key, nonce, ct, badAad)).rejects.toBeTruthy()
  })

  it('derives HKDF-SHA-256 keys of 32 bytes', async () => {
    const secret = new Uint8Array(32).fill(7)
    const salt = new Uint8Array(32).fill(3)
    const k = await hkdfSha256(secret, salt, 'nekonest-v1-pair-wrap')
    expect(k.length).toBe(32)
    const k2 = await hkdfSha256(secret, salt, 'nekonest-v1-pair-wrap')
    expect(Array.from(k)).toEqual(Array.from(k2))
  })

  it('encodes base64url without padding', () => {
    const raw = new Uint8Array([1, 2, 3, 250])
    const s = b64urlEncode(raw)
    expect(s.includes('=')).toBe(false)
    expect(Array.from(b64urlDecode(s))).toEqual(Array.from(raw))
  })
})
