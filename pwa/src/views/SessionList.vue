<template>
  <div class="session-list-page">
    <div class="page-header">
      <n-button text @click="$router.back()">← 返回</n-button>
      <h1>全部会话</h1>
      <n-button size="small" aria-label="刷新会话列表" @click="refresh()">🔄</n-button>
    </div>

    <SessionThreadList :sessions="sessionStore.sessions" @open="goSession" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NButton } from 'naive-ui'
import { useSessionStore } from '@/stores/session'
import { apiFetch } from '@/api/http'
import { nekoWS } from '@/api/websocket'
import SessionThreadList from '@/components/SessionThreadList.vue'

const route = useRoute()
const router = useRouter()
const sessionStore = useSessionStore()
const deviceId = computed(() => String(route.params.deviceId || ''))
let fetchGen = 0
let fetchController: AbortController | null = null
let mounted = false

onMounted(() => {
  mounted = true
  activateDevice(deviceId.value)
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
  sessionStore.subscribeDevice(want)
  void refresh(want)
}

async function refresh(want = deviceId.value) {
  if (!want) return
  sessionStore.subscribeDevice(want)
  const gen = ++fetchGen
  fetchController?.abort()
  const controller = new AbortController()
  fetchController = controller
  try {
    const res = await apiFetch(
      `/api/devices/sessions?device_id=${encodeURIComponent(want)}`,
      { signal: controller.signal }
    )
    if (!res.ok || !isCurrentRequest(want, gen, controller)) return
    const data = await res.json()
    if (!isCurrentRequest(want, gen, controller)) return
    if (data.sessions) {
      sessionStore.sessions = data.sessions
    }
  } catch (error) {
    if (!controller.signal.aborted) {
      console.warn('[sessions] fetch failed:', error)
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
.session-list-page {
  padding: 20px;
  padding-bottom: 40px;
  min-height: 100vh;
  background:
    radial-gradient(ellipse 80% 35% at 0% 0%, rgba(255, 182, 193, 0.18), transparent 50%),
    #FAF8F5;
}
.page-header {
  display: flex; align-items: center; gap: 12px; margin-bottom: 16px;
}
.page-header h1 { font-size: 20px; font-weight: 600; color: #4A4A4A; margin: 0; flex: 1; }
</style>
