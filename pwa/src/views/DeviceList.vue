<template>
  <div class="device-list-page">
    <header class="brand-hero">
      <div class="brand-copy">
        <p class="eyebrow">{{ t('deviceList.eyebrow') }}</p>
        <h1>
          {{ t('brand.name') }}
          <span>{{ t('brand.nameEn') }}</span>
        </h1>
        <p class="brand-intro">{{ t('deviceList.intro') }}</p>
      </div>

      <figure class="mascot-stage">
        <span class="mascot-glow" aria-hidden="true"></span>
        <img
          src="/brand/nekonest-duo.webp"
          :alt="t('brand.duoAlt')"
          width="132"
          height="132"
        />
        <figcaption>{{ t('brand.duoCaption') }}</figcaption>
      </figure>
    </header>

    <section
      class="connection-panel"
      :class="{
        'connection-panel--connected': connection.tone === 'connected',
        'connection-panel--error': connection.tone === 'error'
      }"
      :aria-label="t('deviceList.connectionAria')"
      aria-live="polite"
    >
      <div class="connection-copy">
        <span
          class="status-dot"
          :class="connection.dot"
          aria-hidden="true"
        ></span>
        <span class="connection-label">{{ connection.label }}</span>
      </div>
      <button
        v-if="deviceStore.loadError"
        type="button"
        class="connection-action"
        :disabled="deviceStore.loading"
        @click="retryDevices"
      >
        {{ deviceStore.loading ? t('common.retrying') : t('common.retry') }}
      </button>
      <RouterLink v-else class="connection-action" :to="setupLocation()">{{ t('deviceList.keySettings') }}</RouterLink>
    </section>

    <div v-if="deviceStore.authError" class="auth-banner" role="alert">
      <strong>{{ t('deviceList.authTitle') }}</strong>
      <span>{{ t('deviceList.authBody') }}</span>
    </div>

    <section class="device-section" aria-labelledby="device-section-title">
      <div class="section-heading">
        <div>
          <p class="section-kicker">{{ t('deviceList.sectionKicker') }}</p>
          <h2 id="device-section-title">{{ t('deviceList.sectionTitle') }}</h2>
        </div>
        <span class="device-total">{{ t('deviceList.deviceCount', { n: visibleDevices.length }) }}</span>
      </div>

      <div v-if="deviceStore.loading && visibleDevices.length === 0" class="device-skeletons" role="status">
        <span class="sr-only">{{ t('deviceList.loadingList') }}</span>
        <div v-for="index in 2" :key="index" class="device-skeleton" aria-hidden="true">
          <span class="skeleton-line skeleton-line--title"></span>
          <span class="skeleton-line skeleton-line--meta"></span>
        </div>
      </div>

      <div v-else-if="deviceStore.loadError" class="load-failure" role="alert">
        <span class="load-failure__mark" aria-hidden="true">↻</span>
        <h3>{{ t('deviceList.loadFailureTitle') }}</h3>
        <p>{{ deviceStore.loadError }}</p>
        <button type="button" :disabled="deviceStore.loading" @click="retryDevices">
          {{ deviceStore.loading ? t('common.reconnecting') : t('common.reconnect') }}
        </button>
      </div>

      <div v-else class="device-cards">
        <article
          v-for="device in visibleDevices"
          :key="device.id"
          class="device-card"
        >
          <RouterLink
            class="device-entry"
            :class="{ 'device-entry--offline': device.status !== 'online' }"
            :to="deviceDetailLocation(device.id)"
            :aria-label="t('deviceList.openDevice', {
              name: device.name,
              status: device.status === 'online' ? t('common.online') : t('common.offline')
            })"
            @click="onOpenDevice(device)"
          >
            <span class="device-entry__accent" aria-hidden="true"></span>
            <span class="device-entry__body">
              <span class="device-entry__topline">
                <span class="device-name">{{ device.name }}</span>
                <span class="device-os">{{ osLabel(device.os) }}</span>
              </span>
              <span class="device-entry__meta">
                <span class="status-dot" :class="device.status" aria-hidden="true"></span>
                <span>{{ device.status === 'online' ? t('common.online') : t('common.offline') }}</span>
                <span class="meta-divider" aria-hidden="true"></span>
                <span class="agent-count">{{ t('deviceList.threadCount', { n: device.active_agents }) }}</span>
              </span>
            </span>
            <span class="device-arrow" aria-hidden="true">›</span>
          </RouterLink>
          <button
            v-if="binding.boundIds.has(device.id)"
            type="button"
            class="unbind-btn"
            :aria-label="t('deviceList.unbindAria', { name: device.name })"
            @click="confirmUnbind(device)"
          >
            {{ t('deviceList.unbind') }}
          </button>
        </article>

        <div v-if="deviceStore.loaded && visibleDevices.length === 0" class="empty-state">
          <img
            src="/brand/nekonest-duo.webp"
            alt=""
            width="88"
            height="88"
            aria-hidden="true"
          />
          <h3>{{ t('deviceList.emptyTitle') }}</h3>
          <p>{{ t('deviceList.emptyBody') }}</p>
          <RouterLink class="empty-pair-link" :to="pairLocation()">{{ t('deviceList.emptyPair') }}</RouterLink>
        </div>
      </div>
    </section>

    <LocaleThemeBar class="list-prefs" />

    <nav class="bottom-dock" :aria-label="t('deviceList.dockAria')">
      <RouterLink class="dock-pair" :to="pairLocation()">
        <span class="add-mark" aria-hidden="true">＋</span>
        {{ t('deviceList.pairNew') }}
      </RouterLink>
    </nav>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import { useMessage } from 'naive-ui'
