<template>
  <div class="session-detail-page">
    <div class="page-header">
      <n-button text @click="goBack">← 返回</n-button>
      <img
        class="header-agent-avatar"
        :src="agentMeta.avatar"
        alt=""
        width="34"
        height="34"
        :style="{ borderColor: agentMeta.color }"
        @load="onAgentAvatarLoad"
        @error="onAgentAvatarError"
      />
      <div class="header-mid">
        <h1>{{ agentLabel }}</h1>
        <div v-if="projectLine" class="header-project" :title="projectLine.path">
          📁 {{ projectLine.name }}
        </div>
      </div>
      <span v-if="sessionStore.currentSession?.status === 'running'" class="status-dot online"></span>
      <span v-else-if="sessionStore.currentSession?.status === 'waiting_approval'" class="status-dot waiting"></span>
      <span
        class="sr-only"
        role="status"
        aria-live="polite"
        aria-atomic="true"
      >{{ liveStatusText }}</span>
      <span v-if="sessionStore.streaming" class="ws-pill streaming" aria-hidden="true">流式中</span>
      <span class="ws-pill" :class="sessionStore.wsStatus" aria-hidden="true">{{ wsLabel }}</span>
      <n-button
        v-if="sessionStore.currentSession?.status === 'running' || sessionStore.streaming"
        size="tiny"
        quaternary
        :disabled="sessionStore.wsStatus !== 'connected'"
        aria-label="中断当前任务"
        @click="handleInterrupt"
      >中断</n-button>
    </div>

    <div
      v-if="sessionStore.currentSession?.pending_approval"
      class="approval-banner"
      role="status"
      aria-live="polite"
      aria-atomic="true"
    >
      <div class="approval-info">
        <div class="approval-title">⚠️ 工具调用请求</div>
        <div class="approval-tool">{{ sessionStore.currentSession.pending_approval.tool_name }}</div>
        <div class="approval-desc">{{ sessionStore.currentSession.pending_approval.description }}</div>
        <pre
          v-if="approvalParamsText"
          class="approval-params"
        >{{ approvalParamsText }}</pre>
        <p class="approval-note">
          当前远程审批不可用（Agent 以非交互 print/exec 运行，stdin 已关闭）。请在 PC 终端批准或拒绝。
        </p>
      </div>
      <div class="approval-actions">
        <button
          type="button"
          class="deny-btn"
          :disabled="!canApprove"
          @click="handleDeny"
        >拒绝</button>
        <button
          type="button"
          class="approve-btn"
          :disabled="!canApprove"
          @click="handleApprove"
        >批准</button>
      </div>
    </div>

    <div
      ref="messagesRef"
      class="messages-area"
      role="log"
      aria-live="polite"
      aria-relevant="additions text"
      aria-label="会话消息"
      tabindex="0"
    >
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
        <template v-else>
          <div
            v-if="isMarkdownBubble(msg)"
            class="msg-body md"
            v-html="renderMarkdown(msg.content || '')"
          />
          <div v-else class="msg-body">{{ msg.content }}</div>
          <div v-if="msgAttachments(msg).length" class="attach-row">
            <template v-for="(a, i) in msgAttachments(msg)" :key="a.id || a.url || i">
              <a
                v-if="isImage(a.mime)"
                class="attach-img-wrap"
                :href="a.url"
                target="_blank"
                rel="noopener"
              >
                <img
                  :src="a.url"
                  :alt="a.name || 'image'"
                  width="240"
                  height="180"
                  loading="lazy"
                />
              </a>
              <a
                v-else
                class="attach-file"
                :href="a.url"
                target="_blank"
                rel="noopener"
              >📎 {{ a.name || '附件' }}</a>
            </template>
          </div>
        </template>
        <div
          v-if="deliveryStatus(msg)"
          class="delivery-state"
          :class="deliveryStatus(msg)"
          role="status"
        >
          <span>{{ deliveryLabel(msg) }}</span>
          <button
            v-if="deliveryStatus(msg) === 'failed' && canRetryMessage(msg)"
            type="button"
            class="retry-btn"
            @click="retryMessage(msg.id)"
          >重试</button>
        </div>
      </div>

      <div v-if="sessionStore.importing" class="import-banner" role="status" aria-live="polite">
        🐱 正在从本机同步会话历史…
      </div>

      <div v-if="sessionStore.messages.length === 0 && !sessionStore.importing" class="empty-messages">
        <div class="neko-mascot" aria-hidden="true">
          <img
            :src="agentMeta.avatar"
            alt=""
            width="72"
            height="72"
            @load="onAgentAvatarLoad"
            @error="onAgentAvatarError"
          />
        </div>
        <p class="empty-title">暂无消息</p>
        <p class="empty-hint">打开时会尝试同步 PC 上 Agent 的历史。也可直接输入或发图续写～</p>
        <n-button size="small" @click="reloadHistory">重新同步历史</n-button>
      </div>
    </div>

    <div v-if="pendingAtts.length" class="pending-atts">
      <div v-for="(a, i) in pendingAtts" :key="a.id || i" class="pending-chip">
        <img
          v-if="a.previewUrl || isImage(a.mime)"
          :src="a.previewUrl || a.url"
          alt=""
          width="40"
          height="40"
        />
        <span v-else>📎</span>
        <span class="chip-name">{{ a.name }}</span>
        <button type="button" class="chip-x" :aria-label="`移除附件 ${a.name || ''}`" @click="removeAtt(i)">×</button>
      </div>
    </div>
    <div v-if="uploadError" class="upload-error" role="alert" aria-live="assertive">{{ uploadError }}</div>
    <div
      v-if="sessionStore.lastError"
      class="upload-error"
      role="alert"
      aria-live="assertive"
    >{{ sessionStore.lastError }}</div>

    <div class="input-bar">
      <span class="attachment-picker">
        <n-button
          circle
          quaternary
          tabindex="-1"
          aria-hidden="true"
          :disabled="sending || uploading"
        >
          📎
        </n-button>
        <input
          type="file"
          class="attachment-file"
          multiple
          accept="image/*,.txt,.md,.pdf,.json,text/plain,text/markdown,application/pdf"
          aria-label="添加附件"
          :disabled="sending || uploading"
          @change="onFileChange"
        />
      </span>
      <n-input
        v-model:value="inputText"
        placeholder="输入指令，或点 📎 加图/附件…"
        type="textarea"
        :autosize="{ minRows: 1, maxRows: 4 }"
        :disabled="sending || uploading"
        aria-label="指令输入"
        @keydown.enter.exact="onEnterKey"
        @paste="onPaste"
      />
      <n-button
        type="primary"
        circle
        aria-label="发送"
        @click="handleSend"
        :disabled="(!inputText.trim() && !pendingAtts.length) || sending || uploading"
        :loading="sending || uploading"
      >
        <template #icon>🐾</template>
      </n-button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NButton, NInput } from 'naive-ui'
