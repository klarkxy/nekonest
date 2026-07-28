<template>
  <div class="device-list-page">
    <header class="brand-hero">
      <div class="brand-copy">
        <p class="eyebrow">Remote agent atelier</p>
        <h1>
          猫娘窝
          <span>NekoNest</span>
        </h1>
        <p class="brand-intro">回到熟悉的工作目录，叫醒正在待命的猫娘智能体。</p>
      </div>

      <figure class="mascot-stage">
        <span class="mascot-glow" aria-hidden="true"></span>
        <img
          src="/brand/nekonest-duo.webp"
          alt="NekoNest 的两位原创猫娘看板娘"
          width="132"
          height="132"
        />
        <figcaption>双猫值守中</figcaption>
      </figure>
    </header>

    <section
      class="connection-panel"
      :class="{
        'connection-panel--connected': deviceStore.connected,
        'connection-panel--error': deviceStore.authError
      }"
      aria-label="连接状态"
      aria-live="polite"
    >
      <div class="connection-copy">
        <span
          class="status-dot"
          :class="deviceStore.connected ? 'online' : deviceStore.authError ? 'offline' : 'waiting'"
          aria-hidden="true"
        ></span>
        <span class="connection-label">
          {{
            deviceStore.authError
              ? '手机密钥不匹配'
              : deviceStore.connected
                ? '通道已连接'
                : '正在寻找家里的猫窝'
          }}
        </span>
      </div>
      <n-button text size="small" class="setup-link" @click="goSetup">密钥设置</n-button>
    </section>

    <div v-if="deviceStore.authError" class="auth-banner" role="alert">
      <strong>无法连接到 NekoNest。</strong>
      <span>
        请重新设置手机密钥，并确认它与 VPS 的
        <code>NEKONEST_PHONE_SECRET</code> 一致。
      </span>
    </div>

    <section class="device-section" aria-labelledby="device-section-title">
      <div class="section-heading">
        <div>
          <p class="section-kicker">Your nests</p>
          <h2 id="device-section-title">选择一台电脑</h2>
        </div>
        <span class="device-total">{{ visibleDevices.length }} 台</span>
      </div>

      <div v-if="deviceStore.loading && visibleDevices.length === 0" class="device-skeletons" role="status">
        <span class="sr-only">正在读取设备</span>
        <div v-for="index in 2" :key="index" class="device-skeleton" aria-hidden="true">
          <span class="skeleton-line skeleton-line--title"></span>
          <span class="skeleton-line skeleton-line--meta"></span>
        </div>
      </div>

      <div v-else class="device-cards">
        <button
          v-for="device in visibleDevices"
          :key="device.id"
          type="button"
          class="device-entry"
          :class="{ 'device-entry--offline': device.status !== 'online' }"
          :aria-label="`打开设备 ${device.name}，${device.status === 'online' ? '在线' : '离线'}`"
          @click="goToDevice(device)"
        >
          <span class="device-entry__accent" aria-hidden="true"></span>
          <span class="device-entry__body">
            <span class="device-entry__topline">
              <span class="device-name">{{ device.name }}</span>
              <span class="device-os">{{ osLabel(device.os) }}</span>
            </span>
            <span class="device-entry__meta">
              <span class="status-dot" :class="device.status" aria-hidden="true"></span>
              <span>{{ device.status === 'online' ? '在线待命' : '暂时离线' }}</span>
              <span class="meta-divider" aria-hidden="true"></span>
              <span class="agent-count">{{ device.active_agents }} 位 Agent</span>
            </span>
          </span>
          <span class="device-arrow" aria-hidden="true">›</span>
        </button>

        <div v-if="visibleDevices.length === 0" class="empty-state">
          <img
            src="/brand/nekonest-duo.webp"
            alt=""
            width="88"
            height="88"
            aria-hidden="true"
          />
          <h3>猫窝里还没有电脑</h3>
          <p>先在家里的电脑上完成配对，这里就会出现它的工作目录。</p>
          <n-button type="primary" @click="goPair">开始配对</n-button>
        </div>
      </div>
    </section>

    <nav class="bottom-dock" aria-label="设备操作">
      <n-button type="primary" block size="large" @click="goPair">
        <span class="add-mark" aria-hidden="true">＋</span>
        配对新电脑
      </n-button>
    </nav>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { NButton } from 'naive-ui'
import { useDeviceStore } from '@/stores/device'
import { useBindingStore } from '@/stores/binding'
import {
  deviceDetailLocation,
  pairLocation,
  setupLocation
} from '@/router/navigation'
import type { Device } from '@/types/protocol'

const router = useRouter()
const deviceStore = useDeviceStore()
const binding = useBindingStore()

/** Prefer bound devices; if none are bound, show the server list for self-hosted installs. */
const visibleDevices = computed(() => {
  if (binding.bound.length === 0) return deviceStore.devices
  const ids = binding.boundIds
  const filtered = deviceStore.devices.filter(device => ids.has(device.id))
  for (const boundDevice of binding.bound) {
    if (!filtered.find(device => device.id === boundDevice.id)) {
      filtered.push({
        id: boundDevice.id,
        name: boundDevice.name,
        os: 'windows',
        status: 'offline',
        last_seen: 0,
        active_agents: 0
      })
    }
  }
  return filtered
})

onMounted(() => {
  deviceStore.initWebSocket()
  void deviceStore.fetchDevices()
})

function goToDevice(device: Device) {
  binding.setLastDevice(device.id)
  void router.push(deviceDetailLocation(device.id))
}

function goSetup() {
  void router.push(setupLocation())
}

function goPair() {
  void router.push(pairLocation())
}

function osLabel(os: string): string {
  const normalized = os.trim().toLowerCase()
  if (normalized === 'windows') return 'Windows'
  if (normalized === 'darwin' || normalized === 'macos') return 'macOS'
  if (normalized === 'linux') return 'Linux'
  return os || 'Computer'
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
  color: var(--neko-rose);
  font-size: 10px;
  font-weight: 750;
  letter-spacing: 0.13em;
  line-height: 1.4;
  text-transform: uppercase;
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
  font-size: 9px;
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

.setup-link {
  flex: 0 0 auto;
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

.auth-banner code {
  font-size: 10px;
  overflow-wrap: anywhere;
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
  font-size: 9px;
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
}
</style>