import LocaleThemeBar from '@/components/LocaleThemeBar.vue'
import { useDeviceStore } from '@/stores/device'
import { useBindingStore } from '@/stores/binding'
import {
  deviceDetailLocation,
  pairLocation,
  setupLocation
} from '@/router/navigation'
import type { Device } from '@/types/protocol'
import { selectVisibleDevices } from '@/utils/deviceVisibility'

const { t } = useI18n()
const message = useMessage()
const deviceStore = useDeviceStore()
const binding = useBindingStore()

const visibleDevices = computed(() =>
  selectVisibleDevices(
    deviceStore.devices,
    binding.bound,
    binding.bindingConfigured
  )
)

const connection = computed(() => {
  if (deviceStore.authError) {
    return { tone: 'error', dot: 'offline', label: t('deviceList.connAuth') } as const
  }
  if (deviceStore.loadError) {
    return { tone: 'error', dot: 'offline', label: t('deviceList.connServerFail') } as const
  }
  if (!deviceStore.loaded || deviceStore.loading) {
    return { tone: 'waiting', dot: 'waiting', label: t('deviceList.connChecking') } as const
  }
  if (deviceStore.connected) {
    return { tone: 'connected', dot: 'online', label: t('deviceList.connLive') } as const
  }
  if (visibleDevices.value.length === 0) {
    return { tone: 'connected', dot: 'online', label: t('deviceList.connServerOk') } as const
  }
  return { tone: 'waiting', dot: 'waiting', label: t('deviceList.connWsReconnect') } as const
})

onMounted(() => {
  deviceStore.initWebSocket()
  void deviceStore.fetchDevices()
})

function retryDevices() {
  void deviceStore.fetchDevices()
}

function onOpenDevice(device: Device) {
  binding.setLastDevice(device.id)
}

function confirmUnbind(device: Device) {
  const ok = window.confirm(t('deviceList.unbindConfirm', { name: device.name }))
  if (!ok) return
  binding.removeBinding(device.id)
  message.success(t('deviceList.unbindSuccess'))
}

function osLabel(os: string): string {
  const normalized = os.trim().toLowerCase()
  if (normalized === 'windows') return 'Windows'
  if (normalized === 'darwin' || normalized === 'macos') return 'macOS'
  if (normalized === 'linux') return 'Linux'
  return os || t('common.computer')
}
</script>

<style scoped>
.device-list-page {
  min-height: var(--neko-content-block-size, 100dvh);
  padding: 26px 20px calc(104px + var(--neko-safe-bottom, 0px));
}

.brand-hero {
  position: relative;
  display: grid;
  grid-template-columns: minmax(0, 1fr) 132px;
  align-items: center;
  min-height: 172px;
  margin-bottom: 12px;
}