import { getAgentMeta, UNKNOWN_AGENT_META } from '@/config/agents'
import { deviceDetailLocation } from '@/router/navigation'
import { useSessionStore } from '@/stores/session'
import { useDraftStore } from '@/stores/drafts'
import { projectDisplay } from '@/utils/agent'
import { renderMarkdown, isMarkdownBubble } from '@/utils/markdown'
import {
  pickAndUpload,
  isImageMime,
  MAX_COUNT,
  type AttachmentRef
} from '@/utils/attachments'
import type { SessionMessage, AttachmentRef as ProtoAtt } from '@/types/protocol'

const route = useRoute()
const router = useRouter()
const sessionStore = useSessionStore()
const draftStore = useDraftStore()

const deviceId = computed(() => String(route.params.deviceId || ''))
// Vue Router already decodes params — do not decodeURIComponent again (breaks literal %).
const sessionId = computed(() => String(route.params.sessionId || ''))
const inputText = ref('')
const sending = ref(false)
const uploading = ref(false)
const uploadError = ref('')
const messagesRef = ref<HTMLElement>()
const pendingAtts = ref<AttachmentRef[]>([])
/** avoid writing draft while restoring */
let restoringDraft = false
let draftSaveTimer: number | null = null
let routeGeneration = 0
let uploadController: AbortController | null = null

