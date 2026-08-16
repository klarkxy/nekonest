<template>
  <div class="device-detail-page">
    <header class="device-nav">
      <RouterLink class="back-link" :to="devicesLocation()" :aria-label="t('common.backNest')">
        <span aria-hidden="true">‹</span>
        {{ t('deviceDetail.backNest') }}
      </RouterLink>
      <div class="device-title">
        <h1>{{ device?.name || deviceId }}</h1>
      </div>
      <span
        class="device-status-mark"
        :class="device?.status === 'online' ? 'device-status-mark--online' : ''"
        :title="device?.status === 'online' ? t('common.online') : t('common.offline')"
      >
        <span class="status-dot" :class="device?.status || 'waiting'" aria-hidden="true"></span>
        <span class="sr-only">{{ device?.status === 'online' ? t('common.online') : t('common.offline') }}</span>
      </span>
    </header>

    <section
      v-if="showWelcome"
      class="welcome-scene"
      aria-labelledby="welcome-title"
    >
      <div class="scene-portrait">
        <span class="portrait-backdrop" aria-hidden="true"></span>
        <img
          src="/brand/nekonest-duo.webp"
          :alt="t('brand.duoAlt')"
          width="104"
          height="104"
        />
      </div>
      <div class="scene-dialogue">
        <p class="speaker">{{ t('deviceDetail.speaker') }}</p>
        <h2 id="welcome-title">{{ welcomeTitle }}</h2>
        <p>{{ welcomeBody }}</p>
        <span class="dialogue-tail" aria-hidden="true"></span>
      </div>
    </section>

    <dl
      v-if="showWelcome"
      class="device-stats"
      :aria-label="t('deviceDetail.statsAria')"
    >
      <div>
        <dt>{{ t('deviceDetail.statStatus') }}</dt>
        <dd>
          <span class="status-dot" :class="device?.status || 'waiting'" aria-hidden="true"></span>
          {{ device ? (device.status === 'online' ? t('common.online') : t('common.offline')) : t('common.loading') }}
        </dd>
      </div>
      <div>
        <dt>{{ t('deviceDetail.statThreads') }}</dt>
        <dd>{{ threadStatCount }}</dd>
      </div>
    </dl>

    <section class="sessions-section" aria-labelledby="sessions-title">
      <div class="section-heading">
        <div>
          <h2 id="sessions-title">{{ t('deviceDetail.sectionTitle') }}</h2>
        </div>
        <button
          v-if="showEnableNotifications"
          type="button"
          class="notify-enable"
          :disabled="pushBusy"
          @click="enableNotifications"
        >{{ t('deviceDetail.enableNotifications') }}</button>
        <p v-else-if="showNotificationsBlocked" class="notify-blocked">
          {{ t('deviceDetail.notificationsBlocked') }}
        </p>
      </div>

      <div v-if="authError" class="load-error" role="alert">
        <p>{{ t('deviceList.authBody') }}</p>
        <RouterLink class="retry-load" :to="setupLocation()">{{ t('deviceList.keySettings') }}</RouterLink>
      </div>
      <div v-else-if="loadError" class="load-error" role="alert">
        <p>{{ loadError }}</p>
        <button type="button" class="retry-load" :disabled="loadingSessions" @click="retryFetch">
          {{ loadingSessions ? t('common.retrying') : t('deviceDetail.retryOnce') }}
        </button>
      </div>

      <div
        v-if="loadingSessions && sessionStore.sessions.length === 0 && !loadError"
        class="load-pending"
        role="status"
      >
        <span class="sr-only">{{ t('deviceDetail.loadingThreads') }}</span>
        <div v-for="index in 3" :key="index" class="load-pending__row" aria-hidden="true">
          <span class="neko-skeleton-line load-pending__title"></span>
          <span class="neko-skeleton-line load-pending__meta"></span>
        </div>
      </div>

      <SessionThreadList
        v-if="!authError && (sessionStore.sessions.length > 0 || localThreadCount > 0 || (!loadingSessions && !loadError))"
        :sessions="sessionStore.sessions"
        :device-id="deviceId"
        :device-online="isOnline"
        :start-capabilities="sessionStore.startCapabilities"
      />
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, RouterLink } from 'vue-router'
import { routePageTitle, setDocumentTitle } from '@/router/title'
import { useDeviceStore } from '@/stores/device'
import { useSessionStore } from '@/stores/session'
import { useBindingStore } from '@/stores/binding'
import { useLocalThreadsStore } from '@/stores/localThreads'
import { apiFetch } from '@/api/http'
import { ensurePushSubscription, notificationPermission } from '@/api/push'
import { nekoWS } from '@/api/websocket'
import { devicesLocation, setupLocation } from '@/router/navigation'
import SessionThreadList from '@/components/SessionThreadList.vue'
import { deviceThreadStatCount } from '@/utils/threadCount'

