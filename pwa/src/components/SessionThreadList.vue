<template>
  <div class="thread-list">
    <div class="toolbar">
      <label class="archive-toggle">
        <input type="checkbox" v-model="prefs.showArchived" />
        显示已归档
      </label>
      <div class="sort-row">
        <span class="sort-label">排序</span>
        <select class="sort-select" :value="prefs.sortMode" @change="onSortChange">
          <option value="recent">最近活跃</option>
          <option value="name">标题</option>
          <option value="project">项目/目录</option>
          <option value="manual">手动</option>
        </select>
      </div>
    </div>
    <p class="hint">右侧「归档」仅藏在手机里，不会动 PC 上的 Agent。手动排序用 ▲▼。</p>

    <div v-if="visibleGroups.length === 0" class="empty-hint">
      {{ prefs.showArchived ? '没有会话' : '暂无活跃会话（或都已归档）' }}
    </div>

    <div v-for="group in visibleGroups" :key="group.type" class="agent-group">
      <button
        type="button"
        class="group-header"
        :style="{ borderColor: group.color }"
        @click="prefs.toggleCollapse(group.type)"
      >
        <span class="chevron">{{ prefs.isCollapsed(group.type) ? '▸' : '▾' }}</span>
        <span class="group-icon">{{ group.icon }}</span>
        <span class="group-title">{{ group.label }}</span>
        <span class="group-count">{{ group.sessions.length }}</span>
      </button>

      <div v-show="!prefs.isCollapsed(group.type)" class="group-body">
        <div
          v-for="(session, idx) in group.sessions"
          :key="session.id"
          class="session-item neko-card"
          :class="{ archived: prefs.isArchived(session.id) }"
        >
          <button
            type="button"
            class="session-main"
            :aria-label="`打开会话：${shortSummary(session.summary)}`"
            @click="$emit('open', session.id)"
          >
            <div class="session-top">
              <span class="session-summary">{{ shortSummary(session.summary) }}</span>
              <n-tag :type="statusTagType(session.status)" size="small" round>
                {{ statusLabel(session.status) }}
              </n-tag>
            </div>
            <div v-if="projectDisplay(session)" class="session-project" :title="projectDisplay(session)!.path">
              📁 {{ projectDisplay(session)!.name }}
              <span v-if="projectDisplay(session)!.path" class="proj-path">
                · {{ shortPath(projectDisplay(session)!.path) }}
              </span>
            </div>
          </button>
          <div class="session-actions">
            <button
              v-if="prefs.sortMode === 'manual'"
              type="button"
              class="icon-btn"
              title="上移"
              aria-label="上移会话"
              :disabled="idx === 0"
              @click.stop="prefs.moveSession(session.id, -1, group.sessions.map(s => s.id))"
            >▲</button>
            <button
              v-if="prefs.sortMode === 'manual'"
              type="button"
              class="icon-btn"
              title="下移"
              aria-label="下移会话"
              :disabled="idx === group.sessions.length - 1"
              @click.stop="prefs.moveSession(session.id, 1, group.sessions.map(s => s.id))"
            >▼</button>
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
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { NTag } from 'naive-ui'
import { useSessionPrefsStore, type SessionSortMode } from '@/stores/sessionPrefs'
import type { AgentSession } from '@/types/protocol'
import {
  groupSessionsByAgent,
  shortSummary,
  statusLabel,
  statusTagType,
  projectDisplay
} from '@/utils/agent'

const props = defineProps<{
  sessions: AgentSession[]
}>()

defineEmits<{
  open: [id: string]
}>()

const prefs = useSessionPrefsStore()

const visibleGroups = computed(() => {
  const filtered = props.sessions.filter(s => prefs.showArchived || !prefs.isArchived(s.id))
  return groupSessionsByAgent(filtered, list => prefs.sortSessions(list))
})

function onSortChange(e: Event) {
  const v = (e.target as HTMLSelectElement).value as SessionSortMode
  prefs.setSortMode(v)
}

function shortPath(p: string, max = 36): string {
  const n = p.replace(/\\/g, '/')
  if (n.length <= max) return n
  return '…' + n.slice(-(max - 1))
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
.archive-toggle {
  font-size: 12px;
  color: #8A8A8A;
  display: flex;
  align-items: center;
  gap: 6px;
  user-select: none;
}
.sort-row {
  display: flex;
  align-items: center;
  gap: 6px;
}
.sort-label { font-size: 12px; color: #9E9E9E; }
.sort-select {
  font-size: 13px;
  border: 1px solid #E0DCE8;
  border-radius: 8px;
  padding: 4px 8px;
  background: #fff;
  color: #4A4A4A;
}
.hint {
  font-size: 11px;
  color: #B0A8B8;
  margin: 0 0 12px;
  line-height: 1.4;
}
.empty-hint {
  text-align: center;
  color: #9E9E9E;
  font-size: 13px;
  padding: 24px;
}

.agent-group { margin-bottom: 14px; }
.group-header {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
  padding: 6px 8px;
  border: none;
  border-left: 3px solid #B8A9E8;
  background: transparent;
  cursor: pointer;
  text-align: left;
  border-radius: 0 8px 8px 0;
}
.group-header:active { background: rgba(184,169,232,0.12); }
.chevron { width: 14px; color: #9E9E9E; font-size: 12px; }
.group-icon { font-size: 16px; }
.group-title { font-size: 14px; font-weight: 600; color: #4A4A4A; flex: 1; }
.group-count {
  font-size: 12px;
  color: #9E9E9E;
  background: #F0ECF8;
  padding: 1px 8px;
  border-radius: 999px;
}

.session-item {
  cursor: default;
  padding: 10px 12px;
  display: flex;
  align-items: stretch;
  gap: 8px;
}
.session-item.archived { opacity: 0.55; }
.session-main {
  flex: 1;
  min-width: 0;
  cursor: pointer;
  display: flex;
  flex-direction: column;
  gap: 4px;
  border: none;
  background: transparent;
  padding: 0;
  text-align: left;
  font: inherit;
  color: inherit;
}
.session-main:focus-visible {
  outline: 2px solid #B8A9E8;
  outline-offset: 2px;
  border-radius: 8px;
}
.session-top {
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
.session-project {
  font-size: 11px;
  color: #8A7AA8;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.proj-path { color: #B0A8C0; }

.session-actions {
  display: flex;
  flex-direction: column;
  align-items: stretch;
  justify-content: center;
  gap: 4px;
  flex-shrink: 0;
}
.icon-btn {
  border: 1px solid #E8E4F0;
  background: #FAF8FF;
  border-radius: 6px;
  width: 28px;
  height: 22px;
  font-size: 10px;
  cursor: pointer;
  color: #6A6A6A;
  align-self: flex-end;
}
.icon-btn:disabled { opacity: 0.35; cursor: default; }
.archive-btn {
  border: 1px solid #E0D4F0;
  background: #F5F0FC;
  border-radius: 8px;
  padding: 6px 10px;
  font-size: 12px;
  font-weight: 600;
  color: #6B5BB8;
  cursor: pointer;
  white-space: nowrap;
}
.archive-btn.on {
  background: #EEE;
  border-color: #DDD;
  color: #888;
}
.archive-btn:active { transform: scale(0.97); }
</style>
