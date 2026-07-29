import type { Device } from '@/types/protocol'

export interface BoundDeviceRef {
  id: string
  name: string
}

/**
 * An absent binding preference keeps the self-hosted server-list fallback.
 * Once the user has configured bindings, an explicit empty list must remain
 * empty instead of exposing every server device again.
 */
export function selectVisibleDevices(
  devices: Device[],
  bound: BoundDeviceRef[],
  bindingConfigured: boolean
): Device[] {
  if (!bindingConfigured) return devices

  const ids = new Set(bound.map(device => device.id))
  const visible = devices.filter(device => ids.has(device.id))
  for (const boundDevice of bound) {
    if (!visible.some(device => device.id === boundDevice.id)) {
      visible.push({
        id: boundDevice.id,
        name: boundDevice.name,
        os: 'windows',
        status: 'offline',
        last_seen: 0,
        active_agents: 0
      })
    }
  }
  return visible
}
