<template>
  <div class="device-detail-page">
    <header class="device-nav">
      <n-button text class="back-button" aria-label="返回设备列表" @click="goBack">
        <span aria-hidden="true">‹</span>
        设备列表
      </n-button>
      <div class="device-title">
        <p>Current nest</p>
        <h1>{{ device?.name || deviceId }}</h1>
      </div>
      <span
        class="device-status-mark"
        :class="device?.status === 'online' ? 'device-status-mark--online' : ''"
        :title="device?.status === 'online' ? '在线' : '离线'"
      >
        <span class="status-dot" :class="device?.status || 'waiting'" aria-hidden="true"></span>
        <span class="sr-only">{{ device?.status === 'online' ? '在线' : '离线' }}</span>
      </span>
    </header>

    <section class="welcome-scene" aria-labelledby="welcome-title">
      <div class="scene-portrait">
        <span class="portrait-backdrop" aria-hidden="true"></span>
        <img
          src="/brand/nekonest-duo.webp"
          alt="NekoNest 的两位原创猫娘看板娘"
          width="104"
          height="104"
        />
      </div>
      <div class="scene-dialogue">
        <p class="speaker">NekoNest guide</p>
        <h2 id="welcome-title">
          {{ device?.status === 'online' ? '欢迎回来，目录已经整理好了。' : '这台电脑现在没有回应。' }}
        </h2>
        <p>
          {{
            device?.status === 'online'
              ? '从工作目录进入对应智能体，继续本机已有线程。'
              : '请检查家中 Daemon；恢复在线后，线程会在这里自动出现。'
          }}
        </p>
        <span class="dialogue-tail" aria-hidden="true"></span>
      </div>
    </section>

    <dl class="device-stats" aria-label="设备概况">
      <div>
        <dt>状态</dt>
        <dd>
          <span class="status-dot" :class="device?.status || 'waiting'" aria-hidden="true"></span>
          {{ device ? (device.status === 'online' ? '在线' : '离线') : '读取中' }}
        </dd>
      </div>
      <div>
        <dt>智能体</dt>
        <dd>{{ device?.active_agents ?? 0 }}</dd>
      </div>
      <div>
        <dt>线程</dt>
        <dd>{{ sessionStore.sessions.length }}</dd>
      </div>
    </dl>

    <section class="sessions-section" aria-labelledby="sessions-title">
      <div class="section-heading">
        <div>
          <p class="section-kicker">Directory · Agent · Thread</p>
          <h2 id="sessions-title">工作目录</h2>
        </div>
        <div class="session-overview" role="status" aria-live="polite">
          <span v-if="runningSessionCount" class="session-count-badge session-count-badge--active">
            <span aria-hidden="true">🐾</span>
            {{ runningSessionCount }} 条线程还在跑
          </span>
          <span v-if="waitingApprovalCount" class="session-count-badge session-count-badge--waiting">
            <span aria-hidden="true">🔔</span>
            {{ waitingApprovalCount }} 条等你点头
          </span>
          <span class="local-only">本机线程</span>
        </div>
      </div>
      <SessionThreadList :sessions="sessionStore.sessions" @open="goSession" />
    </section>
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
import {
  devicesLocation,
  sessionDetailLocation
} from '@/router/navigation'
import SessionThreadList from '@/components/SessionThreadList.vue'

const route = useRoute()
const router = useRouter()
const deviceStore = useDeviceStore()
const sessionStore = useSessionStore()
const binding = useBindingStore()

const deviceId = computed(() => String(route.params.deviceId || ''))
const device = computed(() => deviceStore.devices.find(d => d.id === deviceId.value))
const runningSessionCount = computed(
  () => sessionStore.sessions.filter(session => session.status === 'running').length
)
const waitingApprovalCount = computed(
  () => sessionStore.sessions.filter(session => session.status === 'waiting_approval').length
)
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
  void router.push(sessionDetailLocation(deviceId.value, id))
}

function goBack() {
  void router.push(devicesLocation())
}
</script>

<style scoped>
.device-detail-page {
  min-height: var(--neko-content-block-size, 100dvh);
  padding: 18px 20px 42px;
}

.device-nav {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  margin-bottom: 22px;
}

.back-button span {
  margin-right: 4px;
  font-family: serif;
  font-size: 21px;
  line-height: 0;
  transform: translateY(-1px);
}

.device-title {
  min-width: 0;
}

.device-title p,
.section-kicker,
.speaker {
  margin: 0;
  color: var(--neko-rose);
  font-size: 9px;
  font-weight: 760;
  letter-spacing: 0.11em;
  line-height: 1.4;
  text-transform: uppercase;
}

