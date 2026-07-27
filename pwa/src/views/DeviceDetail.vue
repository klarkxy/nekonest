<template>
  <div class="device-detail-page">
    <div class="page-header">
      <n-button text @click="$router.back()">← 返回</n-button>
      <h1>{{ device?.name || deviceId }}</h1>
      <span v-if="device" class="status-dot" :class="device.status"></span>
    </div>

    <div class="hero-banner neko-card">
      <div class="hero-cat" aria-hidden="true">
        <img src="/neko-avatar.webp" alt="" width="72" height="72" />
      </div>
      <div class="hero-text">
        <div class="hero-title">猫娘窝 · 遥控台</div>
        <div class="hero-sub">
          {{ device?.status === 'online' ? '点会话可同步 PC 历史；右侧「归档」藏线程' : '设备离线，请检查家中 Daemon' }}
        </div>
      </div>
    </div>

    <div v-if="device" class="device-info-card neko-card">
      <div class="info-row">
        <span class="info-label">状态</span>
        <span class="info-value">
          <span class="status-dot" :class="device.status"></span>
          {{ device.status === 'online' ? '在线' : '离线' }}
        </span>
      </div>
      <div class="info-row">
        <span class="info-label">Agent 数</span>
        <span class="info-value">{{ device.active_agents }} 个</span>
      </div>
    </div>

    <div class="sessions-section">
      <h2>Agent 会话</h2>
      <SessionThreadList :sessions="sessionStore.sessions" @open="goSession" />
    </div>

    <div class="actions-section">
      <n-button block @click="$router.push(`/device/${deviceId}/sessions`)">📋 全部会话</n-button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NButton } from 'naive-ui'
import { useDeviceStore } from '@/stores/device'
import { useSessionStore } from '@/stores/session'
import { useBindingStore } from '@/stores/binding'
import { apiFetch } from '@/api/http'
import { ensurePushSubscription } from '@/api/push'
import { nekoWS } from '@/api/websocket'
import SessionThreadList from '@/components/SessionThreadList.vue'

const route = useRoute()
const router = useRouter()
const deviceStore = useDeviceStore()
const sessionStore = useSessionStore()
const binding = useBindingStore()

const deviceId = computed(() => String(route.params.deviceId || ''))
const device = computed(() => deviceStore.devices.find(d => d.id === deviceId.value))
let fetchGen = 0
let fetchController: AbortController | null = null
let mounted = false

onMounted(() => {
  mounted = true
  deviceStore.initWebSocket()
  activateDevice(deviceId.value)
  void deviceStore.fetchDevices()
})

onUnmounted(() => {
  mounted = false
  fetchGen++
  fetchController?.abort()
  fetchController = null
})

watch(deviceId, (next, previous) => {
  if (mounted && next && next !== previous) {
    activateDevice(next)
  }
})

function activateDevice(want: string) {
  if (!want) return
  binding.setLastDevice(want)
  sessionStore.subscribeDevice(want)
  void fetchSessions(want)
  void ensurePushSubscription(want)
}

async function fetchSessions(want: string) {
  const gen = ++fetchGen
  fetchController?.abort()
  const controller = new AbortController()
  fetchController = controller
  try {
    const res = await apiFetch(
      `/api/devices/sessions?device_id=${encodeURIComponent(want)}`,
      { signal: controller.signal }
    )
    if (!res.ok) return
    if (!isCurrentRequest(want, gen, controller)) return
    const data = await res.json()
    if (!isCurrentRequest(want, gen, controller)) return
    if (data.sessions) {
      sessionStore.sessions = data.sessions
    }
  } catch (error) {
    if (!controller.signal.aborted) {
      console.warn('[device] session fetch failed:', error)
    }
  } finally {
    if (fetchController === controller) fetchController = null
  }
}

function isCurrentRequest(want: string, gen: number, controller: AbortController) {
  return (
    mounted &&
    !controller.signal.aborted &&
    gen === fetchGen &&
    deviceId.value === want &&
    nekoWS().getSubscribedDevice() === want
  )
}

function goSession(id: string) {
  router.push(`/device/${deviceId.value}/session/${encodeURIComponent(id)}`)
}
</script>

<style scoped>
.device-detail-page {
  padding: 20px;
  padding-bottom: 40px;
  min-height: 100vh;
  background:
    radial-gradient(ellipse 90% 40% at 50% -10%, rgba(255, 182, 193, 0.22), transparent 55%),
    radial-gradient(ellipse 60% 30% at 100% 40%, rgba(184, 169, 232, 0.15), transparent 50%),
    #FAF8F5;
}
.page-header {
  display: flex; align-items: center; gap: 12px; margin-bottom: 16px;
}
.page-header h1 {
  font-size: 20px; font-weight: 600; color: #4A4A4A; margin: 0; flex: 1;
}
.hero-banner {
  display: flex; align-items: center; gap: 14px; margin-bottom: 16px;
  background: linear-gradient(135deg, #F3EEFF 0%, #FFE8F0 100%);
  border: 1px solid rgba(184, 169, 232, 0.35);
}
.hero-cat img {
  display: block; width: 72px; height: 72px; border-radius: 50%; object-fit: cover;
  box-shadow: 0 4px 14px rgba(184, 169, 232, 0.45);
  border: 2px solid rgba(255,255,255,0.95);
}
.hero-title { font-size: 16px; font-weight: 700; color: #5A4A8A; }
.hero-sub { font-size: 13px; color: #7A6A9A; margin-top: 4px; }

.device-info-card { margin-bottom: 20px; }
.info-row {
  display: flex; justify-content: space-between; align-items: center;
  padding: 8px 0; border-bottom: 1px solid #F5F3F0;
}
.info-row:last-child { border-bottom: none; }
.info-label { font-size: 13px; color: #9E9E9E; }
.info-value { font-size: 14px; color: #4A4A4A; display: flex; align-items: center; gap: 6px; }

h2 { font-size: 16px; font-weight: 600; color: #4A4A4A; margin: 0 0 12px; }
.actions-section { margin-top: 20px; }
</style>