const agentMeta = computed(() => getAgentMeta(sessionStore.currentSession?.agent_type))
const agentLabel = computed(() => {
  return sessionStore.currentSession ? agentMeta.value.label : '会话详情'
})

const projectLine = computed(() => {
  const s = sessionStore.currentSession
  if (!s) return null
  return projectDisplay(s)
})

const wsLabel = computed(() => {
  switch (sessionStore.wsStatus) {
    case 'connected': return '已连接'
    case 'connecting': return '连接中'
    case 'auth_error': return '密钥错误'
    default: return '未连接'
  }
})

// Remote approve requires live interactive stdin — print/exec mode does not support it.
const canApprove = computed(() => false)

const liveStatusText = computed(() => {
  const parts = [wsLabel.value]
  if (sessionStore.streaming) parts.push('流式输出中')
  if (sessionStore.importing) parts.push('正在同步历史')
  if (sessionStore.lastError) parts.push(sessionStore.lastError)
  return parts.join('，')
})

const approvalParamsText = computed(() => {
  const p = sessionStore.currentSession?.pending_approval?.parameters
  if (!p || typeof p !== 'object') return ''
  try {
    return JSON.stringify(p, null, 2)
  } catch {
    return ''
  }
})
/** Last session we bound drafts to (route may already have changed when saving). */
let boundDraftKey: { deviceId: string; sessionId: string } | null = null

function saveDraftFor(did: string, sid: string) {
  if (!did || !sid) return
  draftStore.set(did, sid, inputText.value, pendingAtts.value)
}

function saveDraftNow() {
  if (boundDraftKey) {
    saveDraftFor(boundDraftKey.deviceId, boundDraftKey.sessionId)
  } else {
    saveDraftFor(deviceId.value, sessionId.value)
  }
}

function scheduleSaveDraft() {
  if (restoringDraft) return
  if (draftSaveTimer) window.clearTimeout(draftSaveTimer)
  draftSaveTimer = window.setTimeout(() => {
    draftSaveTimer = null
    saveDraftNow()
  }, 200)
}

function restoreDraft() {
  restoringDraft = true
  const d = draftStore.get(deviceId.value, sessionId.value)
  inputText.value = d?.text || ''
  // revoke old previews
  for (const a of pendingAtts.value) {
    if (a.previewUrl) URL.revokeObjectURL(a.previewUrl)
  }
  pendingAtts.value = (d?.attachments || []).map(a => ({
    id: a.id,
    url: a.url,
    name: a.name,
    mime: a.mime,
    size: a.size,
    key: a.key,
    // images can preview from server url
    previewUrl: a.mime?.startsWith('image/') ? a.url : undefined
  }))
  restoringDraft = false
}

function bindSession() {
  routeGeneration++
  uploadController?.abort()
  uploadController = null
  uploading.value = false
  uploadError.value = ''
  const did = deviceId.value
  const sid = sessionId.value
  // Persist draft under the previous session key (not the new route ids).
  if (
    boundDraftKey &&
    (boundDraftKey.deviceId !== did || boundDraftKey.sessionId !== sid)
  ) {
    saveDraftFor(boundDraftKey.deviceId, boundDraftKey.sessionId)
  }
  sessionStore.subscribeDevice(did)
  const session = sessionStore.sessions.find(s => s.id === sid)
  if (session) {
    sessionStore.setCurrentSession(session)
  } else {
    sessionStore.setCurrentSession({
      id: sid,
      device_id: did,
      agent_type: 'unknown',
      status: 'idle',
      summary: '',
      last_activity: 0
    })
  }
  restoreDraft()
  boundDraftKey = { deviceId: did, sessionId: sid }
}

