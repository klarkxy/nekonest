<template>
  <div class="thread-list">
    <div class="list-controls">
      <label class="control-field">
        <span class="sr-only">{{ t('threadList.filterAgent') }}</span>
        <select
          id="agent-filter"
          class="control-select"
          v-model="agentFilter"
          :aria-label="t('threadList.filterAgent')"
        >
          <option value="">{{ t('threadList.filterAgentAll') }}</option>
          <option
            v-for="opt in agentOptions"
            :key="opt.type"
            :value="opt.type"
          >{{ opt.label }} ({{ opt.count }})</option>
        </select>
      </label>
      <label class="control-field">
        <span class="sr-only">{{ t('threadList.searchPlaceholder') }}</span>
        <input
          v-model="searchQuery"
          type="search"
          class="control-input"
          :placeholder="t('threadList.searchPlaceholder')"
          autocomplete="off"
        />
      </label>
      <div class="control-meta">
        <label class="archive-toggle">
          <input v-model="prefs.showArchived" type="checkbox" class="archive-checkbox" />
          {{ t('threadList.showArchived') }}
        </label>
        <p class="hint">{{ t('threadList.hint') }}</p>
      </div>
    </div>

    <div v-if="visibleProjects.length === 0" class="empty-hint">
      <template v-if="agentFilter && sessions.length > 0">{{ t('threadList.emptyFilter') }}</template>
      <template v-else-if="prefs.showArchived">{{ t('threadList.emptyNone') }}</template>
      <template v-else-if="sessions.length === 0">
        {{ t('threadList.emptyCreateOnPc') }}
      </template>
      <template v-else>{{ t('threadList.emptyAllArchived') }}</template>
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
          <span v-else class="project-path">{{ t('threadList.noPath') }}</span>
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
              <span class="agent-subtitle">{{ t('threadList.agentThreads', { n: agent.sessions.length }) }}</span>
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
              v-for="session in agent.sessions"
              :key="session.id"
              class="session-item"
              :class="{ archived: prefs.isArchived(session.id) }"
            >
              <RouterLink
                class="session-main"
                :to="sessionDetailLocation(deviceId, session.id)"
                :aria-label="t('threadList.openThread', {
                  summary: shortSummary(session.summary),
                  detail: sessionActivityPresentation(session.status).detail
                })"
              >
                <span class="session-summary">{{ shortSummary(session.summary) }}</span>
                <span
                  class="session-status"
                  :class="`session-status--${sessionActivityPresentation(session.status).tone}`"
                  :title="sessionActivityPresentation(session.status).detail"
                >
                  <span class="session-status-icon" aria-hidden="true">
                    {{ sessionActivityPresentation(session.status).icon }}
                  </span>
                  <span>{{ sessionActivityPresentation(session.status).label }}</span>
                </span>
              </RouterLink>

              <div class="session-actions">
                <button
                  type="button"
                  class="archive-btn"
                  :class="{ on: prefs.isArchived(session.id) }"
                  :title="prefs.isArchived(session.id) ? t('threadList.unarchiveTitle') : t('threadList.archiveTitle')"
                  :aria-label="prefs.isArchived(session.id) ? t('threadList.unarchiveAria') : t('threadList.archiveAria')"
                  @click.stop="prefs.toggleArchive(session.id)"
                >
                  {{ prefs.isArchived(session.id) ? t('threadList.unarchive') : t('threadList.archive') }}
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
import { computed, ref, type CSSProperties } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import { UNKNOWN_AGENT_META } from '@/config/agents'
import { sessionDetailLocation } from '@/router/navigation'
import { useSessionPrefsStore } from '@/stores/sessionPrefs'
import type { AgentSession, AgentType } from '@/types/protocol'
import { agentLabel, sessionActivityPresentation, shortSummary } from '@/utils/agent'
import { buildSessionTree, type SessionTreeAgent } from '@/utils/sessionTree'
import { sortSessionsByMode } from '@/utils/sessionSort'

const props = defineProps<{
  sessions: AgentSession[]
  deviceId: string
}>()

const { t } = useI18n()
const prefs = useSessionPrefsStore()
const searchQuery = ref('')
/** Empty string = all agents. */
const agentFilter = ref('')

const agentOptions = computed(() => {
  const counts = new Map<string, number>()
  for (const s of props.sessions) {
    const key = String(s.agent_type || '').trim() || 'unknown'
    counts.set(key, (counts.get(key) || 0) + 1)
  }
  return [...counts.entries()]
    .map(([type, count]) => ({
      type,
      count,
      label: agentLabel(type as AgentType)
    }))
    .sort((a, b) => a.label.localeCompare(b.label, undefined, { sensitivity: 'base' }))
})

const visibleProjects = computed(() => {
  const q = searchQuery.value.trim().toLowerCase()
  const agent = agentFilter.value
  const filtered = props.sessions.filter(session => {
    if (!prefs.showArchived && prefs.isArchived(session.id)) return false
    if (agent && String(session.agent_type || '').trim() !== agent) return false
    if (!q) return true
    // Agent filtering is the top select; search is for thread/folder text only.
    const hay = [
      session.summary,
      session.project,
      session.project_dir,
      session.id
    ]
      .filter(Boolean)
      .join(' ')
      .toLowerCase()
    return hay.includes(q)
  })
  // Product rule: always recent activity first (no manual reorder).
  return buildSessionTree(filtered, list => sortSessionsByMode(list, 'recent'))
})

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
.list-controls {
  display: grid;
  gap: 8px;
  margin-bottom: 12px;
}

.control-field {
  display: block;
  min-width: 0;
}

.control-select,
.control-input {
  width: 100%;
  min-height: 44px;
  padding: 0 12px;
  border: 1px solid var(--neko-line);
  border-radius: 12px;
  color: var(--neko-ink);
  background: var(--neko-surface-solid);
  font: inherit;
}

