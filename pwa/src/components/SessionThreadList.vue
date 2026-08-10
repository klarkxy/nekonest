<template>
  <div class="thread-list">
    <div class="list-controls">
      <label class="control-field control-field--search">
        <span class="sr-only">{{ t('threadList.searchPlaceholder') }}</span>
        <input
          v-model="searchQuery"
          type="search"
          class="control-input"
          :placeholder="t('threadList.searchPlaceholder')"
          autocomplete="off"
        />
      </label>
      <div class="control-utility-row">
        <label class="control-field control-field--filter">
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
        <label class="archive-toggle">
          <input v-model="prefs.showArchived" type="checkbox" class="archive-checkbox" />
          {{ t('threadList.showArchived') }}
        </label>
      </div>
    </div>

    <div v-if="visibleProjects.length === 0" class="empty-hint">
      <template v-if="searchQuery.trim()">{{ t('threadList.emptySearch') }}</template>
      <template v-else-if="agentFilter && mergedSessions.length > 0">{{ t('threadList.emptyFilter') }}</template>
      <template v-else-if="prefs.showArchived">{{ t('threadList.emptyNone') }}</template>
      <template v-else-if="mergedSessions.length === 0">
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
      <div class="project-header-row">
        <button
          :id="projectHeadingId(project.key)"
          type="button"
          class="project-header"
          :aria-expanded="!prefs.isCollapsed(projectNodeKey(project.key))"
          :aria-controls="projectPanelId(project.key)"
          @click="prefs.toggleCollapse(projectNodeKey(project.key))"
        >
          <span class="folder-icon" aria-hidden="true">
            <svg v-if="project.uncategorized" viewBox="0 0 24 24">
              <path d="M4 9h16l-1.35 9.45a2 2 0 0 1-1.98 1.72H7.33a2 2 0 0 1-1.98-1.72L4 9Z" />
              <path d="m8 9 1.5-5h5L16 9M9 13v3m3-3v3m3-3v3" />
            </svg>
            <svg v-else viewBox="0 0 24 24">
              <path d="M3.5 6.75A1.75 1.75 0 0 1 5.25 5h4l2 2h7.5a1.75 1.75 0 0 1 1.75 1.75v8.5A1.75 1.75 0 0 1 18.75 19H5.25a1.75 1.75 0 0 1-1.75-1.75V6.75Z" />
            </svg>
          </span>
            <span class="project-copy" :title="project.path || undefined">
              <span class="project-title">{{ project.label }}</span>
            </span>
            <span class="project-count">{{ project.sessionCount }}</span>
          <span class="chevron" aria-hidden="true">
            <svg viewBox="0 0 20 20">
              <path d="m6.5 8 3.5 3.5L13.5 8" />
            </svg>
          </span>
        </button>
        <button
          type="button"
          class="archive-btn project-archive-btn"
          :class="{ on: isProjectArchived(project) }"
          :title="isProjectArchived(project) ? t('threadList.unarchiveProjectTitle') : t('threadList.archiveProjectTitle')"
          :aria-label="isProjectArchived(project)
            ? t('threadList.unarchiveProjectAria', { project: project.label })
            : t('threadList.archiveProjectAria', { project: project.label })"
          @click.stop="toggleProjectArchive(project)"
        >
          <svg v-if="isProjectArchived(project)" viewBox="0 0 24 24" aria-hidden="true">
            <path d="M7 9H4.5V5.5M4.8 9A7.5 7.5 0 1 1 6.7 17.4" />
            <path d="m4.8 9 3-3" />
          </svg>
          <svg v-else viewBox="0 0 24 24" aria-hidden="true">
            <path d="M4.5 8.5h15v10a1.5 1.5 0 0 1-1.5 1.5H6a1.5 1.5 0 0 1-1.5-1.5v-10Z" />
            <path d="M8 8.5V5h8v3.5M9 13h6" />
          </svg>
          <span class="sr-only">{{ isProjectArchived(project) ? t('threadList.unarchive') : t('threadList.archive') }}</span>
        </button>
        <details
          v-if="missingStartOptions(project).length"
          class="project-start-menu"
        >
          <summary class="agent-add-btn project-start-menu__toggle">
            <span aria-hidden="true">＋</span>
            {{ t('threadList.newThreadAction') }}
          </summary>
          <div class="project-start-menu__options">
            <button
              v-for="option in missingStartOptions(project)"
              :key="option.agentType"
              type="button"
              :disabled="!deviceOnline"
              :title="projectStartTitle(project, option)"
              :aria-label="projectStartTitle(project, option)"
              @click="onNewThread(project, option.agentType)"
            >{{ option.label }}</button>
          </div>
        </details>
      </div>

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
          <div class="agent-header-row">
            <button
              :id="agentHeadingId(agent.key)"
              type="button"
              class="agent-header"
              :aria-expanded="agent.sessions.length
                ? !prefs.isCollapsed(agentNodeKey(project.key, agent.type))
                : undefined"
              :aria-controls="agent.sessions.length ? agentPanelId(agent.key) : undefined"
              :disabled="agent.sessions.length === 0"
              @click="agent.sessions.length && prefs.toggleCollapse(agentNodeKey(project.key, agent.type))"
            >
              <img
                class="agent-avatar"
                :src="agent.avatar"
                alt=""
                width="42"
                height="42"
                @error="onAvatarError"
              />
              <span class="agent-copy">
                <span class="agent-title">{{ agent.label }}</span>
                <span
                  class="agent-subtitle"
                  :class="{ 'agent-subtitle--unavailable': agentStartUnavailable(project, agent) }"
                >{{ agentSubtitle(project, agent) }}</span>
              </span>
              <span v-if="agent.sessions.length" class="agent-chevron" aria-hidden="true">
                {{ prefs.isCollapsed(agentNodeKey(project.key, agent.type)) ? '▸' : '▾' }}
              </span>
            </button>
            <button
              v-if="agentStartOption(project, agent)"
              type="button"
              class="agent-add-btn"
              :disabled="!deviceOnline"
              :title="agentStartTitle(project, agent)"
              :aria-label="agentStartAria(project, agent)"
              @click.stop="onNewThread(project, agent.type)"
            >
              <span aria-hidden="true">＋</span>
              {{ t('threadList.newThreadAction') }}
            </button>
          </div>

          <div
            v-if="agent.sessions.length"
            v-show="!prefs.isCollapsed(agentNodeKey(project.key, agent.type))"
            :id="agentPanelId(agent.key)"
            class="agent-body"
          >
            <div
              v-for="session in agent.sessions"
              :key="session.id"
              class="session-item"
              :class="{
                archived: prefs.isArchived(session.id),
                draft: isLocalDraftSessionId(session.id)
              }"
            >
              <RouterLink
                class="session-main"
                :to="sessionDetailLocation(deviceId, session.id)"
                :aria-label="t('threadList.openThread', {
                  summary: shortSummary(session.summary),
                  detail: sessionActivityPresentation(session.status).detail
                })"
              >
                <span class="session-summary">
                  <span
                    v-if="isLocalDraftSessionId(session.id)"
                    class="draft-badge"
                  >{{ t('threadList.draftBadge') }}</span>
                  {{ shortSummary(session.summary) || (isLocalDraftSessionId(session.id) ? t('threadList.draftSummary') : '') }}
                </span>
                <span
                  class="session-status"
                  :class="[
                    `session-status--${sessionActivityPresentation(session.status).tone}`,
                    { 'session-status--attention': needsAttention(session.status) }
                  ]"
                  :title="sessionActivityPresentation(session.status).detail"
                  :aria-label="sessionActivityPresentation(session.status).label"
                >
                  <span v-if="needsAttention(session.status)">
                    {{ sessionActivityPresentation(session.status).label }}
                  </span>
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
                  <svg v-if="prefs.isArchived(session.id)" viewBox="0 0 24 24" aria-hidden="true">
                    <path d="M7 9H4.5V5.5M4.8 9A7.5 7.5 0 1 1 6.7 17.4" />
                    <path d="m4.8 9 3-3" />
                  </svg>
                  <svg v-else viewBox="0 0 24 24" aria-hidden="true">
                    <path d="M4.5 8.5h15v10a1.5 1.5 0 0 1-1.5 1.5H6a1.5 1.5 0 0 1-1.5-1.5v-10Z" />
                    <path d="M8 8.5V5h8v3.5M9 13h6" />
                  </svg>
                  <span class="sr-only">{{ prefs.isArchived(session.id) ? t('threadList.unarchive') : t('threadList.archive') }}</span>
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
import { RouterLink, useRouter } from 'vue-router'
import {
  KNOWN_AGENT_TYPES,
  UNKNOWN_AGENT_META
} from '@/config/agents'
import { sessionDetailLocation } from '@/router/navigation'
import { useSessionPrefsStore } from '@/stores/sessionPrefs'
import { isLocalDraftSessionId, useLocalThreadsStore } from '@/stores/localThreads'
import type { AgentSession, AgentStartCapability, AgentType } from '@/types/protocol'
import { agentLabel, sessionActivityPresentation, shortSummary } from '@/utils/agent'
import { projectStartOptions as startOptionsForProject } from '@/utils/startCapabilities'
import {
  buildSessionTree,
  projectKeyFromDir,
  type SessionTreeAgent,
  type SessionTreeProject
} from '@/utils/sessionTree'
import { sortSessionsByMode } from '@/utils/sessionSort'

