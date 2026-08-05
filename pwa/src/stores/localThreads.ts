import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import type { AgentSession, AgentType } from '@/types/protocol'

const STORAGE_KEY = 'nekonest_local_threads_v1'
const ID_PREFIX = 'local_draft_'

export type LocalThread = {
  id: string
  deviceId: string
  agentType: AgentType
  projectDir: string
  project: string
  summary: string
  createdAt: number
  lastActivity: number
  /** Persisted before start_thread leaves the phone; presence is fail-closed on reload. */
  startOperationId?: string
}

function loadAll(): LocalThread[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return []
    const arr = JSON.parse(raw) as LocalThread[]
    return Array.isArray(arr) ? arr : []
  } catch {
    return []
  }
}

function saveAll(list: LocalThread[]) {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(list.slice(0, 40)))
}

export function isLocalDraftSessionId(id: string): boolean {
  return id.startsWith(ID_PREFIX)
}

function folderLabel(path: string): string {
  const parts = path.replace(/\\/g, '/').split('/').filter(Boolean)
  return parts[parts.length - 1] || path
}

export const useLocalThreadsStore = defineStore('localThreads', () => {
  const threads = ref<LocalThread[]>(loadAll())

  function persist() {
    saveAll(threads.value)
  }

  function listForDevice(deviceId: string): LocalThread[] {
    return threads.value.filter(t => t.deviceId === deviceId)
  }

  function get(id: string): LocalThread | undefined {
    return threads.value.find(t => t.id === id)
  }

  /** Create a phone-only draft under a daemon-discovered project directory. */
  function createDraft(
    deviceId: string,
    agentType: AgentType,
    projectDir: string,
    project?: string
  ): LocalThread {
    const now = Math.floor(Date.now() / 1000)
    const t: LocalThread = {
      id: `${ID_PREFIX}${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`,
      deviceId,
      agentType,
      projectDir,
      project: (project || folderLabel(projectDir)).trim(),
      summary: '',
      createdAt: now,
      lastActivity: now
    }
    threads.value = [t, ...threads.value.filter(x => x.id !== t.id)]
    persist()
    return t
  }

  /** @deprecated use createDraft with an explicit agent type. */
  function createCodexDraft(deviceId: string, projectDir: string, project?: string): LocalThread {
    return createDraft(deviceId, 'codex', projectDir, project)
  }

  function touch(id: string, summary?: string) {
    const idx = threads.value.findIndex(t => t.id === id)
    if (idx < 0) return
    const next = { ...threads.value[idx] }
    next.lastActivity = Math.floor(Date.now() / 1000)
    if (summary != null) next.summary = summary
    const copy = [...threads.value]
    copy[idx] = next
    threads.value = copy
    persist()
  }

  function bindStartOperation(id: string, operationId: string): boolean {
    const idx = threads.value.findIndex(t => t.id === id)
    if (idx < 0 || !operationId.trim()) return false
    const next = [...threads.value]
    next[idx] = { ...next[idx], startOperationId: operationId.trim(), lastActivity: Math.floor(Date.now() / 1000) }
    try {
      saveAll(next)
      threads.value = next
      return true
    } catch {
      return false
    }
  }

  function clearStartOperation(id: string) {
    const idx = threads.value.findIndex(t => t.id === id)
    if (idx < 0) return
    const next = [...threads.value]
    const { startOperationId: _, ...thread } = next[idx]
    next[idx] = thread
    try {
      saveAll(next)
      threads.value = next
    } catch {
      // Keeping the durable binding is fail-closed; reload still forbids retry.
    }
  }

  function remove(id: string) {
    threads.value = threads.value.filter(t => t.id !== id)
    persist()
  }

  /** Map local drafts into AgentSession shape for the tree. */
  function asSessions(deviceId: string): AgentSession[] {
    return listForDevice(deviceId).map(t => ({
      id: t.id,
      device_id: t.deviceId,
      agent_type: t.agentType,
      status: 'idle' as const,
      summary: t.summary || '',
      last_activity: t.lastActivity,
      project_dir: t.projectDir,
      project: t.project,
      capabilities: {
        control_mode: 'compatibility' as const,
        spawn: false,
        interrupt: false,
        approve: false,
        deny: false,
        attachment_mode: 'unsupported' as const
      }
    }))
  }

  const count = computed(() => threads.value.length)

  return {
    threads,
    count,
    listForDevice,
    get,
    createDraft,
    createCodexDraft,
    touch,
    bindStartOperation,
    clearStartOperation,
    remove,
    asSessions
  }
})
