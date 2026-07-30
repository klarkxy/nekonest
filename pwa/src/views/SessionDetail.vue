<template>
  <div class="session-detail-page">
    <header class="page-header">
      <RouterLink
        class="back-link"
        :to="deviceDetailLocation(deviceId)"
        aria-label="返回工作目录"
      >← 返回</RouterLink>
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
        <h1>{{ agentLabelText }}</h1>
        <div v-if="projectLine" class="header-project" :title="projectLine.path">
          {{ projectLine.name }}
        </div>
      </div>
      <span
        v-if="showHeaderActivity"
        class="thread-state-pill"
        :class="`thread-state-pill--${threadActivity.tone}`"
        :title="threadActivity.detail"
        aria-hidden="true"
      >
        <span class="thread-state-label">{{ threadActivity.label }}</span>
      </span>
      <span
        class="sr-only"
        role="status"
        aria-live="polite"
        aria-atomic="true"
      >{{ liveStatusText }}</span>
      <span
        v-if="sessionStore.wsStatus !== 'connected'"
        class="ws-pill"
        :class="sessionStore.wsStatus"
        aria-hidden="true"
      >{{ wsLabel }}</span>
      <n-button
        v-if="canInterrupt"
        size="tiny"
        quaternary
        :disabled="sessionStore.wsStatus !== 'connected'"
        aria-label="中断当前任务"
        @click="handleInterrupt"
      >中断</n-button>
    </header>

    <section
      v-if="showActivityBanner"
      class="thread-activity"
      :class="`thread-activity--${threadActivity.tone}`"
      aria-hidden="true"
    >
      <span class="thread-activity-copy">
        <strong>{{ threadActivity.headline }}</strong>
        <span>{{ threadActivity.detail }}</span>
      </span>
    </section>

    <div
      v-if="sessionStore.currentSession?.pending_approval"
      class="approval-banner"
      role="status"
      aria-live="polite"
      aria-atomic="true"
    >
      <div class="approval-title">等电脑点头</div>
      <div class="approval-tool">{{ sessionStore.currentSession.pending_approval.tool_name }}</div>
      <div
        v-if="sessionStore.currentSession.pending_approval.description"
        class="approval-desc"
      >{{ sessionStore.currentSession.pending_approval.description }}</div>
      <pre
        v-if="approvalParamsText"
        class="approval-params"
      >{{ approvalParamsText }}</pre>
      <p class="approval-note">请到家里电脑的终端批准或拒绝。手机不能代点。</p>
    </div>

    <div
      ref="messagesRef"
      class="messages-area"
      role="log"
      aria-live="polite"
      aria-relevant="additions text"
      aria-label="线团消息"
      tabindex="0"
    >
      <div
        v-for="msg in sessionStore.messages"
        :key="msg.id"
        class="message-bubble"
        :class="[msg.role, msg.type]"
      >
        <div v-if="msg.type === 'thinking'" class="thinking-indicator">
          {{ msg.content }}
        </div>
        <div v-else-if="msg.role === 'system'" class="system-msg">
          {{ msg.content }}
        </div>
        <div v-else-if="msg.type === 'tool_call'" class="tool-call-info">
          {{ msg.content }}
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
                  :alt="a.name || '图片附件'"
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
              >{{ a.name || '附件' }}</a>
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
        正在同步家里的记录…
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
        <p class="empty-title">线团还是空的</p>
        <p class="empty-hint">
          打开时会试着同步家里的记录。也可以直接说一句继续；新线团请先在电脑上开。
        </p>
        <n-button size="small" @click="reloadHistory">重新同步</n-button>
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
        <span v-else class="chip-file" aria-hidden="true">文件</span>
        <span class="chip-name">{{ a.name }}</span>
        <button type="button" class="chip-x" :aria-label="`移除附件 ${a.name || ''}`" @click="removeAtt(i)">×</button>
      </div>
    </div>

    <div
      v-if="uploadError || sessionStore.lastError"
      class="error-banner"
      role="alert"
      aria-live="assertive"
    >
      <p>{{ uploadError || sessionStore.lastError }}</p>
      <button type="button" class="error-dismiss" aria-label="关闭提示" @click="dismissErrors">×</button>
    </div>

    <p v-if="sendBlocked" class="compose-status" role="status">
      猫娘还在处理上一条，可以先把下一句写好。
    </p>

    <div class="input-bar">
      <label
        for="session-attachment-input"
        class="attachment-picker"
        :class="{ disabled: sending || uploading }"
        :aria-disabled="sending || uploading ? 'true' : undefined"
      >
        <span class="attachment-picker-icon" aria-hidden="true">＋</span>
        <span class="sr-only">添加附件</span>
        <input
          id="session-attachment-input"
          type="file"
          class="attachment-file"
          multiple
          accept="image/*,.txt,.md,.markdown,.pdf,.json,text/plain,text/markdown,application/pdf,application/json"
          aria-label="添加附件"
          :disabled="sending || uploading"
          @change="onFileChange"
        />
      </label>
      <n-input
        v-model:value="inputText"
        :placeholder="composerPlaceholder"
        type="textarea"
        :autosize="{ minRows: 1, maxRows: 4 }"
        :disabled="sending || uploading"
        aria-label="消息输入"
        @keydown.enter.exact="onEnterKey"
        @paste="onPaste"
      />
      <n-button
        type="primary"
        circle
        :aria-label="sendButtonLabel"
        @click="handleSend"
        :disabled="sendBlocked || (!inputText.trim() && !pendingAtts.length) || sending || uploading"
        :loading="sending || uploading"
      >
        <template #icon>↑</template>
      </n-button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import { useRoute, RouterLink } from 'vue-router'
