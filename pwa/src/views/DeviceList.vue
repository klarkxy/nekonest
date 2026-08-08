<template>
  <div class="device-list-page">
    <header class="brand-hero">
      <div class="brand-copy">
        <p class="eyebrow">{{ t('deviceList.eyebrow') }}</p>
        <h1>{{ t('brand.name') }}</h1>
      </div>

      <figure class="mascot-stage">
        <span class="mascot-glow" aria-hidden="true"></span>
        <img
          src="/brand/nekonest-duo.webp"
          :alt="t('brand.duoAlt')"
          width="88"
          height="88"
        />
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

    <section
      class="version-panel"
      :class="{
        'version-panel--ok': deviceStore.versionStatus.aligned,
        'version-panel--warning': deviceStore.versionStatus.refreshRequired,
        'version-panel--compact': versionCompact
      }"
      :aria-label="t('deviceList.versionTitle')"
      aria-live="polite"
    >
      <div class="version-panel__summary">
        <div>
          <span class="version-panel__title">{{ t('deviceList.versionTitle') }}</span>
          <strong>{{ versionSummary }}</strong>
        </div>
        <button
          v-if="deviceStore.versionStatus.refreshRequired"
          type="button"
          @click="refreshFrontend"
        >
          {{ t('deviceList.refreshNow') }}
        </button>
        <span
          v-else-if="versionCompact && deviceStore.frontendVersion"
          class="version-panel__pill"
          translate="no"
        >{{ displayVersion(deviceStore.frontendVersion) }}</span>
      </div>
      <dl v-if="!versionCompact" class="version-grid">
        <div>
          <dt>{{ t('deviceList.frontendVersion') }}</dt>
          <dd>{{ displayVersion(deviceStore.frontendVersion) }}</dd>
        </div>
        <div>
          <dt>{{ t('deviceList.serverVersion') }}</dt>
          <dd>{{ displayVersion(deviceStore.serverVersion) }}</dd>
        </div>
      </dl>
    </section>

    <div v-if="deviceStore.authError" class="auth-banner" role="alert">
      <strong>{{ t('deviceList.authTitle') }}</strong>
      <span>{{ t('deviceList.authBody') }}</span>
    </div>

    <section class="device-section" aria-labelledby="device-section-title">
      <div class="section-heading">
        <div>
          <h2 id="device-section-title">{{ t('deviceList.sectionTitle') }}</h2>
        </div>
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
            :class="{
              'device-entry--offline': device.status !== 'online',
              'device-entry--daemon-stale': daemonNeedsUpdate(device)
            }"
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
                  <span
                    v-if="daemonNeedsUpdate(device)"
                    class="daemon-update-badge"
                >{{ t('deviceList.daemonUpdateNeeded') }}</span>
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
import { daemonVersionStatus } from '@/utils/componentVersions'

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

const versionSummary = computed(() => {
  const status = deviceStore.versionStatus
  if (status.refreshRequired) return t('deviceList.versionRefreshNeeded')
  if (status.aligned) return t('deviceList.versionAligned')
  if (!deviceStore.serverVersion) return t('deviceList.versionChecking')
  return t('deviceList.versionUnknown')
})

/** Aligned builds only need a one-line status; expand when refresh or versions differ. */
const versionCompact = computed(() =>
  deviceStore.versionStatus.aligned && !deviceStore.versionStatus.refreshRequired
)

onMounted(() => {
  deviceStore.initWebSocket()
  void deviceStore.fetchDevices()
})

function retryDevices() {
  void deviceStore.fetchDevices()
}

async function refreshFrontend() {
  if ('serviceWorker' in navigator) {
    try {
      const registration = await navigator.serviceWorker.getRegistration()
      await registration?.update()
    } catch {
      // A normal reload remains useful when an update probe is unavailable.
    }
  }
  window.location.reload()
}

function displayVersion(version: string): string {
  return version ? `v${version}` : t('deviceList.versionUnreported')
}

function daemonNeedsUpdate(device: Device): boolean {
  return device.status === 'online' && daemonVersionStatus(
    deviceStore.serverVersion,
    device.daemon_version || ''
  ).updateRequired
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
  padding: 20px 20px calc(104px + var(--neko-safe-bottom, 0px));
}

.brand-hero {
  position: relative;
  display: grid;
  grid-template-columns: minmax(0, 1fr) 88px;
  align-items: center;
  min-height: 106px;
  margin-bottom: 12px;
}

