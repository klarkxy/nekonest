<template>
  <div class="device-detail-page">
    <div class="page-header">
      <n-button text @click="$router.back()">← 返回</n-button>
      <h1>{{ device?.name || deviceId }}</h1>
      <span v-if="device" class="status-dot" :class="device.status"></span>
    </div>

    <div v-if="device" class="device-info-card neko-card">
      <div class="info-row">
        <span class="info-label">设备 ID</span>
        <span class="info-value mono">{{ device.id }}</span>
      </div>
      <div class="info-row">
        <span class="info-label">操作系统</span>
        <span class="info-value">{{ device.os === 'windows' ? '🪟 Windows' : device.os }}</span>
      </div>
      <div class="info-row">
        <span class="info-label">状态</span>
        <span class="info-value">
          <span class="status-dot" :class="device.status"></span>
          {{ device.status === 'online' ? '在线' : '离线' }}
        </span>
      </div>
      <div class="info-row">
        <span class="info-label">最后活跃</span>
        <span class="info-value">{{ formatTime(device.last_seen) }}</span>
      </div>
      <div class="info-row">
        <span class="info-label">活跃 Agent</span>
        <span class="info-value">{{ device.active_agents }} 个</span>
      </div>
    </div>

    <!-- Quick actions -->
    <div class="actions-section">
      <h2>快捷操作</h2>
      <div class="action-buttons">
        <n-button @click="$router.push(`/device/${deviceId}`)">
          🐾 查看 Agent 会话
        </n-button>
        <n-button @click="$router.push(`/device/${deviceId}/sessions`)">
          📋 全部会话
        </n-button>
        <n-button quaternary @click="$router.push(`/device/${deviceId}/new-session`)">
          ❓ 如何开启会话
        </n-button>
      </div>
    </div>

    <!-- Agent sessions preview -->
    <div class="sessions-section">
      <h2>Agent 会话</h2>
      <div v-if="sessions.length === 0" class="empty-hint">
        这台设备上暂无活跃 Agent
      </div>
      <div
        v-for="session in sessions"
        :key="session.id"
        class="session-item neko-card"
        @click="$router.push(`/device/${deviceId}/session/${session.id}`)"
      >
        <div class="session-row">
          <span>{{ session.agent_type === 'claude_code' ? '🟣' : session.agent_type === 'kilo' ? '🔴' : '🟢' }}</span>
          <span class="session-summary">{{ session.summary || '空闲会话' }}</span>
          <n-tag :type="statusType(session.status)" size="small" round>{{ statusLabel(session.status) }}</n-tag>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { NButton, NTag } from 'naive-ui'
import { useDeviceStore } from '@/stores/device'
import { useSessionStore } from '@/stores/session'
import { useBindingStore } from '@/stores/binding'
import { apiFetch } from '@/api/http'

const route = useRoute()
const deviceStore = useDeviceStore()
const sessionStore = useSessionStore()
const binding = useBindingStore()

const deviceId = route.params.deviceId as string
const sessions = computed(() => sessionStore.sessions)

const device = computed(() => deviceStore.devices.find(d => d.id === deviceId))

onMounted(() => {
  binding.setLastDevice(deviceId)
  deviceStore.initWebSocket()
  deviceStore.fetchDevices()
  sessionStore.subscribeDevice(deviceId)
  fetchSessions()
})

async function fetchSessions() {
  try {
    const res = await apiFetch(`/api/devices/sessions?device_id=${encodeURIComponent(deviceId)}`)
    if (!res.ok) return
    const data = await res.json()
    if (data.sessions) {
      sessionStore.sessions = data.sessions
    }
  } catch {
    // ignore
  }
}

function statusLabel(status: string): string {
  return { running: '运行中', idle: '空闲', waiting_approval: '等待审批' }[status] || status
}

function statusType(status: string): 'success' | 'default' | 'warning' {
  return { running: 'success', idle: 'default', waiting_approval: 'warning' }[status] as any || 'default'
}

function formatTime(ts: number): string {
  if (!ts) return '从未'
  const date = new Date(ts * 1000)
  const now = new Date()
  const diff = now.getTime() - date.getTime()
  if (diff < 60000) return '刚刚'
  if (diff < 3600000) return `${Math.floor(diff / 60000)} 分钟前`
  if (diff < 86400000) return `${Math.floor(diff / 3600000)} 小时前`
  return date.toLocaleDateString('zh-CN')
}
</script>

<style scoped>
.device-detail-page {
  padding: 20px;
}

.page-header {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 20px;
}

.page-header h1 {
  font-size: 20px;
  font-weight: 600;
  color: #4A4A4A;
  margin: 0;
  flex: 1;
}

.device-info-card {
  margin-bottom: 24px;
}

.info-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 0;
  border-bottom: 1px solid #F5F3F0;
}

.info-row:last-child {
  border-bottom: none;
}

.info-label {
  font-size: 13px;
  color: #9E9E9E;
}

.info-value {
  font-size: 14px;
  color: #4A4A4A;
  display: flex;
  align-items: center;
  gap: 6px;
}

.info-value.mono {
  font-family: monospace;
  font-size: 12px;
  color: #6A6A6A;
}

h2 {
  font-size: 16px;
  font-weight: 600;
  color: #4A4A4A;
  margin: 0 0 12px;
}

.actions-section {
  margin-bottom: 24px;
}

.action-buttons {
  display: flex;
  gap: 12px;
}

.action-buttons .n-button {
  flex: 1;
}

.empty-hint {
  text-align: center;
  color: #9E9E9E;
  font-size: 13px;
  padding: 24px;
}

.session-item {
  cursor: pointer;
  padding: 12px;
}

.session-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.session-summary {
  flex: 1;
  font-size: 14px;
  color: #4A4A4A;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
