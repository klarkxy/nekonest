import { describe, expect, it } from 'vitest'

// urlBase64 helper is private — exercise ensurePushSubscription guards only.
import { ensurePushSubscription } from './push'

describe('ensurePushSubscription', () => {
  it('returns false without device or PushManager', async () => {
    expect(await ensurePushSubscription('')).toBe(false)
    // jsdom may lack PushManager
    expect(await ensurePushSubscription('dev1')).toBe(false)
  })
})
