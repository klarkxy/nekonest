<template>
  <div class="thread-list">
    <div class="toolbar">
      <label class="archive-toggle">
        <input v-model="prefs.showArchived" type="checkbox" />
        显示已归档
      </label>
      <div class="sort-row">
        <label class="sort-label" for="session-sort">线程排序</label>
        <select
          id="session-sort"
          class="sort-select"
          :value="prefs.sortMode"
          @change="onSortChange"
        >
          <option value="recent">最近活跃</option>
          <option value="name">标题</option>
          <option value="manual">手动</option>
        </select>
      </div>
    </div>
    <p class="hint">目录与智能体可分别收起；归档只影响手机端。手动排序用 ▲▼。</p>

    <div v-if="visibleProjects.length === 0" class="empty-hint">
      {{ prefs.showArchived ? '没有会话' : '暂无活跃会话（或都已归档）' }}
    </div>

    <section
      v-for="project in visibleProjects"
      :key="project.key"
      class="project-group"
      :class="{ uncategorized: project.uncategorized }"
      :aria-labelledby="projectHeadingId(project.key)"
    >
      <button
        :id="projectHeadingId(project.key)"
        type="button"
        class="project-header"
        :aria-expanded="!prefs.isCollapsed(projectNodeKey(project.key))"
        :aria-controls="projectPanelId(project.key)"
        @click="prefs.toggleCollapse(projectNodeKey(project.key))"
      >
        <span class="folder-icon" aria-hidden="true">
          {{ project.uncategorized ? '🧺' : '📁' }}
        </span>
        <span class="project-copy">
          <span class="project-title">{{ project.label }}</span>
          <span v-if="project.path" class="project-path" :title="project.path">
            {{ shortPath(project.path, 54) }}
          </span>
          <span v-else class="project-path">没有可识别的工作目录</span>
        </span>
        <span class="project-count">{{ project.sessionCount }}</span>
        <span class="chevron" aria-hidden="true">
          {{ prefs.isCollapsed(projectNodeKey(project.key)) ? '▸' : '▾' }}
        </span>
      </button>

      <div
        v-show="!prefs.isCollapsed(projectNodeKey(project.key))"
        :id="projectPanelId(project.key)"
        class="project-body"
      >
        <section
          v-for="agent in project.agents"
          :key="agent.key"
          class="agent-group"
          :style="agentStyle(agent)"
          :aria-labelledby="agentHeadingId(agent.key)"
        >
          <button
            :id="agentHeadingId(agent.key)"
            type="button"
            class="agent-header"
            :aria-expanded="!prefs.isCollapsed(agentNodeKey(project.key, agent.type))"
            :aria-controls="agentPanelId(agent.key)"
            @click="prefs.toggleCollapse(agentNodeKey(project.key, agent.type))"
          >
            <img
              class="agent-avatar"
              :src="agent.avatar"
              alt=""
              width="36"
              height="36"
              @error="onAvatarError"
            />
            <span class="agent-copy">
              <span class="agent-title">{{ agent.label }}</span>
              <span class="agent-subtitle">{{ agent.sessions.length }} 条线程</span>
            </span>
            <span class="agent-chevron" aria-hidden="true">
              {{ prefs.isCollapsed(agentNodeKey(project.key, agent.type)) ? '▸' : '▾' }}
            </span>
          </button>

          <div
            v-show="!prefs.isCollapsed(agentNodeKey(project.key, agent.type))"
            :id="agentPanelId(agent.key)"
            class="agent-body"
          >
            <div
              v-for="(session, index) in agent.sessions"
              :key="session.id"
              class="session-item"
              :class="{
                archived: prefs.isArchived(session.id),
                manual: prefs.sortMode === 'manual'
              }"
            >
              <button
                type="button"
                class="session-main"
                :aria-label="`打开会话：${shortSummary(session.summary)}`"
                @click="$emit('open', session.id)"
              >
                <span class="session-summary">{{ shortSummary(session.summary) }}</span>
                <n-tag :type="statusTagType(session.status)" size="small" round>
                  {{ statusLabel(session.status) }}
                </n-tag>
              </button>

              <div class="session-actions">
                <template v-if="prefs.sortMode === 'manual'">
                  <button
                    type="button"
                    class="icon-btn"
                    title="上移"
                    aria-label="上移会话"
                    :disabled="index === 0"
                    @click.stop="
                      prefs.moveSession(session.id, -1, agent.sessions.map(item => item.id))
                    "
                  >▲</button>
                  <button
                    type="button"
                    class="icon-btn"
                    title="下移"
                    aria-label="下移会话"
                    :disabled="index === agent.sessions.length - 1"
                    @click.stop="
                      prefs.moveSession(session.id, 1, agent.sessions.map(item => item.id))
                    "
                  >▼</button>
                </template>
                <button
                  type="button"
                  class="archive-btn"
                  :class="{ on: prefs.isArchived(session.id) }"
                  :title="prefs.isArchived(session.id) ? '取消归档' : '归档到手机侧'"
                  :aria-label="prefs.isArchived(session.id) ? '取消归档' : '归档会话'"
                  @click.stop="prefs.toggleArchive(session.id)"
                >
                  {{ prefs.isArchived(session.id) ? '取消归档' : '归档' }}
                </button>
              </div>
            </div>
          </div>
        </section>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, type CSSProperties } from 'vue'
