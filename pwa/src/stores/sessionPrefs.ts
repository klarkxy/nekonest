import { ref, watch } from 'vue'
import { defineStore } from 'pinia'
import type { AgentSession } from '@/types/protocol'
import {
  ensureManualOrder,
  pruneManualOrder,
  sortSessionsByMode,
  type SessionSortMode
} from '@/utils/sessionSort'

const ARCHIVED_KEY = 'nekonest_archived_sessions'
const COLLAPSED_KEY = 'nekonest_collapsed_nodes_v2'
const SHOW_ARCHIVED_KEY = 'nekonest_show_archived'
const SORT_KEY = 'nekonest_session_sort'
const ORDER_KEY = 'nekonest_session_order'

export type { SessionSortMode }

function loadSet(key: string): Set<string> {
  try {
    const raw = localStorage.getItem(key)
    if (!raw) return new Set()
    const arr = JSON.parse(raw) as string[]
    return new Set(Array.isArray(arr) ? arr : [])
  } catch {
    return new Set()
  }
}

function saveSet(key: string, set: Set<string>) {
  localStorage.setItem(key, JSON.stringify([...set]))
}

function loadOrder(): string[] {
  try {
    const raw = localStorage.getItem(ORDER_KEY)
    if (!raw) return []
    const arr = JSON.parse(raw) as string[]
    return Array.isArray(arr) ? arr : []
  } catch {
    return []
  }
}

function loadSortMode(): SessionSortMode {
  const stored = localStorage.getItem(SORT_KEY)
  return stored === 'name' || stored === 'manual' || stored === 'recent'
    ? stored
    : 'recent'
}

/** Local-only session prefs: archive, hierarchy collapse, sort, manual order. */
export const useSessionPrefsStore = defineStore('sessionPrefs', () => {
  const archived = ref<Set<string>>(loadSet(ARCHIVED_KEY))
  const collapsed = ref<Set<string>>(loadSet(COLLAPSED_KEY))
  const showArchived = ref(localStorage.getItem(SHOW_ARCHIVED_KEY) === '1')
  const sortMode = ref<SessionSortMode>(loadSortMode())
  const manualOrder = ref<string[]>(loadOrder())

  watch(showArchived, (v) => localStorage.setItem(SHOW_ARCHIVED_KEY, v ? '1' : '0'))
  watch(sortMode, (v) => localStorage.setItem(SORT_KEY, v))
  watch(manualOrder, (v) => localStorage.setItem(ORDER_KEY, JSON.stringify(v)), { deep: true })

  function isArchived(id: string) {
    return archived.value.has(id)
  }

  function archive(id: string) {
    const next = new Set(archived.value)
    next.add(id)
    archived.value = next
    saveSet(ARCHIVED_KEY, next)
  }

  function unarchive(id: string) {
    const next = new Set(archived.value)
    next.delete(id)
    archived.value = next
    saveSet(ARCHIVED_KEY, next)
  }

  function toggleArchive(id: string) {
    if (isArchived(id)) unarchive(id)
    else archive(id)
  }

  function isCollapsed(nodeKey: string) {
    return collapsed.value.has(nodeKey)
  }

  function toggleCollapse(nodeKey: string) {
    const next = new Set(collapsed.value)
    if (next.has(nodeKey)) next.delete(nodeKey)
    else next.add(nodeKey)
    collapsed.value = next
    saveSet(COLLAPSED_KEY, next)
  }

  function setCollapsed(nodeKey: string, value: boolean) {
    const next = new Set(collapsed.value)
    if (value) next.add(nodeKey)
    else next.delete(nodeKey)
    collapsed.value = next
    saveSet(COLLAPSED_KEY, next)
  }

  function setSortMode(mode: SessionSortMode) {
    sortMode.value = mode
  }

  /** Ensure ids exist in manual order (append only — safe for per-group calls). */
  function ensureOrder(ids: string[]) {
    const next = ensureManualOrder(manualOrder.value, ids)
    if (next.length !== manualOrder.value.length || next.some((id, i) => id !== manualOrder.value[i])) {
      manualOrder.value = next
    }
  }

  /** Prune against the full known session id list (call from ungrouped list only). */
  function pruneOrder(allIds: string[]) {
    const next = pruneManualOrder(manualOrder.value, allIds)
    if (next.length !== manualOrder.value.length || next.some((id, i) => id !== manualOrder.value[i])) {
      manualOrder.value = next
    }
  }

  function moveSession(id: string, dir: -1 | 1, amongIds: string[]) {
    // Only reorder within this agent group — do not drop other groups from global order.
    ensureOrder(amongIds)
    const groupOrder = manualOrder.value.filter(x => amongIds.includes(x))
    // Append any missing amongIds at end of group slice
    for (const x of amongIds) {
      if (!groupOrder.includes(x)) groupOrder.push(x)
    }
    const gIdx = groupOrder.indexOf(id)
    const tIdx = gIdx + dir
    if (gIdx < 0 || tIdx < 0 || tIdx >= groupOrder.length) return
    ;[groupOrder[gIdx], groupOrder[tIdx]] = [groupOrder[tIdx], groupOrder[gIdx]]
    // Rebuild full order: replace occurrences of amongIds with new group order
    const amongSet = new Set(amongIds)
    const next: string[] = []
    let inserted = false
    for (const x of manualOrder.value) {
      if (amongSet.has(x)) {
        if (!inserted) {
          next.push(...groupOrder)
          inserted = true
        }
        continue
      }
      next.push(x)
    }
    if (!inserted) next.push(...groupOrder)
    manualOrder.value = next
    sortMode.value = 'manual'
  }

  function sortSessions(list: AgentSession[]): AgentSession[] {
    // Locked product rule: newest activity first (manual/name modes retired).
    return sortSessionsByMode(list, 'recent')
  }

  return {
    archived, collapsed, showArchived, sortMode, manualOrder,
    isArchived, archive, unarchive, toggleArchive,
    isCollapsed, toggleCollapse, setCollapsed,
    setSortMode, sortSessions, moveSession, ensureOrder, pruneOrder
  }
})