function onAgentAvatarLoad(event: Event) {
  const image = event.currentTarget as HTMLImageElement
  image.hidden = false
  delete image.dataset.fallbackApplied
}

function onAgentAvatarError(event: Event) {
  const image = event.currentTarget as HTMLImageElement
  if (image.dataset.fallbackApplied === '1') {
    image.hidden = true
    return
  }
  image.dataset.fallbackApplied = '1'
  image.src = UNKNOWN_AGENT_META.avatar
}

onMounted(() => {
  bindSession()
})

onUnmounted(() => {
  routeGeneration++
  uploadController?.abort()
  uploadController = null
  if (boundDraftKey) {
    saveDraftFor(boundDraftKey.deviceId, boundDraftKey.sessionId)
  }
  if (draftSaveTimer) window.clearTimeout(draftSaveTimer)
  for (const a of pendingAtts.value) {
    if (a.previewUrl && a.previewUrl.startsWith('blob:')) {
      URL.revokeObjectURL(a.previewUrl)
    }
  }
  sessionStore.setCurrentSession(null)
  boundDraftKey = null
})

watch(
  () => [route.params.deviceId, route.params.sessionId],
  () => {
    bindSession()
  }
)

watch([inputText, pendingAtts], () => {
  scheduleSaveDraft()
}, { deep: true })

watch(
  () => [sessionStore.messages.length, sessionStore.messages.map(m => m.content?.length || 0).join(',')],
  async () => {
    await nextTick()
    if (messagesRef.value) {
      messagesRef.value.scrollTop = messagesRef.value.scrollHeight
    }
  }
)

function msgAttachments(msg: SessionMessage): ProtoAtt[] {
  const a = msg.metadata?.attachments
  return Array.isArray(a) ? a : []
}

function isImage(mime?: string) {
  return !!mime && isImageMime(mime)
}

function deliveryStatus(msg: SessionMessage) {
  return msg.metadata?.delivery_status
}

function deliveryLabel(msg: SessionMessage) {
  switch (deliveryStatus(msg)) {
    case 'queued': return '等待连接后发送'
    case 'sending': return '正在等待 Agent 接收'
    case 'failed': return msg.metadata?.delivery_error || '发送失败'
    default: return ''
  }
}

function canRetryMessage(msg: SessionMessage) {
  return msg.metadata?.delivery_retry_allowed !== false
}

function retryMessage(messageId: string) {
  sessionStore.retryPrompt(messageId)
}

function removeAtt(i: number) {
  const [a] = pendingAtts.value.splice(i, 1)
  if (a?.previewUrl) URL.revokeObjectURL(a.previewUrl)
}

async function onFileChange(ev: Event) {
  const input = ev.target as HTMLInputElement
  if (!input.files?.length) return
  await addFiles(input.files)
  input.value = ''
}

async function onPaste(ev: ClipboardEvent) {
  const items = ev.clipboardData?.items
  if (!items) return
  const files: File[] = []
  let hasText = false
  for (const it of items) {
    if (it.kind === 'string' && (it.type === 'text/plain' || it.type.startsWith('text/'))) {
      hasText = true
    }
    if (it.kind === 'file') {
      const f = it.getAsFile()
      if (f) files.push(f)
    }
  }
  if (!files.length) return
  // Only block default paste when clipboard is file-only; keep text when both present.
  if (!hasText) {
    ev.preventDefault()
  }
  await addFiles(files)
}