.control-select:focus-visible,
.control-input:focus-visible {
  outline: 2px solid var(--neko-primary);
  outline-offset: 2px;
}

.control-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 8px 12px;
  min-height: 36px;
  padding-inline: 2px;
}

.archive-toggle {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-height: 36px;
  color: var(--neko-ink-soft);
  font-size: 12px;
  font-weight: 600;
  user-select: none;
}

.archive-checkbox {
  width: 18px;
  height: 18px;
  flex: 0 0 18px;
  accent-color: var(--neko-primary);
}

.hint {
  margin: 0;
  flex: 1 1 10rem;
  color: var(--neko-ink-faint);
  font-size: 11px;
  line-height: 1.45;
  text-align: end;
}

.empty-hint {
  padding: 28px 20px;
  color: var(--neko-ink-soft);
  font-size: 13px;
  text-align: center;
}

.project-group {
  margin-bottom: 14px;
  overflow: hidden;
  border: 1px solid var(--neko-line);
  border-radius: 18px;
  background: var(--neko-surface-solid);
  box-shadow: var(--neko-shadow-soft);
}

.project-group.uncategorized {
  border-style: dashed;
  background: var(--neko-surface-muted);
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
    radial-gradient(circle at 4% 0%, var(--neko-rose-soft), transparent 42%),
    var(--neko-surface);
  color: inherit;
  cursor: pointer;
  font: inherit;
  text-align: left;
  transition: background-color 180ms ease;
}

.project-header:focus-visible,
.agent-header:focus-visible,
.session-main:focus-visible,
.archive-btn:focus-visible {
  outline: 2px solid var(--neko-primary);
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
  color: var(--neko-ink);
  font-size: 15px;
  font-weight: 650;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.project-path {
  overflow: hidden;
  color: var(--neko-ink-faint);
  font-size: 10px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.project-count {
  min-width: 22px;
  padding: 2px 7px;
  border-radius: 8px;
  background: rgba(126, 103, 146, 0.1);
  color: var(--neko-primary-deep);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  text-align: center;
}

.chevron,
.agent-chevron {
  color: var(--neko-ink-faint);
  font-size: 12px;
}

.project-body {
  padding: 2px 13px 10px;
}

.agent-group + .agent-group {
  border-top: 1px solid var(--neko-line);
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
  border: 2px solid color-mix(in srgb, var(--agent-color) 58%, var(--neko-surface-solid));
  border-radius: 12px;
  background: color-mix(in srgb, var(--agent-color) 18%, var(--neko-surface-solid));
  box-shadow: 0 4px 10px color-mix(in srgb, var(--agent-color) 22%, transparent);
  object-fit: cover;
}

.agent-copy {
  display: flex;
  flex: 1;
  flex-direction: column;
}

.agent-title {
  color: var(--neko-ink);
  font-size: 14px;
  font-weight: 650;
}

.agent-subtitle {
  margin-top: 1px;
  color: color-mix(in srgb, var(--agent-color) 45%, var(--neko-ink-soft));
  font-size: 10px;
}

.agent-body {
  margin: 0 0 3px 18px;
  padding-left: 12px;
  border-left: 2px solid color-mix(in srgb, var(--agent-color) 40%, transparent);
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
  text-decoration: none;
}

.session-summary {
  overflow: hidden;
  flex: 1;
  color: var(--neko-ink);
  font-size: 13px;
  line-height: 1.35;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.session-status {
  display: inline-flex;
  min-height: 24px;
  flex: 0 0 auto;
  align-items: center;
  gap: 4px;
  padding: 3px 8px;
  border: 1px solid var(--neko-neutral-line);
  border-radius: 999px;
  background: var(--neko-neutral-soft);
  color: var(--neko-neutral-ink);
  font-size: 10px;
  font-weight: 700;
  line-height: 1.2;
  white-space: nowrap;
}

.session-status--active {
  border-color: var(--neko-success-line);
  background: var(--neko-success-soft);
  color: var(--neko-success-ink);
}

.session-status--waiting {
  border-color: var(--neko-warning-line);
  background: var(--neko-warning-soft);
  color: var(--neko-warning-ink);
}

.session-status--unknown {
  border-style: dashed;
}

.session-status-icon {
  line-height: 1;
}

.session-actions {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 4px;
}

@media (max-width: 390px) {
  .session-status {
    padding-inline: 6px;
  }

  .session-status > span:last-child {
    max-width: 64px;
    overflow: hidden;
    text-overflow: ellipsis;
  }
}

.archive-btn {
  min-width: 52px;
  min-height: 44px;
  padding: 0 10px;
  border: 1px solid var(--neko-line);
  border-radius: 8px;
  background: var(--neko-primary-soft);
  color: var(--neko-primary-deep);
  font-size: 11px;
  font-weight: 600;
  white-space: nowrap;
  cursor: pointer;
  transition: transform 160ms ease, background-color 160ms ease;
}

.archive-btn.on {
  border-color: var(--neko-neutral-line);
  background: var(--neko-neutral-soft);
  color: var(--neko-neutral-ink);
}

@media (hover: hover) {
  .project-header:hover {
    background-color: var(--neko-surface-muted);
  }

  .agent-header:hover,
  .session-main:hover {
    background: color-mix(in srgb, var(--agent-color) 12%, transparent);
  }
}

.project-header:active,
.agent-header:active,
.session-main:active,
.archive-btn:active {
  transform: scale(0.985);
}

@media (prefers-reduced-motion: reduce) {
  .project-header,
  .archive-btn {
    transition: none;
  }

  .project-header:active,
  .agent-header:active,
  .session-main:active,
  .archive-btn:active {
    transform: none;
  }
}
</style>