.brand-copy {
  position: relative;
  z-index: 1;
  padding: 10px 0 18px;
}

.eyebrow,
.section-kicker {
  margin: 0;
  color: var(--neko-primary-deep);
  font-size: 12px;
  font-weight: 750;
  letter-spacing: 0.04em;
  line-height: 1.4;
}

.list-prefs {
  margin-bottom: 12px;
}

.brand-copy h1 {
  margin: 5px 0 0;
  color: var(--neko-ink);
  font-family: var(--neko-display);
  font-size: clamp(2.1rem, 10vw, 3rem);
  font-weight: 760;
  letter-spacing: -0.075em;
  line-height: 0.96;
}

.brand-copy h1 span {
  display: block;
  margin-top: 9px;
  color: var(--neko-primary);
  font-size: 0.35em;
  font-weight: 720;
  letter-spacing: 0.08em;
  line-height: 1;
}

.brand-intro {
  max-width: 19rem;
  margin: 15px 0 0;
  color: var(--neko-ink-soft);
  font-size: 12px;
  line-height: 1.65;
  text-wrap: pretty;
}

.mascot-stage {
  position: relative;
  align-self: start;
  width: 132px;
  margin: 7px -7px 0 0;
}

.mascot-stage::before {
  position: absolute;
  right: 8px;
  bottom: 8px;
  z-index: -1;
  width: 104px;
  height: 38px;
  border-radius: 50%;
  background: rgba(91, 69, 99, 0.15);
  filter: blur(13px);
  content: "";
  transform: rotate(-5deg);
}

.mascot-glow {
  position: absolute;
  inset: -14px;
  z-index: -1;
  border-radius: 50%;
  background: radial-gradient(circle, rgba(255, 255, 255, 0.92), transparent 68%);
}

.mascot-stage img {
  display: block;
  width: 132px;
  height: 132px;
  border: 4px solid rgba(255, 253, 251, 0.88);
  border-radius: 31px 31px 40px 22px;
  box-shadow: 0 16px 34px rgba(104, 74, 105, 0.2);
  object-fit: cover;
  transform: rotate(2deg);
}

.mascot-stage figcaption {
  position: absolute;
  right: -1px;
  bottom: -7px;
  padding: 5px 9px;
  border: 1px solid rgba(255, 255, 255, 0.9);
  border-radius: 8px 8px 11px 5px;
  color: var(--neko-primary-deep);
  background: rgba(247, 239, 251, 0.94);
  box-shadow: 0 5px 12px rgba(91, 67, 90, 0.1);
  font-size: 11px;
  font-weight: 700;
}

.connection-panel {
  display: flex;
  min-height: 44px;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 12px;
  padding: 8px 9px 8px 13px;
  border: 1px solid rgba(110, 89, 119, 0.12);
  border-radius: 13px;
  background: rgba(247, 242, 241, 0.78);
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.72);
}

.connection-panel--connected {
  background: rgba(232, 244, 237, 0.77);
}

.connection-panel--error {
  border-color: rgba(191, 104, 116, 0.22);
  background: rgba(249, 232, 233, 0.74);
}

.connection-copy {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 10px;
}