.brand-copy {
  position: relative;
  z-index: 1;
  padding: 4px 0;
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
  margin: 3px 0 0;
  color: var(--neko-ink);
  font-family: var(--neko-display);
  font-size: clamp(2rem, 9vw, 2.6rem);
  font-weight: 760;
  letter-spacing: -0.075em;
  line-height: 0.96;
}

.mascot-stage {
  position: relative;
  align-self: start;
  width: 88px;
  margin: 0;
}

.mascot-stage::before {
  position: absolute;
  right: 8px;
  bottom: 8px;
  z-index: -1;
  width: 72px;
  height: 24px;
  border-radius: 50%;
  background: color-mix(in srgb, var(--neko-primary) 22%, transparent);
  filter: blur(13px);
  content: "";
  transform: rotate(-5deg);
}

.mascot-glow {
  position: absolute;
  inset: -14px;
  z-index: -1;
  border-radius: 50%;
  background: var(--neko-primary-soft);
  opacity: 0.35;
}

.mascot-stage img {
  display: block;
  width: 88px;
  height: 88px;
  border: 3px solid var(--neko-panel-border);
  border-radius: 22px 22px 28px 16px;
  box-shadow: var(--neko-shadow-soft);
  object-fit: cover;
  transform: none;
}

.connection-panel {
  display: flex;
  min-height: 44px;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 12px;
  padding: 8px 9px 8px 13px;
  border: 1px solid var(--neko-line);
  border-radius: 13px;
  background: var(--neko-surface-muted);
  box-shadow: inset 0 1px 0 color-mix(in srgb, var(--neko-surface-solid) 55%, transparent);
}

.connection-panel--connected {
  border-color: var(--neko-success-line);
  background: var(--neko-success-soft);
}

.connection-panel--connected .connection-label {
  color: var(--neko-success-ink);
}

.connection-panel--error {
  border-color: var(--neko-danger-line);
  background: var(--neko-danger-soft);
}

.connection-panel--error .connection-label {
  color: var(--neko-danger-ink);
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
  background: var(--neko-primary-soft);
}

.connection-action:disabled {
  cursor: wait;
  opacity: 0.65;
}

.version-panel {
  display: grid;
  gap: 11px;
  margin-bottom: 16px;
  padding: 12px 13px;
  border: 1px solid var(--neko-line);
  border-radius: 14px;
  color: var(--neko-ink-soft);
  background: var(--neko-surface-muted);
}

.version-panel--ok {
  border-color: var(--neko-success-line);
}

.version-panel--compact {
  gap: 0;
  margin-bottom: 12px;
  padding: 8px 13px;
}

.version-panel--warning {
  border-color: var(--neko-danger-line);
  color: var(--neko-danger-ink);
  background: var(--neko-danger-soft);
}

