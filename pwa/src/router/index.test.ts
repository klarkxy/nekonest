import { beforeEach, describe, expect, it } from 'vitest'
import { setPhoneSecret, setPhoneToken, clearPhoneSecret, clearPhoneIdentity } from '../api/http'
import { privateRouteNeedsCredential } from './index'
import { createMemoryHistory, createRouter } from 'vue-router'
import {
  deviceDetailLocation,
  sessionDetailLocation
} from './navigation'
import { appRoutes } from './routes'

describe('named route parameter encoding', () => {
  const deviceId = 'device/with #hash %percent ?query'
  const sessionId = 'thread/with #hash %percent ?query'
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: '/device/:deviceId',
        name: 'device-detail',
        component: {}
      },
      {
        path: '/device/:deviceId/session/:sessionId',
        name: 'session-detail',
        component: {}
      }
    ]
  })

  it('round-trips a device id containing reserved URL characters', () => {
    const resolved = router.resolve(deviceDetailLocation(deviceId))

    expect(resolved.href).toBe(
      '/device/device%2Fwith%20%23hash%20%25percent%20%3Fquery'
    )
    expect(router.resolve(resolved.href).params.deviceId).toBe(deviceId)
  })

  it('round-trips device and thread ids containing reserved URL characters', () => {
    const resolved = router.resolve(sessionDetailLocation(deviceId, sessionId))

    expect(resolved.href).toBe(
      '/device/device%2Fwith%20%23hash%20%25percent%20%3Fquery' +
      '/session/thread%2Fwith%20%23hash%20%25percent%20%3Fquery'
    )
    const roundTrip = router.resolve(resolved.href)
    expect(roundTrip.name).toBe('session-detail')
    expect(roundTrip.params.deviceId).toBe(deviceId)
    expect(roundTrip.params.sessionId).toBe(sessionId)
  })

  it('redirects a legacy new-session bookmark to the same device workspace', async () => {
    const legacyRouter = createRouter({
      history: createMemoryHistory(),
      routes: appRoutes
    })
    await legacyRouter.push(
      `/device/${encodeURIComponent(deviceId)}/new-session`
    )

    expect(legacyRouter.currentRoute.value.name).toBe('device-detail')
    expect(legacyRouter.currentRoute.value.params.deviceId).toBe(deviceId)
  })
})

describe('privateRouteNeedsCredential', () => {
  beforeEach(() => {
    localStorage.clear()
    clearPhoneSecret()
    clearPhoneIdentity()
  })

  it('requires a stored phone token or admin secret for private routes', () => {
    localStorage.setItem('nekonest_setup_done', '1')
    expect(privateRouteNeedsCredential(true)).toBe(false)
    expect(privateRouteNeedsCredential(false)).toBe(true)
    setPhoneSecret('admin-secret')
    expect(privateRouteNeedsCredential(false)).toBe(false)
    clearPhoneSecret()
    setPhoneToken('phone-token')
    expect(privateRouteNeedsCredential(false)).toBe(false)
  })
})
