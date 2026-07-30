import { collatorLocale } from '@/i18n'
import type { AgentSession } from '@/types/protocol'

export type SessionSortMode = 'recent' | 'name' | 'project' | 'manual'

/** Pure sort; manual mode uses provided order ranks. */
export function sortSessionsByMode(
  list: AgentSession[],
  mode: SessionSortMode,
  manualOrder: string[] = []
): AgentSession[] {
  const arr = [...list]
  if (mode === 'manual') {
    const rank = new Map(manualOrder.map((id, i) => [id, i]))
    arr.sort((a, b) => (rank.get(a.id) ?? 9999) - (rank.get(b.id) ?? 9999))
    return arr
  }
  if (mode === 'name') {
    arr.sort((a, b) => (a.summary || a.id).localeCompare(b.summary || b.id, collatorLocale()))
    return arr
  }
  if (mode === 'project') {
    arr.sort((a, b) => {
      const pa = a.project || a.project_dir || ''
      const pb = b.project || b.project_dir || ''
      const c = pa.localeCompare(pb, collatorLocale())
      if (c !== 0) return c
      return (b.last_activity || 0) - (a.last_activity || 0)
    })
    return arr
  }
  arr.sort((a, b) => (b.last_activity || 0) - (a.last_activity || 0))
  return arr
}

/** Append new ids only — never drop (group-local calls must not erase other groups). */
export function ensureManualOrder(order: string[], ids: string[]): string[] {
  const cur = [...order]
  const set = new Set(cur)
  for (const id of ids) {
    if (!set.has(id)) {
      cur.push(id)
      set.add(id)
    }
  }
  return cur
}

/** Drop ids no longer present in the full session universe. */
export function pruneManualOrder(order: string[], allIds: string[]): string[] {
  const idSet = new Set(allIds)
  return order.filter(id => idSet.has(id))
}