import { NButton, NInput } from 'naive-ui'
import { getAgentMeta, UNKNOWN_AGENT_META } from '@/config/agents'
import { deviceDetailLocation } from '@/router/navigation'
import { useSessionStore } from '@/stores/session'
import { useDraftStore } from '@/stores/drafts'
import { projectDisplay, sessionActivityPresentation } from '@/utils/agent'
import { renderMarkdown, isMarkdownBubble } from '@/utils/markdown'
import {
  pickAndUpload,
  isImageMime,
  MAX_COUNT,
  type AttachmentRef
} from '@/utils/attachments'
import type { SessionMessage, AttachmentRef as ProtoAtt } from '@/types/protocol'

const route = useRoute()
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
const agentLabelText = computed(() => {
  return sessionStore.currentSession ? agentMeta.value.label : '线团'
})

const projectLine = computed(() => {
  const s = sessionStore.currentSession
  if (!s) return null
  return projectDisplay(s)
})

const threadActivity = computed(() => sessionActivityPresentation(
  sessionStore.currentSession?.status || '',
  sessionStore.streaming
))

const sessionBusy = computed(
  () => sessionStore.currentSession?.status === 'running' || sessionStore.streaming
)

const sendBlocked = computed(
  () => sessionBusy.value && sessionStore.wsStatus === 'connected'
)

const composerPlaceholder = computed(() =>
  sendBlocked.value ? '先写下一句，任务结束后再发送…' : '跟猫娘说点什么…'
)

const sendButtonLabel = computed(() =>
  sendBlocked.value ? '当前任务结束后可发送' : '发送'
)

const hasPendingApproval = computed(() => !!sessionStore.currentSession?.pending_approval)

const showHeaderActivity = computed(() => {
  if (hasPendingApproval.value) return true
  const status = sessionStore.currentSession?.status
  return sessionStore.streaming || status === 'running' || status === 'waiting_approval'
})

/** Full banner only when busy and not already covered by approval card. */
const showActivityBanner = computed(() => {
  if (hasPendingApproval.value) return false
  const status = sessionStore.currentSession?.status
  return sessionStore.streaming || status === 'running'
})

const canInterrupt = sessionBusy

const wsLabel = computed(() => {
  switch (sessionStore.wsStatus) {
    case 'connected': return '通道畅通'
    case 'connecting': return '接通中…'
    case 'auth_error': return '钥匙不对'
    default: return '通道断开'
  }
})