.version-panel__summary {
  display: flex;
  min-height: 44px;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.version-panel--compact .version-panel__summary {
  min-height: 36px;
}

.version-panel__pill {
  flex: 0 0 auto;
  padding: 4px 8px;
  border-radius: 8px;
  color: var(--neko-success-ink);
  background: color-mix(in srgb, var(--neko-surface-solid) 72%, transparent);
  font-size: 11px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}

.version-panel__summary > div {
  display: grid;
  gap: 3px;
}

.version-panel__title {
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.version-panel__summary strong {
  color: var(--neko-ink);
  font-size: 12px;
  line-height: 1.45;
}

.version-panel__summary button {
  flex: 0 0 auto;
  min-height: 44px;
  padding: 0 13px;
  border: 0;
  border-radius: 10px;
  color: #fff;
  background: var(--neko-danger);
  font-size: 12px;
  font-weight: 700;
  cursor: pointer;
}

.version-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
  margin: 0;
}

.version-grid > div {
  min-width: 0;
  padding: 8px;
  border-radius: 9px;
  background: color-mix(in srgb, var(--neko-surface-solid) 72%, transparent);
}

.version-grid dt,
.version-grid dd {
  overflow: hidden;
  margin: 0;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.version-grid dt {
  font-size: 10px;
}

.version-grid dd {
  margin-top: 3px;
  color: var(--neko-ink);
  font-size: 11px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}

.auth-banner {
  display: grid;
  gap: 4px;
  margin: 0 0 16px;
  padding: 13px 14px;
  border-left: 3px solid var(--neko-danger);
  border-radius: 8px 14px 14px 8px;
  color: var(--neko-danger-ink);
  background: var(--neko-danger-soft);
  font-size: 12px;
  line-height: 1.55;
}

.auth-banner strong {
  font-size: 13px;
}

.device-section {
  margin-top: 20px;
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
  margin: 0;
  color: var(--neko-ink);
  font-family: var(--neko-display);
  font-size: 19px;
  font-weight: 710;
  letter-spacing: -0.035em;
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
  border: 1px solid var(--neko-danger-line);
  border-radius: 21px 21px 27px 14px;
  color: var(--neko-danger-ink);
  background: var(--neko-danger-soft);
  text-align: center;
}

.load-failure__mark {
  display: grid;
  width: 52px;
  height: 52px;
  place-items: center;
  border-radius: 17px;
  color: var(--neko-danger);
  background: var(--neko-panel);
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
  border: 1px solid var(--neko-line);
  border-radius: 17px 17px 22px 12px;
  color: var(--neko-ink);
  background: var(--neko-surface-solid);
  box-shadow: var(--neko-shadow-soft);
  cursor: pointer;
  text-align: left;
  text-decoration: none;
  transition: transform 190ms ease, box-shadow 190ms ease, background-color 190ms ease, border-color 190ms ease;
}

.device-entry__accent {
  align-self: stretch;
  border-radius: 0 8px 8px 0;
  background: var(--neko-primary);
}

.device-entry--offline {
  background: var(--neko-surface-muted);
  opacity: 0.92;
}

.device-entry--offline .device-entry__accent {
  background: var(--neko-neutral-ink);
}

.device-entry--daemon-stale {
  border-color: var(--neko-danger-line);
}

.device-entry--offline .device-name,
.device-entry--offline .device-entry__meta {
  color: var(--neko-ink-soft);
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

.daemon-update-badge {
  flex: 0 0 auto;
  padding: 2px 5px;
  border-radius: 5px;
  color: var(--neko-danger-ink);
  background: var(--neko-danger-soft);
  font-size: 10px;
  font-weight: 700;
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
  border: 1px solid var(--neko-danger-line);
  border-radius: 12px;
  color: var(--neko-danger-ink);
  background: var(--neko-danger-soft);
  font-size: 12px;
  font-weight: 650;
  cursor: pointer;
}

.unbind-btn:hover {
  filter: brightness(1.06);
}

.device-skeleton {
  display: grid;
  gap: 11px;
  min-height: 82px;
  padding: 19px 22px;
  border-radius: 17px 17px 22px 12px;
  background: var(--neko-surface-solid);
  border: 1px solid var(--neko-line);
}

.skeleton-line {
  display: block;
  height: 11px;
  border-radius: 5px;
  background: var(--neko-surface-muted);
}

.skeleton-line--title {
  width: 48%;
}

.skeleton-line--meta {
  width: 68%;
  height: 8px;
}

.empty-state {
  display: grid;
  justify-items: center;
  padding: 30px 24px 33px;
  border: 1px dashed var(--neko-line);
  border-radius: 21px 21px 27px 14px;
  background: var(--neko-surface-solid);
  text-align: center;
}

.empty-state img {
  width: 88px;
  height: 88px;
  border-radius: 25px;
  box-shadow: var(--neko-shadow-soft);
  object-fit: cover;
}

.empty-state h3 {
  margin: 16px 0 0;
  font-size: 16px;
}

.empty-state p {
  max-width: 17rem;
  margin: 7px 0 0;
  color: var(--neko-ink-soft);
  font-size: 12px;
  line-height: 1.6;
}

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
  box-shadow: var(--neko-shadow-soft);
}

html[data-theme='dark'] .dock-pair {
  color: #1a1422;
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
  border-top: 1px solid var(--neko-line);
  background: var(--neko-surface-solid);
  box-shadow: 0 -12px 32px rgba(0, 0, 0, 0.18);
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
    border-color: color-mix(in srgb, var(--neko-primary) 40%, var(--neko-line));
    background-color: var(--neko-surface-muted);
    box-shadow: var(--neko-shadow);
    transform: translateY(-2px);
  }

  .dock-pair:hover {
    background: var(--neko-primary-deep);
  }
}

@media (max-width: 370px) {
  .device-list-page {
    padding-inline: 16px;
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