async function addFiles(fileList: FileList | File[]) {
  if (uploading.value) return
  uploadError.value = ''
  if (pendingAtts.value.length >= MAX_COUNT) {
    uploadError.value = `最多 ${MAX_COUNT} 个附件`
    return
  }
  const generation = routeGeneration
  const did = deviceId.value
  const sid = sessionId.value
  const controller = new AbortController()
  uploadController?.abort()
  uploadController = controller
  uploading.value = true
  try {
    const room = MAX_COUNT - pendingAtts.value.length
    const slice = Array.from(fileList).slice(0, room)
    const uploaded = await pickAndUpload(slice, {
      deviceId: did,
      sessionId: sid,
      signal: controller.signal
    })
    if (
      controller.signal.aborted ||
      generation !== routeGeneration ||
      deviceId.value !== did ||
      sessionId.value !== sid
    ) {
      for (const attachment of uploaded) {
        if (attachment.previewUrl?.startsWith('blob:')) {
          URL.revokeObjectURL(attachment.previewUrl)
        }
      }
      return
    }
    pendingAtts.value.push(...uploaded)
  } catch (e: unknown) {
    if (!controller.signal.aborted && generation === routeGeneration) {
      uploadError.value = e instanceof Error ? e.message : '上传失败'
    }
  } finally {
    if (uploadController === controller) {
      uploadController = null
      uploading.value = false
    }
  }
}

function onEnterKey(event: KeyboardEvent) {
  if (event.isComposing || event.keyCode === 229) return
  event.preventDefault()
  void handleSend()
}

async function handleSend() {
  let prompt = inputText.value.trim()
  // Only strip our exact injected suffix (not arbitrary user text containing the phrase)
  const mark = '\n\n[NekoNest attachments — local files on this PC]\n'
  const mi = prompt.indexOf(mark)
  if (mi >= 0) {
    prompt = prompt.slice(0, mi).trim()
  }
  if ((!prompt && !pendingAtts.value.length) || sending.value || uploading.value) return
  sending.value = true
  uploadError.value = ''
  const atts = pendingAtts.value.map(a => ({
    id: a.id,
    url: a.url,
    name: a.name,
    mime: a.mime,
    size: a.size
  }))
  // Clear UI first so a second enter/tap cannot double-fire same payload
  inputText.value = ''
  const attsSnapshot = [...pendingAtts.value]
  pendingAtts.value = []
  draftStore.clear(deviceId.value, sessionId.value)

  const ok = sessionStore.sendPrompt(deviceId.value, sessionId.value, prompt, atts)
  if (!ok) {
    // restore on failure
    inputText.value = prompt
    pendingAtts.value = attsSnapshot
    scheduleSaveDraft()
  } else {
    for (const a of attsSnapshot) {
      if (a.previewUrl && a.previewUrl.startsWith('blob:')) {
        URL.revokeObjectURL(a.previewUrl)
      }
    }
  }
  // brief lockout against double-tap
  await new Promise(r => setTimeout(r, 400))
  sending.value = false
}

function reloadHistory() {
  sessionStore.requestNativeHistory(deviceId.value, sessionId.value)
}

function handleApprove() {
  if (!canApprove.value) return
  const approval = sessionStore.currentSession?.pending_approval
  if (approval) sessionStore.approve(deviceId.value, sessionId.value, approval.id)
}

function handleDeny() {
  if (!canApprove.value) return
  const approval = sessionStore.currentSession?.pending_approval
  if (approval) sessionStore.deny(deviceId.value, sessionId.value, approval.id)
}

function handleInterrupt() {
  sessionStore.interrupt(deviceId.value, sessionId.value)
}

function goBack() {
  void router.push(deviceDetailLocation(deviceId.value))
}
</script>

<style scoped>
.session-detail-page {
  display: flex;
  flex-direction: column;
  height: var(--neko-content-block-size, 100dvh);
  min-height: 0;
  overflow: hidden;
  padding: 0;
  background:
    radial-gradient(ellipse 80% 50% at 10% 0%, rgba(255, 182, 193, 0.18), transparent 50%),
    radial-gradient(ellipse 70% 40% at 90% 100%, rgba(184, 169, 232, 0.2), transparent 55%),
    #FAF8F5;
}