const props = defineProps<{
  sessions: AgentSession[]
  deviceId: string
  deviceOnline?: boolean
  /** Null = legacy daemon without a device-level start catalog. */
  startCapabilities?: AgentStartCapability[] | null
}>()

const { t } = useI18n()
const router = useRouter()
const prefs = useSessionPrefsStore()
const localThreads = useLocalThreadsStore()
const searchQuery = ref('')
/** Empty string = all agents. */
const agentFilter = ref('')
/** Stable id for archive semantics describedby. */
const deviceOnline = computed(() => props.deviceOnline !== false)

const mergedSessions = computed(() => {
  const remote = props.sessions
  const local = localThreads.asSessions(props.deviceId)
  // Local drafts first within same activity, then remote (dedupe by id).
  const seen = new Set(remote.map(s => s.id))
  return [...local.filter(s => !seen.has(s.id)), ...remote]
})

const agentOptions = computed(() => {
  const counts = new Map<string, number>()
  for (const s of mergedSessions.value) {
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
  const visibleSessions = mergedSessions.value.filter(session => {
    if (!prefs.showArchived && prefs.isArchived(session.id)) return false
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
  const filtered = visibleSessions.filter(session => {
    return !agent || String(session.agent_type || '').trim() === agent
  })
  // Product rule: always recent activity first (no manual reorder).
  const projects = buildSessionTree(filtered, list => sortSessionsByMode(list, 'recent'))
  return projects
})

type ProjectStartOption = { agentType: AgentType; label: string; enabled: boolean; reason?: string }

function projectStartOptions(project: SessionTreeProject): ProjectStartOption[] {
  // A new native thread may only target a path already discovered by the daemon.
  const path = (project.path || '').trim()
  if (!path || project.uncategorized) return []
  if (!props.sessions.some(session => projectKeyFromDir(session.project_dir) === project.key)) {
    return []
  }

  return startOptionsForProject(project, props.startCapabilities)
    .map(option => ({ ...option, label: agentLabel(option.agentType) }))
}

function missingStartOptions(project: SessionTreeProject): ProjectStartOption[] {
  const existing = new Set(project.agents.map(agent => agent.type))
  return projectStartOptions(project).filter(option => option.enabled && !existing.has(option.agentType))
}

function projectStartTitle(project: SessionTreeProject, option: ProjectStartOption): string {
  return deviceOnline.value
    ? t('threadList.newThreadTitle', { agent: option.label, project: project.label })
    : t('threadList.newThreadUnavailable', {
        agent: option.label,
        reason: t('threadList.startOffline')
      })
}

function agentStartOption(
  project: SessionTreeProject,
  agent: SessionTreeAgent
): ProjectStartOption | null {
  if (!KNOWN_AGENT_TYPES.some(agentType => agentType === agent.type)) return null
  const advertised = projectStartOptions(project)
    .find(option => option.agentType === agent.type)
  return advertised?.enabled ? advertised : null
}

function agentStartReason(project: SessionTreeProject, agent: SessionTreeAgent): string {
  if (!deviceOnline.value) return t('threadList.startOffline')
  return agentStartOption(project, agent)?.reason || ''
}

function agentStartUnavailable(project: SessionTreeProject, agent: SessionTreeAgent): boolean {
  const option = agentStartOption(project, agent)
  return Boolean(option && !deviceOnline.value)
}

function agentSubtitle(project: SessionTreeProject, agent: SessionTreeAgent): string {
  return agentStartUnavailable(project, agent)
    ? agentStartReason(project, agent)
    : t('threadList.agentThreads', { n: agent.sessions.length })
}

function agentStartTitle(project: SessionTreeProject, agent: SessionTreeAgent): string {
  const option = agentStartOption(project, agent)
  return option?.enabled && deviceOnline.value
    ? t('threadList.newThreadTitle', { agent: agent.label, project: project.label })
    : t('threadList.newThreadUnavailable', {
        agent: agent.label,
        reason: agentStartReason(project, agent)
      })
}

function agentStartAria(project: SessionTreeProject, agent: SessionTreeAgent): string {
  const option = agentStartOption(project, agent)
  return option?.enabled && deviceOnline.value
    ? t('threadList.newThreadAria', { agent: agent.label, project: project.label })
    : agentStartTitle(project, agent)
}

function onNewThread(project: SessionTreeProject, agentType: AgentType) {
  const path = (project.path || '').trim()
  if (!path || !deviceOnline.value || !projectStartOptions(project).some(
    option => option.agentType === agentType && option.enabled
  )) return
  const draft = localThreads.createDraft(props.deviceId, agentType, path, project.label)
  void router.push(sessionDetailLocation(props.deviceId, draft.id))
}

function needsAttention(status: AgentSession['status']): boolean {
  return status === 'waiting_approval' || status === 'waiting_user' || status === 'error'
}

function projectSessionIds(project: SessionTreeProject): string[] {
  return mergedSessions.value
    .filter(session => projectKeyFromDir(session.project_dir) === project.key)
    .map(session => session.id)
}

function isProjectArchived(project: SessionTreeProject): boolean {
  const ids = projectSessionIds(project)
  return ids.length > 0 && ids.every(id => prefs.isArchived(id))
}

function toggleProjectArchive(project: SessionTreeProject) {
  const ids = projectSessionIds(project)
  prefs.setArchived(ids, !ids.every(id => prefs.isArchived(id)))
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

</script>

<style scoped>
.list-controls {
  display: grid;
  gap: 8px;
  margin-bottom: 14px;
}

.control-utility-row {
  display: grid;
  grid-template-columns: minmax(8.5rem, 1fr) auto;
  align-items: center;
  gap: 12px;
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
  border-radius: 13px;
  color: var(--neko-ink);
  background: var(--neko-surface-solid);
  font: inherit;
}

.control-input {
  padding-inline: 14px;
}

.control-select {
  color: var(--neko-ink-soft);
  background-color: var(--neko-surface-muted);
}

.control-select:focus-visible,
.control-input:focus-visible {
  outline: 2px solid var(--neko-primary);
  outline-offset: 2px;
}

.archive-toggle {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-height: 44px;
  color: var(--neko-ink-soft);
  font-size: 12px;
  font-weight: 600;
  user-select: none;
  cursor: pointer;
}

.archive-checkbox {
  width: 18px;
  height: 18px;
  flex: 0 0 18px;
  accent-color: var(--neko-primary);
}

@media (max-width: 340px) {
  .control-utility-row {
    grid-template-columns: 1fr;
    gap: 2px;
  }
}

.empty-hint {
  padding: 28px 20px;
  color: var(--neko-ink-soft);
  font-size: 13px;
  text-align: center;
}

.project-group {
  margin-bottom: 8px;
  overflow: hidden;
  border: 1px solid var(--neko-line);
  border-radius: 14px;
  background: var(--neko-surface-solid);
  box-shadow: none;
}

.project-group.uncategorized {
  border-style: dashed;
  background: var(--neko-surface-muted);
}

.project-header-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto auto;
  align-items: center;
  gap: 6px;
  padding-right: 8px;
  background: var(--neko-surface-solid);
}

.project-header {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto auto;
  width: 100%;
  align-items: center;
  gap: 10px;
  min-height: 54px;
  padding: 9px 12px;
  border: 0;
  background: transparent;
  color: inherit;
  cursor: pointer;
  font: inherit;
  text-align: left;
  transition: background-color 180ms ease, transform 160ms ease;
}

.project-archive-btn {
  flex: 0 0 auto;
}

.project-start-menu {
  position: relative;
}

.project-start-menu__toggle {
  list-style: none;
  cursor: pointer;
}

.project-start-menu__toggle::-webkit-details-marker {
  display: none;
}

.project-start-menu__options {
  position: absolute;
  z-index: 2;
  top: calc(100% + 4px);
  right: 0;
  display: grid;
  min-width: 128px;
  overflow: hidden;
  border: 1px solid var(--neko-line);
  border-radius: 10px;
  background: var(--neko-surface-solid);
  box-shadow: 0 8px 20px color-mix(in srgb, var(--neko-ink) 18%, transparent);
}

.project-start-menu__options button {
  min-height: 40px;
  padding: 8px 12px;
  border: 0;
  border-bottom: 1px solid var(--neko-line);
  color: var(--neko-ink);
  background: transparent;
  font: inherit;
  font-size: 12px;
  font-weight: 650;
  text-align: left;
  cursor: pointer;
}

.project-start-menu__options button:last-child {
  border-bottom: 0;
}

.project-start-menu__options button:disabled {
  color: var(--neko-muted);
  cursor: not-allowed;
  opacity: 0.62;
}

.project-start-menu__options button:hover,
.project-start-menu__options button:focus-visible {
  background: var(--neko-primary-soft);
}

.project-header:focus-visible,
.agent-header:focus-visible,
.agent-add-btn:focus-visible,
.session-main:focus-visible,
.archive-btn:focus-visible {
  outline: 2px solid var(--neko-primary);
  outline-offset: -2px;
}

.folder-icon {
  display: grid;
  width: 30px;
  height: 30px;
  flex: 0 0 30px;
  place-items: center;
  border-radius: 9px;
  color: var(--neko-primary-deep);
  background: var(--neko-primary-soft);
}

.folder-icon svg {
  width: 18px;
  height: 18px;
  fill: none;
  stroke: currentColor;
  stroke-linecap: round;
  stroke-linejoin: round;
  stroke-width: 1.7;
}

.project-copy,
.agent-copy {
  min-width: 0;
}

.project-copy {
  overflow: hidden;
}

.project-title {
  overflow: hidden;
  color: var(--neko-ink);
  font-size: 14px;
  font-weight: 680;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.project-count {
  min-width: 25px;
  padding: 3px 7px;
  border-radius: 6px;
  background: var(--neko-surface-muted);
  color: var(--neko-ink-soft);
  font-size: 10.5px;
  font-variant-numeric: tabular-nums;
  text-align: center;
}

.chevron {
  display: grid;
  width: 20px;
  height: 20px;
  place-items: center;
  color: var(--neko-ink-faint);
  transition: transform 180ms ease;
}

.project-header[aria-expanded='false'] .chevron {
  transform: rotate(-90deg);
}

.chevron svg {
  width: 18px;
  height: 18px;
  fill: none;
  stroke: currentColor;
  stroke-linecap: round;
  stroke-linejoin: round;
  stroke-width: 1.8;
}

.agent-chevron {
  color: var(--neko-ink-faint);
  font-size: 12px;
}

.project-body {
  padding: 3px 12px 8px;
  border-top: 1px solid var(--neko-line);
}

.agent-group + .agent-group {
  border-top: 1px solid var(--neko-line);
}

.agent-header-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: 8px;
  min-height: 58px;
  padding: 4px 0;
  border-radius: 13px;
  background:
    radial-gradient(circle at 8% 50%, var(--agent-soft-color), transparent 62%);
}

.agent-header {
  display: flex;
  width: 100%;
  min-width: 0;
  align-items: center;
  gap: 10px;
  min-height: 50px;
  padding: 4px 2px;
  border: 0;
  background: transparent;
  color: inherit;
  cursor: pointer;
  font: inherit;
  text-align: left;
}

.agent-header:disabled {
  cursor: default;
}

.session-item.draft {
  border-left: 3px solid var(--neko-primary);
}

.draft-badge {
  display: inline-block;
  margin-right: 6px;
  padding: 1px 6px;
  border-radius: 6px;
  color: var(--neko-primary-deep);
  background: var(--neko-primary-soft);
  font-size: 10px;
  font-weight: 700;
  vertical-align: middle;
}

.agent-avatar {
  display: block;
  width: 42px;
  height: 42px;
  flex: 0 0 42px;
  border: 2px solid color-mix(in srgb, var(--agent-color) 52%, var(--neko-surface-solid));
  border-radius: 14px 14px 17px 10px;
  background: var(--agent-soft-color);
  box-shadow: 0 5px 12px color-mix(in srgb, var(--agent-color) 20%, transparent);
  object-fit: cover;
}

.agent-copy {
  display: flex;
  min-width: 0;
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
  color: color-mix(in srgb, var(--agent-color) 58%, var(--neko-ink-soft));
  font-size: 10px;
  font-weight: 560;
  line-height: 1.35;
}

.agent-subtitle--unavailable {
  overflow: hidden;
  max-width: 11rem;
  color: var(--neko-danger-ink);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.agent-add-btn {
  display: inline-flex;
  min-width: 66px;
  min-height: 44px;
  align-items: center;
  justify-content: center;
  gap: 2px;
  padding: 0 10px;
  border: 1px solid color-mix(in srgb, var(--agent-color) 40%, var(--neko-line));
  border-radius: 12px 12px 15px 9px;
  color: color-mix(in srgb, var(--agent-color) 76%, var(--neko-ink));
  background: color-mix(in srgb, var(--agent-soft-color) 72%, var(--neko-surface-solid));
  box-shadow: 0 4px 10px color-mix(in srgb, var(--agent-color) 12%, transparent);
  font: inherit;
  font-size: 11px;
  font-weight: 700;
  white-space: nowrap;
  cursor: pointer;
  transition: transform 160ms ease, filter 180ms ease, opacity 180ms ease;
}

.agent-add-btn > span {
  font-size: 16px;
  font-weight: 500;
  line-height: 1;
}

.agent-add-btn:disabled {
  border-style: dashed;
  box-shadow: none;
  opacity: 0.48;
  cursor: not-allowed;
}

.agent-body {
  margin: 0 0 2px 10px;
  padding-left: 10px;
  border-left: 1px solid color-mix(in srgb, var(--agent-color) 48%, transparent);
}

.session-item {
  display: flex;
  align-items: stretch;
  gap: 4px;
  min-height: 44px;
  padding: 4px 0;
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
  width: 8px;
  height: 8px;
  flex: 0 0 8px;
  border-radius: 50%;
  background: var(--neko-neutral-soft);
}

.session-status--active {
  background: var(--neko-success-soft);
  box-shadow: inset 0 0 0 2px var(--neko-success-ink);
}

.session-status--waiting {
  background: var(--neko-warning);
}

.session-status--unknown {
  box-shadow: inset 0 0 0 2px var(--neko-neutral-ink);
}

.session-status--attention {
  width: auto;
  min-height: 22px;
  padding: 2px 6px;
  border-radius: 6px;
  color: var(--neko-warning-ink);
  background: var(--neko-warning-soft);
  font-size: 10px;
  font-weight: 700;
  line-height: 1.2;
  white-space: nowrap;
}

.session-status--attention.session-status--unknown {
  color: var(--neko-danger-ink);
  background: var(--neko-danger-soft);
  box-shadow: none;
}

.session-actions {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 4px;
}

.archive-btn {
  display: grid;
  width: 38px;
  min-width: 38px;
  min-height: 44px;
  padding: 0;
  place-items: center;
  border: 1px solid var(--neko-line);
  border-radius: 8px;
  background: var(--neko-primary-soft);
  color: var(--neko-primary-deep);
  cursor: pointer;
  transition: transform 160ms ease, background-color 160ms ease;
}

.archive-btn svg {
  width: 17px;
  height: 17px;
  fill: none;
  stroke: currentColor;
  stroke-linecap: round;
  stroke-linejoin: round;
  stroke-width: 1.7;
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

  .agent-add-btn:not(:disabled):hover {
    filter: saturate(1.08) brightness(0.98);
  }
}

.project-header:active,
.agent-header:active,
.agent-add-btn:not(:disabled):active,
.session-main:active,
.archive-btn:active {
  transform: scale(0.985);
}

@media (prefers-reduced-motion: reduce) {
  .project-header,
  .agent-add-btn,
  .archive-btn,
  .chevron {
    transition: none;
  }

  .project-header:active,
  .agent-header:active,
  .agent-add-btn:not(:disabled):active,
  .session-main:active,
  .archive-btn:active {
    transform: none;
  }
}
</style>
