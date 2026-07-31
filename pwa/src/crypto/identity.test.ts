import { describe, expect, it } from 'vitest'
import {
  buildPairTranscript,
  generatePhoneIdentity,
  parsePairQRPayload,
  shortFingerprint,
  signTranscript,
  verifyTranscript
} from './identity'

describe('phone identity', () => {
  it('generates fingerprint and signs transcripts', () => {
    const id = generatePhoneIdentity()
    expect(id.fingerprint).toHaveLength(64)
    expect(id.ed25519_public.length).toBeGreaterThan(20)
    expect(id.x25519_public.length).toBeGreaterThan(20)

    const transcript = buildPairTranscript({
      code: 'abc123',
      device_id: 'dev1',
      daemon_ed25519: 'd-ed',
      daemon_x25519: 'd-x',
      phone_ed25519: id.ed25519_public,
      phone_x25519: id.x25519_public
    })
    const sig = signTranscript(id.ed25519_private, transcript)
    expect(verifyTranscript(id.ed25519_public, transcript, sig)).toBe(true)
    expect(verifyTranscript(id.ed25519_public, new TextEncoder().encode('nope'), sig)).toBe(false)
  })

  it('parses pair QR JSON and rejects bare codes', () => {
    expect(parsePairQRPayload('a1b2c3')).toBeNull()
    const qr = parsePairQRPayload(
      JSON.stringify({
        v: '1',
        relay_url: 'https://nest.example',
        device_id: 'dev1',
        code: 'abcdef',
        expires_at: 1,
        ed25519_public: 'e',
        x25519_public: 'x',
        identity_fingerprint: 'f'.repeat(64)
      })
    )
    expect(qr?.code).toBe('abcdef')
    expect(shortFingerprint(qr!.identity_fingerprint, 8)).toHaveLength(8)
  })
})