const liveStatusText = computed(() => {
  const parts = [wsLabel.value]
  if (showHeaderActivity.value) {
    parts.push(threadActivity.value.headline, threadActivity.value.detail)
  }
  if (sessionStore.importing) parts.push('正在同步历史')
  if (sessionStore.lastError) parts.push(sessionStore.lastError)
  if (uploadError.value) parts.push(uploadError.value)
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

function dismissErrors() {
  uploadError.value = ''
  sessionStore.lastError = null
}

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
    case 'queued': return '排队中…'
    case 'sending': return '发送中…'
    case 'failed': return msg.metadata?.delivery_error || '没送出去'
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
  if (!hasText) {
    ev.preventDefault()
  }
  await addFiles(files)
}

async function addFiles(fileList: FileList | File[]) {
  if (uploading.value) return
  uploadError.value = ''
  if (pendingAtts.value.length >= MAX_COUNT) {
    uploadError.value = `一次最多 ${MAX_COUNT} 个附件`
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
  if (sendBlocked.value) {
    sessionStore.lastError = '猫娘还在处理上一条，结束后再发送'
    return
  }
  let prompt = inputText.value.trim()
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
  inputText.value = ''
  const attsSnapshot = [...pendingAtts.value]
  pendingAtts.value = []
  draftStore.clear(deviceId.value, sessionId.value)

  const ok = sessionStore.sendPrompt(deviceId.value, sessionId.value, prompt, atts)
  if (!ok) {
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
  await new Promise(r => setTimeout(r, 400))
  sending.value = false
}

function reloadHistory() {
  sessionStore.requestNativeHistory(deviceId.value, sessionId.value)
}

function handleInterrupt() {
  sessionStore.interrupt(deviceId.value, sessionId.value)
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
    radial-gradient(ellipse 80% 50% at 10% 0%, rgba(201, 137, 152, 0.12), transparent 50%),
    radial-gradient(ellipse 70% 40% at 90% 100%, rgba(114, 91, 157, 0.12), transparent 55%),
    var(--neko-bg);
}

.page-header {
  flex: 0 0 auto;
  padding: 10px 14px;
  display: flex;
  align-items: center;
  gap: 8px;
  border-bottom: 1px solid var(--neko-line);
  background: rgba(255, 252, 250, 0.92);
  backdrop-filter: blur(10px);
}

.back-link {
  display: inline-flex;
  flex: 0 0 auto;
  min-height: 44px;
  align-items: center;
  padding: 4px 2px;
  color: var(--neko-primary-deep);
  font-size: 14px;
  font-weight: 620;
  text-decoration: none;
  white-space: nowrap;
}

.header-mid { flex: 1; min-width: 0; }
.header-agent-avatar {
  display: block;
  width: 34px;
  height: 34px;
  flex: 0 0 34px;
  border: 2px solid var(--neko-primary);
  border-radius: 11px;
  background: var(--neko-primary-soft);
  box-shadow: 0 3px 9px rgba(104, 83, 121, 0.14);
  object-fit: cover;
}
.page-header h1 {
  font-size: 16px;
  font-weight: 680;
  color: var(--neko-ink);
  margin: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.header-project {
  font-size: 11px;
  color: var(--neko-ink-soft);
  margin-top: 1px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.thread-state-pill {
  display: inline-flex;
  min-height: 26px;
  flex: 0 0 auto;
  align-items: center;
  padding: 3px 8px;
  border: 1px solid #ded7dc;
  border-radius: 999px;
  background: #f3eff2;
  color: #756b72;
  font-size: 10px;
  font-weight: 700;
  white-space: nowrap;
}
.thread-state-pill--active {
  border-color: #bfdfca;
  background: #e9f5ed;
  color: #3e7654;
}
.thread-state-pill--waiting {
  border-color: #ead09f;
  background: #fff4df;
  color: #8a642f;
}
.ws-pill {
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 999px;
  background: var(--neko-surface-muted);
  color: var(--neko-ink-soft);
  white-space: nowrap;
}
.ws-pill.connecting { background: #FFF5E0; color: #C09040; }
.ws-pill.auth_error,
.ws-pill.disconnected { background: #FDE8E8; color: #C05050; }

.thread-activity {
  flex: 0 0 auto;
  padding: 8px 16px;
  border-bottom: 1px solid #d8e9dd;
  background: #f2f8f4;
  color: #3e7654;
}
.thread-activity--waiting {
  border-bottom-color: #ead09f;
  background: #fff8e9;
  color: #815e2f;
}
.thread-activity-copy {
  display: grid;
  gap: 1px;
  min-width: 0;
}
.thread-activity-copy strong {
  font-size: 12px;
  font-weight: 720;
}
.thread-activity-copy > span {
  color: #68736b;
  font-size: 11px;
  line-height: 1.35;
  text-wrap: pretty;
}

.approval-banner {
  flex: 0 0 auto;
  background: #FFF8E8;
  border-bottom: 1px solid #F4D4A0;
  padding: 12px 16px;
}
.approval-title {
  font-weight: 680;
  font-size: 14px;
  color: var(--neko-ink);
  margin-bottom: 4px;
}
.approval-tool {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  color: var(--neko-ink-soft);
  background: var(--neko-surface-muted);
  padding: 4px 8px;
  border-radius: 6px;
  display: inline-block;
  margin-bottom: 4px;
  max-width: 100%;
  overflow-wrap: anywhere;
}
.approval-desc { font-size: 13px; color: var(--neko-ink-soft); line-height: 1.45; }
.approval-params {
  margin: 8px 0 0;
  padding: 8px;
  max-height: 100px;
  overflow: auto;
  font-size: 11px;
  background: rgba(0,0,0,0.04);
  border-radius: 8px;
  white-space: pre-wrap;
  word-break: break-word;
}
.approval-note {
  margin: 8px 0 0;
  font-size: 12px;
  color: #9E8A6A;
  line-height: 1.45;
}

.messages-area {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 16px;
  display: flex;
  flex-direction: column;
}

.message-bubble.system {
  background: transparent;
  color: var(--neko-ink-faint);
  font-size: 12px;
  text-align: center;
  padding: 4px 8px;
  max-width: 100%;
}
.message-bubble.tool_call,
.message-bubble.thinking {
  background: var(--neko-surface-muted);
  border: 1px solid var(--neko-line);
  color: var(--neko-ink-soft);
  font-size: 12px;
}
.message-bubble .system-msg { color: var(--neko-ink-faint); font-size: 12px; }
.message-bubble .tool-call-info,
.message-bubble .thinking-indicator {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-word;
}
.msg-body { white-space: pre-wrap; word-break: break-word; }

.msg-body.md { white-space: normal; }
.msg-body.md :deep(p) { margin: 0 0 0.6em; }
.msg-body.md :deep(p:last-child) { margin-bottom: 0; }
.msg-body.md :deep(ul), .msg-body.md :deep(ol) { margin: 0.4em 0; padding-left: 1.4em; }
.msg-body.md :deep(li) { margin: 0.15em 0; }
.msg-body.md :deep(pre) {
  overflow-x: auto;
  background: rgba(0,0,0,0.06);
  padding: 10px 12px;
  border-radius: 8px;
  font-size: 12px;
  margin: 0.5em 0;
}
.message-bubble.user .msg-body.md :deep(pre) { background: rgba(255,255,255,0.18); }
.msg-body.md :deep(code) {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 0.9em;
  background: rgba(0,0,0,0.06);
  padding: 0.1em 0.35em;
  border-radius: 4px;
}
.msg-body.md :deep(pre code) { background: none; padding: 0; }
.message-bubble.user .msg-body.md :deep(code) { background: rgba(255,255,255,0.2); }
.msg-body.md :deep(a) { color: var(--neko-primary-deep); word-break: break-all; }
.message-bubble.user .msg-body.md :deep(a) { color: #fff; text-decoration: underline; }
.msg-body.md :deep(blockquote) {
  margin: 0.4em 0;
  padding-left: 0.8em;
  border-left: 3px solid #D0C8E8;
  color: var(--neko-ink-soft);
}
.msg-body.md :deep(h1), .msg-body.md :deep(h2), .msg-body.md :deep(h3) {
  font-size: 1.05em;
  margin: 0.6em 0 0.3em;
  font-weight: 600;
}
.msg-body.md :deep(table) {
  border-collapse: collapse;
  font-size: 12px;
  display: block;
  overflow-x: auto;
}
.msg-body.md :deep(th), .msg-body.md :deep(td) {
  border: 1px solid #E0DCE8;
  padding: 4px 8px;
}

.attach-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 8px;
}
.attach-img-wrap img {
  max-width: 200px;
  max-height: 200px;
  border-radius: 10px;
  object-fit: cover;
  display: block;
  border: 1px solid rgba(0,0,0,0.06);
}
.attach-file {
  font-size: 13px;
  color: inherit;
  text-decoration: underline;
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
.delivery-state.failed { color: #FFF1F1; }
.retry-btn {
  border: 1px solid rgba(255, 255, 255, 0.65);
  border-radius: 999px;
  padding: 2px 8px;
  color: inherit;
  background: rgba(255, 255, 255, 0.12);
  cursor: pointer;
}

.empty-messages {
  text-align: center;
  color: var(--neko-ink-faint);
  padding: 40px 24px;
  margin: auto;
}
.import-banner {
  text-align: center;
  font-size: 13px;
  color: var(--neko-primary-deep);
  padding: 8px;
  background: rgba(236, 229, 245, 0.85);
  border-radius: 10px;
  margin-bottom: 8px;
}
.neko-mascot {
  filter: drop-shadow(0 4px 12px rgba(114, 91, 157, 0.2));
  animation: neko-float 2.4s ease-in-out infinite;
}
.neko-mascot img {
  width: 72px;
  height: 72px;
  border-radius: 50%;
  object-fit: cover;
  border: 2px solid rgba(255,255,255,0.95);
}
.empty-title {
  font-size: 15px;
  color: var(--neko-ink-soft);
  font-weight: 650;
  margin: 12px 0 6px;
}
.empty-hint {
  font-size: 13px;
  line-height: 1.55;
  margin: 0 0 14px;
  color: var(--neko-ink-faint);
  text-wrap: pretty;
}

@keyframes neko-float {
  0%, 100% { transform: translateY(0); }
  50% { transform: translateY(-6px); }
}

.pending-atts {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  padding: 8px 16px 0;
  background: rgba(255, 252, 250, 0.95);
}
.pending-chip {
  display: flex;
  align-items: center;
  gap: 6px;
  background: var(--neko-primary-soft);
  border-radius: 10px;
  padding: 4px 8px 4px 4px;
  font-size: 12px;
  color: var(--neko-primary-deep);
  max-width: 160px;
}
.pending-chip img {
  width: 32px;
  height: 32px;
  border-radius: 6px;
  object-fit: cover;
}
.chip-file {
  display: grid;
  place-items: center;
  width: 32px;
  height: 32px;
  border-radius: 6px;
  background: rgba(114, 91, 157, 0.12);
  font-size: 10px;
  font-weight: 700;
}
.chip-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
}
.chip-x {
  border: none;
  background: transparent;
  cursor: pointer;
  font-size: 16px;
  line-height: 1;
  color: var(--neko-ink-soft);
  padding: 0 2px;
}

.error-banner {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  margin: 0;
  padding: 8px 12px 8px 16px;
  border-top: 1px solid rgba(191, 104, 116, 0.2);
  background: rgba(249, 231, 233, 0.95);
  color: #784951;
  font-size: 12px;
  line-height: 1.45;
}
.error-banner p {
  flex: 1;
  margin: 0;
  min-width: 0;
}
.error-dismiss {
  flex: 0 0 auto;
  width: 32px;
  height: 32px;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: inherit;
  font-size: 18px;
  line-height: 1;
  cursor: pointer;
}

.compose-status {
  flex: 0 0 auto;
  margin: 0;
  padding: 7px 16px;
  border-top: 1px solid rgba(188, 132, 72, 0.2);
  color: #7d623f;
  background: rgba(255, 247, 230, 0.94);
  font-size: 11px;
  line-height: 1.45;
  text-align: center;
  text-wrap: pretty;
}

.compose-status + .input-bar {
  border-top: 0;
}

.input-bar {
  flex: 0 0 auto;
  padding: 12px 16px;
  border-top: 1px solid var(--neko-line);
  background: rgba(255, 252, 250, 0.96);
  backdrop-filter: blur(10px);
  display: flex;
  gap: 8px;
  align-items: flex-end;
}
.input-bar :deep(.n-input) { flex: 1; min-width: 0; }
.attachment-picker {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: 0 0 auto;
  width: 44px;
  height: 44px;
  border-radius: 50%;
  color: var(--neko-ink);
  background: var(--neko-surface-muted);
  cursor: pointer;
  -webkit-tap-highlight-color: transparent;
  touch-action: manipulation;
}
.attachment-picker:hover {
  background: rgba(114, 91, 157, 0.12);
}
.attachment-picker:focus-within {
  outline: 2px solid var(--neko-primary);
  outline-offset: 2px;
}
.attachment-picker.disabled {
  cursor: not-allowed;
  opacity: 0.5;
}
.attachment-picker-icon {
  font-size: 20px;
  line-height: 1;
  font-weight: 500;
}
.attachment-file {
  position: absolute;
  width: 1px;
  height: 1px;
  margin: -1px;
  padding: 0;
  overflow: hidden;
  clip: rect(0 0 0 0);
  clip-path: inset(50%);
  white-space: nowrap;
  border: 0;
  opacity: 0;
}

@media (max-width: 430px) {
  .page-header {
    gap: 6px;
    padding-inline: 10px;
  }

  .thread-state-label {
    max-width: 4.5em;
    overflow: hidden;
    text-overflow: ellipsis;
  }
}

@media (prefers-reduced-motion: reduce) {
  .neko-mascot {
    animation: none;
  }
}
</style>
