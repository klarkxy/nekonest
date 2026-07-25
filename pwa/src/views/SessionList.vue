<template>
  <div class="session-list-page">
    <div class="page-header">
      <n-button text @click="$router.back()">← 返回</n-button>
      <h1>{{ deviceId }}</h1>
    </div>

    <div class="session-cards">
      <div
        v-for="session in sessionStore.sessions"
        :key="session.id"
        class="neko-card session-card"
        @click="goToSession(session)"
      >
        <div class="session-header">
          <span class="agent-icon">{{ agentIcon(session.agent_type) }}</span>
          <span class="session-type">{{ agentTypeLabel(session.agent_type) }}</span>
          <n-tag :type="statusTagType(session.status)" size="small" round>
            {{ statusLabel(session.status) }}
          </n-tag>
        </div>
        
        <div v-if="session.summary" class="session-summary">
          {{ session.summary }}
        </div>

        <div v-if="session.pending_approval" class="approval-hint">
          ⚠️ 等待审批: {{ session.pending_approval.tool_name }}
        </div>

        <div class="session-time">
          {{ formatTime(session.last_activity) }}
        </div>
      </div>

      <!-- Empty state -->
      <div v-if="sessionStore.sessions.length === 0" class="empty-state">
        <div class="empty-icon">💤</div>
        <p>这台电脑上没有活跃的 Agent</p>
        <p class="hint">在 PC 上打开 Claude Code / Codex，稍等几秒后下拉刷新</p>
      </div>
    </div>

    <div class="bottom-bar">
      <n-button block size="large" @click="refresh">🔄 刷新会话</n-button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NButton, NTag } from 'naive-ui'
import { useSessionStore } from '@/stores/session'
import type { AgentSession, AgentType, AgentStatus } from '@/types/protocol'

const route = useRoute()
const router = useRouter()
const sessionStore = useSessionStore()

const deviceId = route.params.deviceId as string

onMounted(() => {
  sessionStore.subscribeDevice(deviceId)
})

function refresh() {
  sessionStore.subscribeDevice(deviceId)
}

function goToSession(session: AgentSession) {
  router.push(`/device/${deviceId}/session/${session.id}`)
}

function agentIcon(type: AgentType): string {
  switch (type) {
    case 'claude_code': return '🟣'
    case 'kilo': return '🔴'
    default: return '🟢'
  }
}

function agentTypeLabel(type: AgentType): string {
  switch (type) {
    case 'claude_code': return 'Claude Code'
    case 'codex': return 'Codex'
    case 'kilo': return 'Kilo'
    default: return type
  }
}

function statusLabel(status: AgentStatus): string {
  const labels: Record<AgentStatus, string> = {
    running: '运行中',
    idle: '空闲',
    waiting_approval: '等待审批'
  }
  return labels[status]
}

function statusTagType(status: AgentStatus): 'success' | 'default' | 'warning' {
  const types: Record<AgentStatus, 'success' | 'default' | 'warning'> = {
    running: 'success',
    idle: 'default',
    waiting_approval: 'warning'
  }
  return types[status]
}

function formatTime(ts: number): string {
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
.session-list-page {
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
}

.session-cards {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.session-card {
  cursor: pointer;
}

.session-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}

.agent-icon {
  font-size: 16px;
}

.session-type {
  font-size: 15px;
  font-weight: 600;
  color: #4A4A4A;
  flex: 1;
}

.session-summary {
  font-size: 13px;
  color: #6A6A6A;
  margin-bottom: 8px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.approval-hint {
  font-size: 12px;
  color: #E8A060;
  background: #FFF8E8;
  padding: 6px 10px;
  border-radius: 8px;
  margin-bottom: 8px;
}

.session-time {
  font-size: 11px;
  color: #BDBDBD;
}

.empty-state {
  text-align: center;
  padding: 48px 20px;
  color: #9E9E9E;
}

.empty-icon {
  font-size: 48px;
  margin-bottom: 12px;
}

.bottom-bar {
  position: fixed;
  bottom: 0;
  left: 50%;
  transform: translateX(-50%);
  width: 100%;
  max-width: 480px;
  padding: 12px 20px;
  background: rgba(250, 248, 245, 0.95);
  backdrop-filter: blur(10px);
  border-top: 1px solid #E8E4E0;
}
</style>