const { t } = useI18n()
const route = useRoute()
const deviceStore = useDeviceStore()
const sessionStore = useSessionStore()
const binding = useBindingStore()
const localThreads = useLocalThreadsStore()

const deviceId = computed(() => String(route.params.deviceId || ''))
const device = computed(() => deviceStore.devices.find(d => d.id === deviceId.value))
const loadError = ref('')
const authError = ref(false)
const loadingSessions = ref(false)
const localThreadCount = computed(() => localThreads.listForDevice(deviceId.value).length)

const isOnline = computed(() => device.value?.status === 'online')
const hasThreads = computed(
  () => sessionStore.sessions.length > 0 || localThreadCount.value > 0
)
/** Offline / empty only — avoid a filler "welcome back" strip when the tree is already primary. */
const showWelcome = computed(() => !isOnline.value || !hasThreads.value)

const welcomeTitle = computed(() => {
  if (!isOnline.value) return t('deviceDetail.welcomeOfflineTitle')
  return t('deviceDetail.welcomeEmptyTitle')
})

const welcomeBody = computed(() => {
  if (!isOnline.value) {
    return hasThreads.value
      ? t('deviceDetail.welcomeOfflineBody')
      : t('deviceDetail.welcomeOfflineEmptyBody')
  }
  return t('deviceDetail.welcomeEmptyBody')
})

const threadStatCount = computed(() =>
  deviceThreadStatCount(sessionStore.sessions.length, localThreadCount.value)
)

