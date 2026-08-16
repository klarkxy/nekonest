import { afterEach, describe, expect, it, vi } from 'vitest'

// urlBase64 helper is private — exercise ensurePushSubscription guards only.
import { ensurePushSubscription } from './push'

describe('ensurePushSubscription', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('returns false without device or PushManager', async () => {
    expect(await ensurePushSubscription('')).toBe(false)
    // jsdom may lack PushManager
    expect(await ensurePushSubscription('dev1')).toBe(false)
  })

  it('does not prompt for notification permission without a user gesture', async () => {
    const requestPermission = vi.fn(async () => 'granted')
    vi.stubGlobal('PushManager', function PushManager() {})
    vi.stubGlobal('Notification', { permission: 'default', requestPermission })
    Object.defineProperty(navigator, 'serviceWorker', {
      configurable: true,
      value: {}
    })

    expect(await ensurePushSubscription('dev1')).toBe(false)
    expect(requestPermission).not.toHaveBeenCalled()
  })
})
