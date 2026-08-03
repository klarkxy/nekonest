import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import type { NekoMessage } from '@/types/protocol'

const harness = vi.hoisted(() => ({
  subscribedDevice: null as string | null,
  status: 'disconnected' as 'connecting' | 'connected' | 'disconnected' | 'auth_error',
  handlers: new Map<string, (msg: NekoMessage) => void>(),
  statusHandlers: new Map<string, (status: 'connecting' | 'connected' | 'disconnected' | 'auth_error') => void>()
}))

vi.mock('@/api/http', () => ({
  apiFetch: vi.fn()
}))

vi.mock('@/api/websocket', () => {
  const socket = {
    subscribe(deviceId: string) {
      harness.subscribedDevice = deviceId
    },
    addHandler(id: string, handler: (msg: NekoMessage) => void) {
      harness.handlers.set(id, handler)
    },
    removeHandler(id: string) {
      harness.handlers.delete(id)
    },
    onStatusChange(
      id: string,
      handler: (status: 'connecting' | 'connected' | 'disconnected' | 'auth_error') => void
    ) {
      harness.statusHandlers.set(id, handler)
      handler(harness.status)
    }
  }
  return { nekoWS: () => socket }
})

import { apiFetch } from '@/api/http'
import { useBindingStore } from './binding'
import { useDeviceStore } from './device'

const fetchMock = vi.mocked(apiFetch)

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' }
  })
}

describe('device store loading states', () => {
  beforeEach(() => {
    localStorage.clear()
    harness.subscribedDevice = null
    harness.status = 'disconnected'
    harness.handlers.clear()
    harness.statusHandlers.clear()
    fetchMock.mockReset()
    setActivePinia(createPinia())
  })

  it('marks an empty device response as a successful server connection', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ devices: [] }))
    const store = useDeviceStore()

    await store.fetchDevices()

    expect(store.loaded).toBe(true)
    expect(store.loadError).toBe('')
    expect(store.authError).toBe(false)
    expect(store.devices).toEqual([])
  })

  it('surfaces an HTTP failure without pretending the device list is empty', async () => {
    fetchMock.mockResolvedValue(new Response('', { status: 503 }))
    const store = useDeviceStore()

    await store.fetchDevices()

    expect(store.loaded).toBe(false)
    expect(store.loadError).toContain('503')
    expect(store.devices).toEqual([])
  })

  it('surfaces an unreachable server as a recoverable load error', async () => {
    fetchMock.mockRejectedValue(new TypeError('network down'))
    const store = useDeviceStore()

    await store.fetchDevices()

    expect(store.loaded).toBe(false)
    expect(store.loadError.length).toBeGreaterThan(0)
    expect(store.loadError).toMatch(/nest server|猫窝服务器/i)
    expect(store.loading).toBe(false)
  })

  it('keeps authentication failures distinct from network failures', async () => {
    fetchMock.mockResolvedValue(new Response('', { status: 401 }))
    const store = useDeviceStore()

    await store.fetchDevices()

    expect(store.authError).toBe(true)
    expect(store.loaded).toBe(false)
    expect(store.loadError).toBe('')
  })

  it('subscribes to the first returned device for live updates', async () => {
    fetchMock.mockResolvedValue(jsonResponse({
      server_version: '0.3.0',
      devices: [{
        id: 'device-a',
        name: '书房电脑',
        os: 'windows',
        status: 'online',
        last_seen: 1,
        active_agents: 2
      }]
    }))
    const store = useDeviceStore()

    await store.fetchDevices()

    expect(harness.subscribedDevice).toBe('device-a')
    expect(useBindingStore().lastDeviceId).toBe('device-a')
    expect(store.serverVersion).toBe('0.3.0')
    expect(store.versionStatus.refreshRequired).toBe(true)
  })

  it('tracks the selected daemon version from the subscribe acknowledgement', async () => {
    fetchMock.mockResolvedValue(jsonResponse({
      server_version: '0.2.0',
      devices: [{
        id: 'device-a',
        name: '书房电脑',
        os: 'windows',
        status: 'online',
        last_seen: 1,
        active_agents: 2
      }]
    }))
    const store = useDeviceStore()
    store.initWebSocket()
    await store.fetchDevices()

    harness.handlers.get('device-store')?.({
      type: 'subscribe_ack',
      device_id: 'device-a',
      timestamp: 1,
      payload: {
        server_version: '0.2.0',
        daemon_version: '0.1.0'
      }
    })

    expect(store.activeDaemonVersion).toBe('0.1.0')
    expect(store.versionStatus.daemonUpdateRequired).toBe(true)
  })
})