watch(
  () => device.value?.name,
  (name) => {
    setDocumentTitle(routePageTitle('device-detail'), name || undefined)
  },
  { immediate: true }
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

const pushCapable =
  typeof window !== 'undefined' &&
  'serviceWorker' in navigator &&
  'PushManager' in window &&
  typeof Notification !== 'undefined'
const notificationPerm = ref(notificationPermission())
const pushBusy = ref(false)
const showEnableNotifications = computed(
  () => pushCapable && notificationPerm.value === 'default'
)
const showNotificationsBlocked = computed(
  () => pushCapable && notificationPerm.value === 'denied'
)

function activateDevice(want: string) {
  if (!want) return
  binding.setLastDevice(want)
  sessionStore.subscribeDevice(want)
  void fetchSessions(want)
  // Already-granted browsers can resubscribe; do not prompt on mount (iOS).
  void ensurePushSubscription(want).finally(() => {
    notificationPerm.value = notificationPermission()
  })
}

async function enableNotifications() {
  if (!deviceId.value || pushBusy.value) return
  pushBusy.value = true
  try {
    await ensurePushSubscription(deviceId.value, { requestPermission: true })
  } finally {
    notificationPerm.value = notificationPermission()
    pushBusy.value = false
  }
}

function retryFetch() {
  void fetchSessions(deviceId.value)
}

async function fetchSessions(want: string) {
  const gen = ++fetchGen
  fetchController?.abort()
  const controller = new AbortController()
  fetchController = controller
  loadError.value = ''
  authError.value = false
  loadingSessions.value = true
  try {
    const res = await apiFetch(
      `/api/devices/sessions?device_id=${encodeURIComponent(want)}`,
      { signal: controller.signal }
    )
    if (!isCurrentRequest(want, gen, controller)) return
    if (res.status === 401) {
      authError.value = true
      return
    }
    if (!res.ok) {
      loadError.value = t('deviceDetail.loadError')
      return
    }
    const data = await res.json()
    if (!isCurrentRequest(want, gen, controller)) return
    sessionStore.applySessionList(data, want, undefined, 'rest')
    loadError.value = ''
  } catch (error) {
    if (!controller.signal.aborted && isCurrentRequest(want, gen, controller)) {
      loadError.value = t('deviceDetail.loadNetwork')
      console.warn('[device] session fetch failed:', error)
    }
  } finally {
    if (fetchController === controller) fetchController = null
    if (gen === fetchGen) loadingSessions.value = false
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
  margin-bottom: 16px;
}

.back-link {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  min-height: 44px;
  padding: 0 4px;
  color: var(--neko-primary-deep);
  font-size: 14px;
  font-weight: 620;
  text-decoration: none;
}

.back-link span {
  margin-right: 4px;
  font-family: serif;
  font-size: 21px;
  line-height: 0;
  transform: translateY(-1px);
}

.device-title {
  min-width: 0;
}

.speaker {
  margin: 0;
  color: var(--neko-primary-deep);
  font-size: 12px;
  font-weight: 760;
  letter-spacing: 0.04em;
  line-height: 1.4;
}

.device-title h1 {
  overflow: hidden;
  margin: 0;
  color: var(--neko-ink);
  font-family: var(--neko-display);
  font-size: 18px;
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
  background: var(--neko-neutral-soft);
}

.device-status-mark--online {
  background: var(--neko-success-soft);
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
  background: var(--neko-primary-soft);
  opacity: 0.45;
}

.scene-portrait img {
  display: block;
  width: 104px;
  height: 104px;
  border: 3px solid var(--neko-panel-border);
  border-radius: 30px 30px 37px 19px;
  box-shadow: var(--neko-shadow-soft);
  object-fit: cover;
}

.scene-dialogue {
  position: relative;
  min-height: 119px;
  padding: 16px 15px 15px 25px;
  border: 1px solid var(--neko-line);
  border-radius: 18px 18px 25px 12px;
  color: var(--neko-ink);
  background: var(--neko-surface-solid);
  box-shadow: var(--neko-shadow-soft);
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
  border-bottom: 1px solid var(--neko-line);
  background: var(--neko-surface-solid);
  clip-path: polygon(100% 0, 100% 100%, 0 100%);
  transform: rotate(45deg);
}

.device-stats {
  display: grid;
  grid-template-columns: 1fr 1fr;
  margin: 0 0 22px;
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
  font-size: 11px;
  letter-spacing: 0.04em;
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
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 12px;
  padding-inline: 2px;
}

.section-heading h2 {
  margin: 0;
  color: var(--neko-ink);
  font-family: var(--neko-display);
  font-size: 19px;
  font-weight: 720;
  letter-spacing: -0.035em;
}

.load-error,
.load-pending {
  padding: 18px 16px;
  border-radius: 16px;
  text-align: center;
  font-size: 13px;
  line-height: 1.55;
  margin-bottom: 12px;
}

.load-error {
  border: 1px solid var(--neko-danger-line);
  color: var(--neko-danger-ink);
  background: var(--neko-danger-soft);
}

.load-error p {
  margin: 0 0 12px;
}

.retry-load {
  display: inline-flex;
  min-height: 44px;
  align-items: center;
  padding: 0 16px;
  border: 0;
  border-radius: 12px;
  color: #fff;
  background: var(--neko-primary);
  font-weight: 650;
  cursor: pointer;
  text-decoration: none;
}

html[data-theme='dark'] .retry-load {
  color: #1a1422;
}

.retry-load:disabled {
  opacity: 0.65;
  cursor: wait;
}

.notify-enable {
  flex: 0 0 auto;
  display: inline-flex;
  min-height: 44px;
  align-items: center;
  padding: 0 12px;
  border: 1px solid var(--neko-line);
  border-radius: 12px;
  color: var(--neko-primary-deep);
  background: var(--neko-surface-solid);
  font-size: 13px;
  font-weight: 650;
  cursor: pointer;
}

.notify-enable:disabled {
  opacity: 0.65;
  cursor: wait;
}

.notify-blocked {
  flex: 0 1 auto;
  margin: 0;
  color: var(--neko-ink-faint);
  font-size: 12px;
  line-height: 1.4;
  text-align: right;
}

.load-pending {
  display: grid;
  gap: 14px;
  padding: 18px 16px;
  background: var(--neko-panel);
}

.load-pending__row {
  display: grid;
  gap: 9px;
}

.load-pending__title {
  width: 52%;
}

.load-pending__meta {
  width: 74%;
  height: 8px;
}

@media (max-width: 370px) {
  .device-detail-page {
    padding-inline: 16px;
  }

  .welcome-scene {
    grid-template-columns: 78px minmax(0, 1fr);
  }

  .welcome-scene .scene-portrait,
  .welcome-scene .scene-portrait img {
    width: 90px;
  }

  .welcome-scene .scene-portrait img {
    height: 90px;
  }

  .scene-dialogue {
    padding-left: 20px;
  }

  .section-heading {
    align-items: flex-start;
  }

  .session-overview {
    max-width: 160px;
  }
}
</style>
