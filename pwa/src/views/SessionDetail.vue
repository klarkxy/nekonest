<template>
  <div class="session-detail-page">
    <!-- Header -->
    <div class="page-header">
      <n-button text @click="$router.back()">← 返回</n-button>
      <h1>{{ agentLabel }}</h1>
      <span v-if="sessionStore.currentSession?.status === 'running'" class="status-dot online"></span>
      <span v-else-if="sessionStore.currentSession?.status === 'waiting_approval'" class="status-dot waiting"></span>
    </div>

    <!-- Approval banner -->
    <div v-if="sessionStore.currentSession?.pending_approval" class="approval-banner">
      <div class="approval-info">
        <div class="approval-title">⚠️ 工具调用请求</div>
        <div class="approval-tool">{{ sessionStore.currentSession.pending_approval.tool_name }}</div>
        <div class="approval-desc">{{ sessionStore.currentSession.pending_approval.description }}</div>
      </div>
      <div class="approval-actions">
        <button class="deny-btn" @click="handleDeny">拒绝</button>
        <button class="approve-btn" @click="handleApprove">批准</button>
      </div>
    </div>

    <!-- Messages -->
    <div class="messages-area" ref="messagesRef">
      <div
        v-for="msg in sessionStore.messages"
        :key="msg.id"
        class="message-bubble"
        :class="[msg.role, msg.type]"
      >
        <div v-if="msg.type === 'thinking'" class="thinking-indicator">
          💭 {{ msg.content }}
        </div>
        <div v-else-if="msg.role === 'system'" class="system-msg">
          {{ msg.content }}
        </div>
        <div v-else-if="msg.type === 'tool_call'" class="tool-call-info">
          🔧 {{ msg.content }}
        </div>
        <div v-else>{{ msg.content }}</div>
      </div>

      <div v-if="sessionStore.messages.length === 0" class="empty-messages">
        <div class="neko-loading">
          <div class="paw"></div>
          <div class="paw"></div>
          <div class="paw"></div>
        </div>
        <p>等待消息...</p>
      </div>
    </div>

    <!-- Input bar -->
    <div class="input-bar">
      <n-input
        v-model:value="inputText"
        placeholder="输入指令..."
        type="textarea"
        :autosize="{ minRows: 1, maxRows: 4 }"
        @keydown.enter.exact.prevent="handleSend"
      />
      <n-button type="primary" circle @click="handleSend" :disabled="!inputText.trim()">
        <template #icon>🐾</template>
      </n-button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import { useRoute } from 'vue-router'
import { NButton, NInput } from 'naive-ui'
import { useSessionStore } from '@/stores/session'

const route = useRoute()
const sessionStore = useSessionStore()

const deviceId = route.params.deviceId as string
const sessionId = route.params.sessionId as string
const inputText = ref('')
const messagesRef = ref<HTMLElement>()

const agentLabel = computed(() => {
  const session = sessionStore.currentSession
  if (!session) return '会话详情'
  return session.agent_type === 'claude_code' ? '🟣 Claude Code' : '🟢 Codex'
})

onMounted(() => {
  // Subscribe to session updates for this device
  sessionStore.subscribeDevice(deviceId)

  // Set currentSession from the sessions list
  const session = sessionStore.sessions.find(s => s.id === sessionId)
  if (session) {
    sessionStore.setCurrentSession(session)
  } else {
    // Session might not be loaded yet, create a placeholder
    sessionStore.setCurrentSession({
      id: sessionId,
      device_id: deviceId,
      agent_type: 'claude_code', // default, will be corrected by server data
      status: 'idle',
      summary: '',
      last_activity: 0
    })
  }
})

onUnmounted(() => {
  // Clear active session so background handlers stop mutating messages.
  // Keep device-level session_list handler; only clear the selected session.
  sessionStore.setCurrentSession(null)
})

// Auto-scroll to bottom when new messages arrive
watch(
  () => sessionStore.messages.length,
  async () => {
    await nextTick()
    if (messagesRef.value) {
      messagesRef.value.scrollTop = messagesRef.value.scrollHeight
    }
  }
)

function handleSend() {
  const prompt = inputText.value.trim()
  if (!prompt) return
  
  sessionStore.sendPrompt(deviceId, sessionId, prompt)
  inputText.value = ''
}

function handleApprove() {
  const approval = sessionStore.currentSession?.pending_approval
  if (approval) {
    sessionStore.approve(deviceId, sessionId, approval.id)
  }
}

function handleDeny() {
  const approval = sessionStore.currentSession?.pending_approval
  if (approval) {
    sessionStore.deny(deviceId, sessionId, approval.id)
  }
}
</script>

<style scoped>
.session-detail-page {
  display: flex;
  flex-direction: column;
  height: 100vh;
  padding: 0;
}

.page-header {
  padding: 12px 16px;
  display: flex;
  align-items: center;
  gap: 12px;
  border-bottom: 1px solid #E8E4E0;
  background: rgba(250, 248, 245, 0.95);
  backdrop-filter: blur(10px);
}

.page-header h1 {
  font-size: 18px;
  font-weight: 600;
  color: #4A4A4A;
  margin: 0;
  flex: 1;
}

/* Approval banner */
.approval-banner {
  background: #FFF8E8;
  border-bottom: 1px solid #F4D4A0;
  padding: 16px;
}

.approval-info {
  margin-bottom: 12px;
}

.approval-title {
  font-weight: 600;
  font-size: 14px;
  color: #4A4A4A;
  margin-bottom: 4px;
}

.approval-tool {
  font-family: monospace;
  font-size: 13px;
  color: #6A6A6A;
  background: #F5F3F0;
  padding: 4px 8px;
  border-radius: 6px;
  display: inline-block;
  margin-bottom: 4px;
}

.approval-desc {
  font-size: 13px;
  color: #6A6A6A;
}

.approval-actions {
  display: flex;
  gap: 12px;
}

/* Messages */
.messages-area {
  flex: 1;
  overflow-y: auto;
  padding: 16px;
  display: flex;
  flex-direction: column;
}

.message-bubble.system {
  background: transparent;
  color: #9E9E9E;
  font-size: 12px;
  text-align: center;
  padding: 4px 8px;
}

.message-bubble.tool_call {
  background: #FFF8E8;
  border: 1px solid #F4D4A0;
  color: #6A6A6A;
  font-size: 13px;
}

.message-bubble .system-msg {
  color: #9E9E9E;
  font-size: 12px;
}

.message-bubble .tool-call-info {
  font-family: monospace;
  font-size: 12px;
}

.empty-messages {
  text-align: center;
  color: #9E9E9E;
  padding: 40px 20px;
}

/* Input bar */
.input-bar {
  padding: 12px 16px;
  border-top: 1px solid #E8E4E0;
  background: rgba(250, 248, 245, 0.95);
  backdrop-filter: blur(10px);
  display: flex;
  gap: 8px;
  align-items: flex-end;
}

.input-bar :deep(.n-input) {
  flex: 1;
}
</style>