.connection-label {
  overflow: hidden;
  color: var(--neko-ink-soft);
  font-size: 12px;
  font-weight: 620;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.connection-action {
  display: inline-flex;
  flex: 0 0 auto;
  min-height: 44px;
  align-items: center;
  padding: 6px 10px;
  border: 0;
  border-radius: 8px;
  color: var(--neko-primary-deep);
  background: transparent;
  font-size: 12px;
  font-weight: 650;
  cursor: pointer;
  text-decoration: none;
}

.connection-action:hover {
  background: rgba(114, 91, 157, 0.1);
}

.connection-action:disabled {
  cursor: wait;
  opacity: 0.65;
}

.auth-banner {
  display: grid;
  gap: 4px;
  margin: 0 0 16px;
  padding: 13px 14px;
  border-left: 3px solid var(--neko-danger);
  border-radius: 8px 14px 14px 8px;
  color: #784951;
  background: rgba(249, 231, 233, 0.9);
  font-size: 12px;
  line-height: 1.55;
}

.auth-banner strong {
  font-size: 13px;
}

.device-section {
  margin-top: 24px;
}

.section-heading {
  display: flex;
  align-items: end;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
  padding-inline: 2px;
}

.section-heading h2 {
  margin: 3px 0 0;
  color: var(--neko-ink);
  font-family: var(--neko-display);
  font-size: 19px;
  font-weight: 710;
  letter-spacing: -0.035em;
}

.device-total {
  color: var(--neko-ink-faint);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}

.device-cards,
.device-skeletons {
  display: grid;
  gap: 10px;
}

.load-failure {
  display: grid;
  justify-items: center;
  padding: 30px 24px 33px;
  border: 1px solid rgba(191, 104, 116, 0.2);
  border-radius: 21px 21px 27px 14px;
  color: #784951;
  background: rgba(249, 231, 233, 0.58);
  text-align: center;
}

.load-failure__mark {
  display: grid;
  width: 52px;
  height: 52px;
  place-items: center;
  border-radius: 17px;
  color: var(--neko-danger);
  background: rgba(255, 255, 255, 0.62);
  font-size: 27px;
  line-height: 1;
}

.load-failure h3 {
  margin: 14px 0 0;
  color: var(--neko-ink);
  font-size: 16px;
}

.load-failure p {
  max-width: 18rem;
  margin: 7px 0 17px;
  font-size: 12px;
  line-height: 1.6;
  text-wrap: pretty;
}

.load-failure button {
  min-height: 44px;
  padding: 0 18px;
  border: 0;
  border-radius: 13px;
  color: #fff;
  background: var(--neko-danger);
  font-size: 14px;
  font-weight: 650;
  cursor: pointer;
  box-shadow: 0 8px 20px rgba(191, 104, 116, 0.22);
}

.load-failure button:disabled {
  cursor: wait;
  opacity: 0.65;
}

.device-card {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: stretch;
  gap: 8px;
}

.device-entry {
  position: relative;
  display: grid;
  grid-template-columns: 4px minmax(0, 1fr) auto;
  width: 100%;
  min-height: 82px;
  align-items: center;
  gap: 13px;
  padding: 14px 14px 14px 0;
  overflow: hidden;
  border: 1px solid rgba(255, 255, 255, 0.78);
  border-radius: 17px 17px 22px 12px;
  color: inherit;
  background:
    radial-gradient(circle at 90% -30%, rgba(224, 207, 236, 0.34), transparent 9rem),
    rgba(255, 252, 250, 0.89);
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.8),
    var(--neko-shadow-soft);
  cursor: pointer;
  text-align: left;
  text-decoration: none;
  transition: transform 190ms ease, box-shadow 190ms ease, background-color 190ms ease;
}

.device-entry__accent {
  align-self: stretch;
  border-radius: 0 8px 8px 0;
  background: linear-gradient(180deg, var(--neko-rose), var(--neko-primary));
}

.device-entry--offline {
  background: rgba(249, 246, 245, 0.83);
  box-shadow: 0 6px 18px rgba(92, 67, 92, 0.06);
}

.device-entry--offline .device-entry__accent {
  background: #bcb2b8;
}

.device-entry__body {
  display: grid;
  min-width: 0;
  gap: 9px;
}

.device-entry__topline,
.device-entry__meta {
  display: flex;
  min-width: 0;
  align-items: center;
}

.device-entry__topline {
  gap: 10px;
}