import { NTag } from 'naive-ui'
import { UNKNOWN_AGENT_META } from '@/config/agents'
import { useSessionPrefsStore, type SessionSortMode } from '@/stores/sessionPrefs'
import type { AgentSession, AgentType } from '@/types/protocol'
import { shortSummary, statusLabel, statusTagType } from '@/utils/agent'
import { buildSessionTree, type SessionTreeAgent } from '@/utils/sessionTree'

const props = defineProps<{
  sessions: AgentSession[]
}>()

defineEmits<{
  open: [id: string]
}>()

const prefs = useSessionPrefsStore()

const visibleProjects = computed(() => {
  const filtered = props.sessions.filter(
    session => prefs.showArchived || !prefs.isArchived(session.id)
  )
  return buildSessionTree(filtered, list => prefs.sortSessions(list))
})

function onSortChange(event: Event) {
  const value = (event.target as HTMLSelectElement).value as SessionSortMode
  prefs.setSortMode(value)
}

function projectNodeKey(projectKey: string): string {
  return `project:${projectKey}`
}

function agentNodeKey(projectKey: string, agentType: AgentType): string {
  return `agent:${projectKey}:${agentType}`
}

function projectHeadingId(projectKey: string): string {
  return domId('project-heading', projectKey)
}

function projectPanelId(projectKey: string): string {
  return domId('project-panel', projectKey)
}

function agentHeadingId(agentKey: string): string {
  return domId('agent-heading', agentKey)
}

function agentPanelId(agentKey: string): string {
  return domId('agent-panel', agentKey)
}

function domId(prefix: string, value: string): string {
  return `${prefix}-${encodeURIComponent(value).replace(/%/g, '_')}`
}

function agentStyle(agent: SessionTreeAgent): CSSProperties {
  return {
    '--agent-color': agent.color,
    '--agent-soft-color': agent.softColor
  } as CSSProperties
}

function onAvatarError(event: Event) {
  const image = event.currentTarget as HTMLImageElement
  if (image.dataset.fallbackApplied === '1') {
    image.hidden = true
    return
  }
  image.dataset.fallbackApplied = '1'
  image.src = UNKNOWN_AGENT_META.avatar
}

function shortPath(path: string, max = 36): string {
  const normalized = path.replace(/\\/g, '/')
  if (normalized.length <= max) return normalized
  return `…${normalized.slice(-(max - 1))}`
}
</script>

<style scoped>
.toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 8px;
}

.archive-toggle,
.sort-row {
  display: flex;
  align-items: center;
  gap: 6px;
}

.archive-toggle {
  min-height: 44px;
  color: #7f7784;
  font-size: 12px;
  user-select: none;
}

.sort-label {
  color: #938c98;
  font-size: 12px;
}

.sort-select {
  min-height: 44px;
  padding: 5px 9px;
  border: 1px solid #e0d8e7;
  border-radius: 9px;
  background: rgba(255, 255, 255, 0.9);
  color: #4a4450;
  font-size: 13px;
}

.hint {
  margin: 0 0 14px;
  color: #aaa0b0;
  font-size: 11px;
  line-height: 1.5;
}

.empty-hint {
  padding: 28px 20px;
  color: #948c99;
  font-size: 13px;
  text-align: center;
}

.project-group {
  margin-bottom: 14px;
  overflow: hidden;
  border: 1px solid rgba(190, 174, 205, 0.42);
  border-radius: 18px;
  background: rgba(255, 253, 255, 0.82);
  box-shadow: 0 8px 24px rgba(105, 82, 118, 0.08);
}

.project-group.uncategorized {
  border-style: dashed;
  background: rgba(249, 247, 251, 0.8);
}

.project-header {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto auto;
  width: 100%;
  align-items: center;
  gap: 10px;
  padding: 13px 14px;
  border: 0;
  background:
    radial-gradient(circle at 4% 0%, rgba(229, 196, 218, 0.28), transparent 42%),
    rgba(250, 246, 252, 0.92);
  color: inherit;
  cursor: pointer;
  font: inherit;
  text-align: left;
  transition: background-color 180ms ease;
}

