<template>
  <div class="device-list-page">
    <div class="page-header">
      <div class="logo">
        <span class="cat-ear">🐱</span>
        <h1>NekoNest</h1>
      </div>
      <p class="subtitle">猫娘窝 · 手机遥控家里的 AI Agent</p>
    </div>

    <div class="connection-bar" :class="{ connected: deviceStore.connected }">
      <span class="status-dot" :class="deviceStore.connected ? 'online' : 'offline'"></span>
      {{
        deviceStore.authError
          ? '密钥错误'
          : deviceStore.connected
            ? '已连接'
            : '连接中...'
      }}
      <n-button text size="tiny" style="margin-left: auto" @click="goSetup">密钥</n-button>
    </div>

    <div v-if="deviceStore.authError" class="auth-banner">
      手机密钥不正确。请点右上角「密钥」重新设置，需与 VPS 的
      <code>NEKONEST_PHONE_SECRET</code> 一致。
    </div>

    <div class="device-cards">
      <div
        v-for="device in visibleDevices"
        :key="device.id"
        class="neko-card device-card"
        @click="goToDevice(device)"
      >
        <div class="device-header">
          <span class="status-dot" :class="device.status"></span>
          <span class="device-name">{{ device.name }}</span>
          <span class="device-os">Windows</span>
        </div>
        <div class="device-info">
          <span v-if="device.status === 'online'" class="agent-count">
            🐾 {{ device.active_agents }} 个 Agent
          </span>
          <span v-else class="offline-text">zzZ 休眠中...</span>
        </div>
      </div>

      <div v-if="visibleDevices.length === 0 && !deviceStore.loading" class="empty-state">
        <div class="empty-icon">🐱</div>
        <p>还没有猫娘入住呢~</p>
        <n-button type="primary" @click="$router.push('/pair')">配对新电脑</n-button>
      </div>
    </div>

    <div class="bottom-bar">
      <n-button type="primary" block size="large" @click="$router.push('/pair')">
        + 配对新电脑
      </n-button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { NButton } from 'naive-ui'
import { useDeviceStore } from '@/stores/device'
import { useBindingStore } from '@/stores/binding'
import type { Device } from '@/types/protocol'

const router = useRouter()
const deviceStore = useDeviceStore()
const binding = useBindingStore()

/** Prefer bound devices; if none bound, show all from server (self-host convenience). */
const visibleDevices = computed(() => {
  if (binding.bound.length === 0) return deviceStore.devices
  const ids = binding.boundIds
  const filtered = deviceStore.devices.filter(d => ids.has(d.id))
  // Also show bound stubs not yet in server list
  for (const b of binding.bound) {
    if (!filtered.find(d => d.id === b.id)) {
      filtered.push({
        id: b.id,
        name: b.name,
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
  deviceStore.fetchDevices()
})

function goToDevice(device: Device) {
  binding.setLastDevice(device.id)
  router.push(`/device/${device.id}`)
}

function goSetup() {
  router.push('/setup')
}
</script>

<style scoped>
.device-list-page {
  padding: 20px;
  padding-bottom: 80px;
}
.page-header {
  margin-bottom: 16px;
}
.logo {
  display: flex;
  align-items: center;
  gap: 8px;
}
.logo h1 {
  margin: 0;
  font-size: 24px;
}
.subtitle {
  color: #6a6a6a;
  font-size: 13px;
  margin: 4px 0 0;
}
.connection-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  background: #f0eeeb;
  border-radius: 10px;
  font-size: 13px;
  margin-bottom: 16px;
}
.connection-bar.connected {
  background: #e8f5e9;
}
.auth-banner {
  background: #fff3e0;
  border-radius: 10px;
  padding: 12px;
  font-size: 13px;
  margin-bottom: 12px;
  color: #bf360c;
}
.device-card {
  padding: 16px;
  margin-bottom: 12px;
  cursor: pointer;
}
.device-header {
  display: flex;
  align-items: center;
  gap: 8px;
}
.device-name {
  font-weight: 600;
  flex: 1;
}
.device-os {
  font-size: 12px;
  color: #999;
}
.device-info {
  margin-top: 8px;
  font-size: 13px;
  color: #666;
}
.empty-state {
  text-align: center;
  padding: 40px 0;
}
.empty-icon {
  font-size: 48px;
}
.bottom-bar {
  position: fixed;
  bottom: 0;
  left: 0;
  right: 0;
  max-width: 480px;
  margin: 0 auto;
  padding: 16px 20px;
  background: #faf8f5;
}
.status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  display: inline-block;
  background: #bbb;
}
.status-dot.online {
  background: #4caf50;
}
.status-dot.offline {
  background: #bbb;
}
code {
  font-size: 11px;
}
</style>