.device-name {
  overflow: hidden;
  flex: 1;
  color: var(--neko-ink);
  font-size: 15px;
  font-weight: 680;
  line-height: 1.2;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.device-os {
  flex: 0 0 auto;
  padding: 3px 6px;
  border-radius: 6px;
  color: var(--neko-primary-deep);
  background: var(--neko-primary-soft);
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.035em;
}

.device-entry__meta {
  gap: 8px;
  color: var(--neko-ink-soft);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}

.meta-divider {
  width: 1px;
  height: 11px;
  background: var(--neko-line);
}

.agent-count {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.device-arrow {
  color: var(--neko-primary);
  font-family: serif;
  font-size: 25px;
  line-height: 1;
  transform: translateY(-1px);
}

.device-entry:active {
  transform: scale(0.985);
}

.unbind-btn {
  align-self: center;
  min-width: 52px;
  min-height: 44px;
  padding: 0 10px;
  border: 1px solid rgba(191, 104, 116, 0.28);
  border-radius: 12px;
  color: var(--neko-danger);
  background: rgba(249, 231, 233, 0.72);
  font-size: 12px;
  font-weight: 650;
  cursor: pointer;
}

.unbind-btn:hover {
  background: rgba(249, 231, 233, 0.95);
}

.device-skeleton {
  display: grid;
  gap: 11px;
  min-height: 82px;
  padding: 19px 22px;
  border-radius: 17px 17px 22px 12px;
  background: rgba(255, 252, 250, 0.7);
}

.skeleton-line {
  display: block;
  height: 11px;
  border-radius: 5px;
  background:
    linear-gradient(
      90deg,
      rgba(221, 211, 217, 0.52) 20%,
      rgba(245, 238, 241, 0.8) 45%,
      rgba(221, 211, 217, 0.52) 70%
    );
  background-size: 220% 100%;
  animation: skeleton-sheen 1.5s ease-in-out infinite;
}

.skeleton-line--title {
  width: 48%;
}

.skeleton-line--meta {
  width: 68%;
  height: 8px;
}

@keyframes skeleton-sheen {
  from {
    background-position: 100% 0;
  }
  to {
    background-position: -100% 0;
  }
}

.empty-state {
  display: grid;
  justify-items: center;
  padding: 30px 24px 33px;
  border: 1px dashed rgba(126, 102, 140, 0.26);
  border-radius: 21px 21px 27px 14px;
  background: rgba(255, 251, 249, 0.62);
  text-align: center;
}

.empty-state img {
  width: 88px;
  height: 88px;
  border-radius: 25px;
  box-shadow: 0 10px 25px rgba(91, 67, 90, 0.14);
  object-fit: cover;
}

.empty-state h3 {
  margin: 16px 0 0;
  font-size: 16px;
}

.empty-state p {
  max-width: 17rem;
  margin: 7px 0 17px;
  color: var(--neko-ink-soft);
  font-size: 12px;
  line-height: 1.6;
}

.empty-pair-link,
.dock-pair {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-height: 44px;
  padding: 0 18px;
  border-radius: 14px;
  color: #fff;
  background: var(--neko-primary);
  font-size: 15px;
  font-weight: 650;
  text-decoration: none;
  box-shadow: 0 8px 20px rgba(114, 91, 157, 0.28);
}

.bottom-dock {
  position: fixed;
  right: 0;
  bottom: 0;
  left: 0;
  z-index: 5;
  width: min(100%, 540px);
  margin: 0 auto;
  padding: 12px 20px calc(12px + var(--neko-safe-bottom, 0px));
  border-top: 1px solid rgba(110, 89, 119, 0.1);
  background: rgba(250, 244, 242, 0.88);
  box-shadow: 0 -12px 32px rgba(87, 64, 89, 0.07);
  backdrop-filter: blur(18px);
  -webkit-backdrop-filter: blur(18px);
}

.dock-pair {
  width: 100%;
  min-height: 48px;
  border-radius: 16px;
}

.add-mark {
  margin-right: 5px;
  font-size: 17px;
  font-weight: 400;
  line-height: 1;
}

@media (hover: hover) {
  .device-entry:hover {
    background-color: rgba(255, 255, 255, 0.96);
    box-shadow: 0 13px 28px rgba(92, 67, 92, 0.13);
    transform: translateY(-2px);
  }

  .dock-pair:hover,
  .empty-pair-link:hover {
    background: var(--neko-primary-deep);
  }
}

@media (max-width: 370px) {
  .device-list-page {
    padding-inline: 16px;
  }

  .brand-hero {
    grid-template-columns: minmax(0, 1fr) 108px;
  }

  .mascot-stage,
  .mascot-stage img {
    width: 108px;
  }

  .mascot-stage img {
    height: 108px;
  }

  .brand-copy h1 {
    font-size: 2rem;
  }

  .brand-intro {
    font-size: 11px;
  }

  .bottom-dock {
    padding-inline: 16px;
  }

  .device-card {
    grid-template-columns: minmax(0, 1fr);
  }

  .unbind-btn {
    justify-self: end;
  }
}
</style>
