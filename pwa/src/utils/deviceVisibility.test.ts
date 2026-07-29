import { describe, expect, it } from 'vitest'
import type { Device } from '@/types/protocol'
import { selectVisibleDevices } from './deviceVisibility'

const devices: Device[] = [
  {
    id: 'device-a',
    name: 'A',
    os: 'windows',
    status: 'online',
    last_seen: 2,
    active_agents: 1
  },
  {
    id: 'device-b',
    name: 'B',
    os: 'windows',
    status: 'online',
    last_seen: 1,
    active_agents: 0
  }
]

describe('device visibility', () => {
  it('uses the server list only before bindings are configured', () => {
    expect(selectVisibleDevices(devices, [], false)).toEqual(devices)
    expect(selectVisibleDevices(devices, [], true)).toEqual([])
  })

  it('filters bound devices and preserves missing ones as offline', () => {
    const result = selectVisibleDevices(
      devices,
      [
        { id: 'device-b', name: 'B' },
        { id: 'device-c', name: 'C' }
      ],
      true
    )

    expect(result.map(device => [device.id, device.status])).toEqual([
      ['device-b', 'online'],
      ['device-c', 'offline']
    ])
    expect(devices).toHaveLength(2)
  })
})