.project-header:focus-visible,
.agent-header:focus-visible,
.session-main:focus-visible,
.icon-btn:focus-visible,
.archive-btn:focus-visible {
  outline: 2px solid #8f7fc2;
  outline-offset: -2px;
}

.folder-icon {
  font-size: 21px;
  line-height: 1;
}

.project-copy,
.agent-copy {
  min-width: 0;
}

.project-copy {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.project-title {
  overflow: hidden;
  color: #47404b;
  font-size: 15px;
  font-weight: 650;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.project-path {
  overflow: hidden;
  color: #958a9c;
  font-size: 10px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.project-count {
  min-width: 22px;
  padding: 2px 7px;
  border-radius: 8px;
  background: rgba(126, 103, 146, 0.1);
  color: #7d6d87;
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  text-align: center;
}

.chevron,
.agent-chevron {
  color: #9f95a4;
  font-size: 12px;
}

.project-body {
  padding: 2px 13px 10px;
}

.agent-group + .agent-group {
  border-top: 1px solid rgba(210, 202, 214, 0.62);
}

.agent-header {
  display: flex;
  width: 100%;
  align-items: center;
  gap: 10px;
  padding: 11px 2px 9px;
  border: 0;
  background: transparent;
  color: inherit;
  cursor: pointer;
  font: inherit;
  text-align: left;
}

.agent-avatar {
  display: block;
  width: 36px;
  height: 36px;
  flex: 0 0 36px;
  border: 2px solid color-mix(in srgb, var(--agent-color) 58%, white);
  border-radius: 12px;
  background: var(--agent-soft-color);
  box-shadow: 0 4px 10px color-mix(in srgb, var(--agent-color) 22%, transparent);
  object-fit: cover;
}

.agent-copy {
  display: flex;
  flex: 1;
  flex-direction: column;
}

.agent-title {
  color: #4c4650;
  font-size: 14px;
  font-weight: 650;
}

.agent-subtitle {
  margin-top: 1px;
  color: color-mix(in srgb, var(--agent-color) 70%, #777);
  font-size: 10px;
}

.agent-body {
  margin: 0 0 3px 18px;
  padding-left: 12px;
  border-left: 2px solid var(--agent-soft-color);
}

.session-item {
  display: flex;
  align-items: stretch;
  gap: 8px;
  min-height: 52px;
  padding: 8px 0;
  border-top: 1px solid rgba(228, 222, 230, 0.72);
}

.session-item:first-child {
  border-top: 0;
}

.session-item.archived {
  opacity: 0.55;
}

.session-main {
  display: flex;
  min-width: 0;
  flex: 1;
  align-items: center;
  gap: 8px;
  padding: 2px 4px;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: inherit;
  cursor: pointer;
  font: inherit;
  text-align: left;
}

.session-summary {
  overflow: hidden;
  flex: 1;
  color: #514b55;
  font-size: 13px;
  line-height: 1.35;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.session-actions {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 4px;
}

.icon-btn,
.archive-btn {
  border: 1px solid #e3dce8;
  color: #6d6275;
  cursor: pointer;
  font-weight: 600;
  transition: transform 160ms ease, background-color 160ms ease;
}

.icon-btn {
  width: 44px;
  height: 44px;
  padding: 0;
  border-radius: 10px;
  background: #fbf9fc;
  font-size: 11px;
}

.icon-btn:disabled {
  cursor: default;
  opacity: 0.32;
}

.archive-btn {
  min-width: 52px;
  min-height: 44px;
  padding: 0 10px;
  border-color: #dfd3e8;
  border-radius: 8px;
  background: #f6f0fa;
  color: #705f9b;
  font-size: 11px;
  white-space: nowrap;
}

.archive-btn.on {
  border-color: #ddd;
  background: #eee;
  color: #858085;
}

@media (max-width: 430px) {
  .session-item.manual {
    flex-direction: column;
    gap: 4px;
  }

  .session-item.manual .session-main {
    min-height: 44px;
  }

  .session-item.manual .session-actions {
    align-self: flex-end;
  }
}

@media (hover: hover) {
  .project-header:hover {
    background-color: rgba(244, 237, 248, 0.94);
  }

  .agent-header:hover,
  .session-main:hover {
    background: color-mix(in srgb, var(--agent-soft-color, #eeeaf8) 56%, transparent);
  }
}

.project-header:active,
.agent-header:active,
.session-main:active,
.icon-btn:active:not(:disabled),
.archive-btn:active {
  transform: scale(0.985);
}

@media (prefers-reduced-motion: reduce) {
  .project-header,
  .icon-btn,
  .archive-btn {
    transition: none;
  }

  .project-header:active,
  .agent-header:active,
  .session-main:active,
  .icon-btn:active,
  .archive-btn:active {
    transform: none;
  }
}
</style>
