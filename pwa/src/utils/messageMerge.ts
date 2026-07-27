import type { SessionMessage } from '@/types/protocol'

const MAX_MESSAGES = 500

/** Insert or patch message by id (streaming). Longer content wins. */
export function upsertMessageList(
  messages: SessionMessage[],
  msg: SessionMessage,
  mintId: () => string = () => `msg_${Date.now()}_${Math.random().toString(36).slice(2, 7)}`
): SessionMessage[] {
  let next = msg
  if (!next.id) {
    next = { ...next, id: mintId() }
  }
  const idx = messages.findIndex(m => m.id === next.id)
  if (idx >= 0) {
    const prev = messages[idx]
    const content =
      (next.content?.length || 0) >= (prev.content?.length || 0) ? next.content : prev.content
    const copy = [...messages]
    copy[idx] = { ...prev, ...next, content }
    return copy
  }
  const copy = [...messages, next]
  if (copy.length > MAX_MESSAGES) {
    return copy.slice(-MAX_MESSAGES)
  }
  return copy
}

/** Merge server history with local messages by stable message id only. */
export function mergeHistoryLists(
  current: SessionMessage[],
  hist: SessionMessage[]
): SessionMessage[] {
  if (!hist.length) return current
  const byId = new Map<string, SessionMessage>()
  for (const m of hist) {
    if (m?.id) byId.set(m.id, m)
  }
  for (const m of current) {
    if (!m?.id) continue
    const existing = byId.get(m.id)
    if (!existing) {
      byId.set(m.id, m)
    } else if ((m.content?.length || 0) >= (existing.content?.length || 0)) {
      byId.set(m.id, { ...existing, ...m })
    }
  }
  const merged = [...byId.values()].sort((a, b) => (a.timestamp || 0) - (b.timestamp || 0))
  return merged.slice(-MAX_MESSAGES)
}