.page-header {
  padding: 12px 16px;
  display: flex;
  align-items: center;
  gap: 10px;
  border-bottom: 1px solid #E8E4E0;
  background: rgba(250, 248, 245, 0.92);
  backdrop-filter: blur(10px);
}
.header-mid { flex: 1; min-width: 0; }
.header-agent-avatar {
  display: block;
  width: 34px;
  height: 34px;
  flex: 0 0 34px;
  border: 2px solid #8f7fc2;
  border-radius: 11px;
  background: #eeeaf8;
  box-shadow: 0 3px 9px rgba(104, 83, 121, 0.18);
  object-fit: cover;
}
.page-header h1 {
  font-size: 17px; font-weight: 600; color: #4A4A4A; margin: 0;
}
.header-project {
  font-size: 11px; color: #8A7AA8; margin-top: 2px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.ws-pill {
  font-size: 11px; padding: 2px 8px; border-radius: 999px; background: #EEE; color: #888;
}
.ws-pill.connected { background: #E6F7EE; color: #3A9B6A; }
.ws-pill.connecting { background: #FFF5E0; color: #C09040; }
.ws-pill.auth_error { background: #FDE8E8; color: #C05050; }
.ws-pill.streaming { background: #EDE6FF; color: #6B5BB8; animation: pulse 1.2s ease-in-out infinite; }
@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.55; }
}

.approval-banner {
  background: #FFF8E8; border-bottom: 1px solid #F4D4A0; padding: 16px;
}
.approval-info { margin-bottom: 12px; }
.approval-title { font-weight: 600; font-size: 14px; color: #4A4A4A; margin-bottom: 4px; }
.approval-tool {
  font-family: monospace; font-size: 13px; color: #6A6A6A;
  background: #F5F3F0; padding: 4px 8px; border-radius: 6px;
  display: inline-block; margin-bottom: 4px;
}
.approval-desc { font-size: 13px; color: #6A6A6A; }
.approval-params {
  margin: 8px 0 0;
  padding: 8px;
  max-height: 120px;
  overflow: auto;
  font-size: 11px;
  background: rgba(0,0,0,0.04);
  border-radius: 8px;
  white-space: pre-wrap;
  word-break: break-word;
}
.approval-note {
  margin: 8px 0 0;
  font-size: 11px;
  color: #9E8A6A;
  line-height: 1.4;
}
.approval-actions { display: flex; gap: 12px; }

.messages-area {
  flex: 1; min-height: 0; overflow-y: auto; padding: 16px;
  display: flex; flex-direction: column;
}

.message-bubble.system {
  background: transparent; color: #9E9E9E; font-size: 12px;
  text-align: center; padding: 4px 8px; max-width: 100%;
}
.message-bubble.tool_call {
  background: #FFF8E8; border: 1px solid #F4D4A0; color: #6A6A6A; font-size: 13px;
}
.message-bubble .system-msg { color: #9E9E9E; font-size: 12px; }
.message-bubble .tool-call-info { font-family: monospace; font-size: 12px; }
.msg-body { white-space: pre-wrap; word-break: break-word; }

/* Markdown */
.msg-body.md { white-space: normal; }
.msg-body.md :deep(p) { margin: 0 0 0.6em; }
.msg-body.md :deep(p:last-child) { margin-bottom: 0; }
.msg-body.md :deep(ul), .msg-body.md :deep(ol) { margin: 0.4em 0; padding-left: 1.4em; }
.msg-body.md :deep(li) { margin: 0.15em 0; }
.msg-body.md :deep(pre) {
  overflow-x: auto; background: rgba(0,0,0,0.06); padding: 10px 12px;
  border-radius: 8px; font-size: 12px; margin: 0.5em 0;
}
.message-bubble.user .msg-body.md :deep(pre) { background: rgba(255,255,255,0.18); }
.msg-body.md :deep(code) {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 0.9em;
  background: rgba(0,0,0,0.06); padding: 0.1em 0.35em; border-radius: 4px;
}
.msg-body.md :deep(pre code) { background: none; padding: 0; }
.message-bubble.user .msg-body.md :deep(code) { background: rgba(255,255,255,0.2); }
.msg-body.md :deep(a) { color: #6B5BB8; word-break: break-all; }
.message-bubble.user .msg-body.md :deep(a) { color: #fff; text-decoration: underline; }
.msg-body.md :deep(blockquote) {
  margin: 0.4em 0; padding-left: 0.8em; border-left: 3px solid #D0C8E8; color: #666;
}
.msg-body.md :deep(h1), .msg-body.md :deep(h2), .msg-body.md :deep(h3) {
  font-size: 1.05em; margin: 0.6em 0 0.3em; font-weight: 600;
}
.msg-body.md :deep(table) {
  border-collapse: collapse; font-size: 12px; display: block; overflow-x: auto;
}
.msg-body.md :deep(th), .msg-body.md :deep(td) {
  border: 1px solid #E0DCE8; padding: 4px 8px;
}

.attach-row {
  display: flex; flex-wrap: wrap; gap: 8px; margin-top: 8px;
}
.attach-img-wrap img {
  max-width: 200px; max-height: 200px; border-radius: 10px;
  object-fit: cover; display: block;
  border: 1px solid rgba(0,0,0,0.06);
}
.attach-file {
  font-size: 13px; color: inherit; text-decoration: underline;
  word-break: break-all;
}
.delivery-state {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 6px;
  font-size: 11px;
  color: rgba(255, 255, 255, 0.78);
}
.delivery-state.failed {
  color: #FFF1F1;
}
.retry-btn {
  border: 1px solid rgba(255, 255, 255, 0.65);
  border-radius: 999px;
  padding: 2px 8px;
  color: inherit;
  background: rgba(255, 255, 255, 0.12);
  cursor: pointer;
}

.empty-messages {
  text-align: center; color: #9E9E9E; padding: 48px 24px; margin: auto;
}
.import-banner {
  text-align: center; font-size: 13px; color: #7A6A9A; padding: 8px;
  background: rgba(243, 238, 255, 0.8); border-radius: 10px; margin-bottom: 8px;
}
.neko-mascot {
  filter: drop-shadow(0 4px 12px rgba(184, 169, 232, 0.45));
  animation: neko-float 2.4s ease-in-out infinite;
}
.neko-mascot img {
  width: 72px; height: 72px; border-radius: 50%; object-fit: cover;
  border: 2px solid rgba(255,255,255,0.95);
}
.empty-title { font-size: 15px; color: #6A6A6A; font-weight: 600; margin: 12px 0 6px; }
.empty-hint { font-size: 13px; line-height: 1.5; margin: 0 0 12px; color: #A0A0A0; }

@keyframes neko-float {
  0%, 100% { transform: translateY(0); }
  50% { transform: translateY(-8px); }
}

.pending-atts {
  display: flex; flex-wrap: wrap; gap: 8px;
  padding: 8px 16px 0;
  background: rgba(250, 248, 245, 0.95);
}
.pending-chip {
  display: flex; align-items: center; gap: 6px;
  background: #F0ECF8; border-radius: 10px; padding: 4px 8px 4px 4px;
  font-size: 12px; color: #5A4A8A; max-width: 160px;
}
.pending-chip img {
  width: 32px; height: 32px; border-radius: 6px; object-fit: cover;
}
.chip-name {
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex: 1;
}
.chip-x {
  border: none; background: transparent; cursor: pointer;
  font-size: 16px; line-height: 1; color: #888; padding: 0 2px;
}
.upload-error {
  padding: 4px 16px 0; font-size: 12px; color: #C05050;
}

.input-bar {
  flex: 0 0 auto;
  padding: 12px 16px;
  padding-bottom: 12px;
  border-top: 1px solid #E8E4E0;
  background: rgba(250, 248, 245, 0.95);
  backdrop-filter: blur(10px);
  display: flex;
  gap: 8px;
  align-items: flex-end;
}
.input-bar :deep(.n-input) { flex: 1; }
.attachment-picker {
  position: relative;
  display: inline-flex;
  flex: 0 0 auto;
}
.attachment-file {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  opacity: 0;
  cursor: pointer;
}
.attachment-file:disabled { cursor: not-allowed; }

</style>
