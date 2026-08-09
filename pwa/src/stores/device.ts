import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import type { Device } from '@/types/protocol'
import { nekoWS } from '@/api/websocket'
import { apiFetch } from '@/api/http'
import { APP_VERSION } from '@/config/version'
import { tGlobal } from '@/i18n'
import { componentVersionStatus } from '@/utils/componentVersions'
import { useBindingStore } from './binding'
import {
  acknowledgeOpenTransport,
  openTransportConsentRequired
} from '@/api/transport'

export const useDeviceStore = defineStore('devices', () => {
  const devices = ref<Device[]>([])
  const loading = ref(false)
  const loaded = ref(false)
  const loadError = ref('')
  const connected = ref(false)
  const authError = ref(false)
  const transportError = ref('')
  const needsOpenTransportConsent = ref(false)
  const serverVersion = ref('')

  const versionStatus = computed(() => componentVersionStatus({
    frontend: APP_VERSION,
    server: serverVersion.value
  }))

  const HANDLER_ID = 'device-store'

  function initWebSocket() {
    const ws = nekoWS()
    const binding = useBindingStore()

    ws.removeHandler(HANDLER_ID)
    ws.addHandler(HANDLER_ID, (msg) => {
      if (msg.type === 'device_list') {
        devices.value = (msg.payload?.devices as Device[]) || []
      } else if (msg.type === 'subscribe_ack') {
        const reportedServer = msg.payload?.server_version
        if (typeof reportedServer === 'string') {
          serverVersion.value = reportedServer
        }
        const reportedDaemon = msg.payload?.daemon_version
        const device = devices.value.find(d => d.id === msg.device_id)
        if (device && typeof reportedDaemon === 'string') {
          device.daemon_version = reportedDaemon || undefined
        }
      } else if (msg.type === 'device_online' || msg.type === 'device_offline') {
        const deviceId = (msg.payload?.device_id as string) || msg.device_id
        const device = devices.value.find(d => d.id === deviceId)
        if (device) {
          device.status = msg.type === 'device_online' ? 'online' : 'offline'
          const reportedDaemon = msg.payload?.daemon_version
          device.daemon_version = msg.type === 'device_online' && typeof reportedDaemon === 'string'
            ? reportedDaemon || undefined
            : undefined
        }
      }
    })

    ws.onStatusChange(HANDLER_ID, (status) => {
      connected.value = status === 'connected'
      authError.value = status === 'auth_error'
      transportError.value = status === 'transport_error' ? ws.getTransportError() : ''
      needsOpenTransportConsent.value = status === 'transport_error' && openTransportConsentRequired()
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

  function confirmOpenTransport() {
    acknowledgeOpenTransport()
    transportError.value = ''
    needsOpenTransportConsent.value = false
    nekoWS().connect()
  }

  async function fetchDevices() {
    loading.value = true
    loadError.value = ''
    authError.value = false
    transportError.value = ''
    try {
      const res = await apiFetch('/api/devices')
      if (res.status === 401) {
        authError.value = true
        loaded.value = false
        devices.value = []
        return
      }
      if (!res.ok) {
        loadError.value = tGlobal('errors.deviceServerStatus', { status: res.status })
        console.error('failed to fetch devices:', res.status)
        return
      }
      const data = await res.json()
      devices.value = data.devices || []
      serverVersion.value = typeof data.server_version === 'string' ? data.server_version : ''
      loaded.value = true

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
      loadError.value = tGlobal('errors.deviceNetwork')
      console.error('failed to fetch devices:', err)
    } finally {
      loading.value = false
    }
  }

  return {
    devices,
    loading,
    loaded,
    loadError,
    connected,
    authError,
    transportError,
    needsOpenTransportConsent,
    frontendVersion: APP_VERSION,
    serverVersion,
    versionStatus,
    initWebSocket,
    confirmOpenTransport,
    fetchDevices
  }
})