.device-title h1 {
  overflow: hidden;
  margin: 1px 0 0;
  color: var(--neko-ink);
  font-family: var(--neko-display);
  font-size: 17px;
  font-weight: 720;
  letter-spacing: -0.035em;
  line-height: 1.25;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.device-status-mark {
  display: grid;
  width: 30px;
  height: 30px;
  place-items: center;
  border-radius: 10px;
  background: rgba(235, 229, 232, 0.8);
}

.device-status-mark--online {
  background: rgba(226, 241, 233, 0.86);
}

.welcome-scene {
  position: relative;
  display: grid;
  grid-template-columns: 92px minmax(0, 1fr);
  align-items: end;
  gap: 0;
  min-height: 142px;
  margin: 0 -4px 15px;
}

.scene-portrait {
  position: relative;
  z-index: 2;
  align-self: center;
  width: 104px;
  transform: translate(3px, 4px);
}

.portrait-backdrop {
  position: absolute;
  inset: -13px;
  z-index: -1;
  border-radius: 50%;
  background: radial-gradient(circle, rgba(255, 255, 255, 0.92), transparent 67%);
}

.scene-portrait img {
  display: block;
  width: 104px;
  height: 104px;
  border: 3px solid rgba(255, 253, 251, 0.92);
  border-radius: 30px 30px 37px 19px;
  box-shadow: 0 13px 28px rgba(92, 67, 92, 0.18);
  object-fit: cover;
}

.scene-dialogue {
  position: relative;
  min-height: 119px;
  padding: 16px 15px 15px 25px;
  border: 1px solid rgba(255, 255, 255, 0.82);
  border-radius: 18px 18px 25px 12px;
  background:
    radial-gradient(circle at 96% 4%, rgba(225, 207, 237, 0.42), transparent 8rem),
    rgba(255, 252, 250, 0.9);
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.82),
    var(--neko-shadow-soft);
}

.scene-dialogue h2 {
  margin: 5px 0 0;
  color: var(--neko-ink);
  font-size: 14px;
  font-weight: 700;
  line-height: 1.42;
  text-wrap: balance;
}

.scene-dialogue > p:last-of-type {
  margin: 7px 0 0;
  color: var(--neko-ink-soft);
  font-size: 11px;
  line-height: 1.58;
  text-wrap: pretty;
}

.dialogue-tail {
  position: absolute;
  bottom: 13px;
  left: -8px;
  width: 16px;
  height: 16px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.82);
  background: #fffaf8;
  clip-path: polygon(100% 0, 100% 100%, 0 100%);
  transform: rotate(45deg);
}

.device-stats {
  display: grid;
  grid-template-columns: 1.25fr 0.8fr 0.8fr;
  margin: 0 0 26px;
  padding: 10px 4px;
  border-block: 1px solid var(--neko-line);
}

.device-stats > div {
  min-width: 0;
  padding: 2px 12px;
  border-left: 1px solid var(--neko-line);
}

.device-stats > div:first-child {
  border-left: 0;
}

.device-stats dt {
  color: var(--neko-ink-faint);
  font-size: 9px;
  letter-spacing: 0.06em;
}

.device-stats dd {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 5px 0 0;
  color: var(--neko-ink);
  font-size: 13px;
  font-weight: 680;
  font-variant-numeric: tabular-nums;
}

.sessions-section {
  position: relative;
}

.section-heading {
  display: flex;
  align-items: end;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 14px;
  padding-inline: 2px;
}

.section-heading h2 {
  margin: 3px 0 0;
  color: var(--neko-ink);
  font-family: var(--neko-display);
  font-size: 19px;
  font-weight: 720;
  letter-spacing: -0.035em;
}

.session-overview {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 5px;
}

.session-count-badge,
.local-only {
  padding: 4px 7px;
  border-radius: 7px;
  color: var(--neko-primary-deep);
  background: rgba(236, 229, 245, 0.86);
  font-size: 9px;
  font-weight: 680;
  font-variant-numeric: tabular-nums;
}

.session-count-badge {
  border: 1px solid transparent;
}

.session-count-badge--active {
  border-color: #bfdfca;
  background: #e9f5ed;
  color: #3e7654;
}

.session-count-badge--waiting {
  border-color: #ead09f;
  background: #fff4df;
  color: #8a642f;
}

@media (max-width: 370px) {
  .device-detail-page {
    padding-inline: 16px;
  }

  .welcome-scene {
    grid-template-columns: 78px minmax(0, 1fr);
  }

  .scene-portrait,
  .scene-portrait img {
    width: 90px;
  }

  .scene-portrait img {
    height: 90px;
  }

  .scene-dialogue {
    padding-left: 20px;
  }

  .scene-dialogue h2 {
    font-size: 13px;
  }

  .section-heading {
    align-items: flex-start;
  }

  .session-overview {
    max-width: 160px;
  }
}
</style>
