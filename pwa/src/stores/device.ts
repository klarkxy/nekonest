import { ref } from 'vue'
import { defineStore } from 'pinia'
import type { Device } from '@/types/protocol'
import { nekoWS } from '@/api/websocket'
import { apiFetch } from '@/api/http'
import { useBindingStore } from './binding'

export const useDeviceStore = defineStore('devices', () => {
  const devices = ref<Device[]>([])
  const loading = ref(false)
  const connected = ref(false)
  const authError = ref(false)

  const HANDLER_ID = 'device-store'

  function initWebSocket() {
    const ws = nekoWS()
    const binding = useBindingStore()

    ws.removeHandler(HANDLER_ID)
    ws.addHandler(HANDLER_ID, (msg) => {
      if (msg.type === 'device_list') {
        devices.value = (msg.payload?.devices as Device[]) || []
      } else if (msg.type === 'device_online' || msg.type === 'device_offline') {
        const deviceId = (msg.payload?.device_id as string) || msg.device_id
        const device = devices.value.find(d => d.id === deviceId)
        if (device) {
          device.status = msg.type === 'device_online' ? 'online' : 'offline'
        }
      }
    })

    ws.onStatusChange(HANDLER_ID, (status) => {
      connected.value = status === 'connected'
      authError.value = status === 'auth_error'
    })

    // Connect once we have a device to subscribe to
    const target =
      binding.lastDeviceId ||
      binding.bound[0]?.id ||
      devices.value[0]?.id
    if (target) {
      ws.subscribe(target)
    }
  }

  async function fetchDevices() {
    loading.value = true
    authError.value = false
    try {
      const res = await apiFetch('/api/devices')
      if (res.status === 401) {
        authError.value = true
        devices.value = []
        return
      }
      if (!res.ok) {
        console.error('failed to fetch devices:', res.status)
        return
      }
      const data = await res.json()
      devices.value = data.devices || []

      // Auto-subscribe to last / first device for live updates
      const binding = useBindingStore()
      const target =
        binding.lastDeviceId ||
        binding.bound[0]?.id ||
        devices.value[0]?.id
      if (target) {
        nekoWS().subscribe(target)
        binding.setLastDevice(target)
      }
    } catch (err) {
      console.error('failed to fetch devices:', err)
    } finally {
      loading.value = false
    }
  }

  return { devices, loading, connected, authError, initWebSocket, fetchDevices }
})
