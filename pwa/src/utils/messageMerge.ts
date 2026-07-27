import type { SessionMessage } from '@/types/protocol'

const MAX_MESSAGES = 500
const IMPORTED_DEDUPE_WINDOW_SECONDS = 15

type DuplicateCandidate = {
  canonicalIndex: number
  importedIndex: number
  distance: number
}

function agentType(msg: SessionMessage): string | undefined {
  const value = msg.metadata?.agent_type
  return typeof value === 'string' && value ? value : undefined
}

function canMapImportedToCanonical(
  imported: SessionMessage,
  canonical: SessionMessage
): boolean {
  if (imported.metadata?.imported !== true || canonical.metadata?.imported === true) {
    return false
  }
  if (imported.role !== canonical.role || imported.content !== canonical.content) {
    return false
  }
  if (!Number.isFinite(imported.timestamp) || !Number.isFinite(canonical.timestamp)) {
    return false
  }
  if (Math.abs(imported.timestamp - canonical.timestamp) > IMPORTED_DEDUPE_WINDOW_SECONDS) {
    return false
  }
  const importedAgent = agentType(imported)
  const canonicalAgent = agentType(canonical)
  return !importedAgent || !canonicalAgent || importedAgent === canonicalAgent
}

/**
 * Remove agent-native history copies after stable-id merging.
 *
 * Matching is deliberately source-aware and one-to-one. Content alone is not
 * enough: two consecutive prompts such as "继续" must remain two prompts.
 */
function dedupeImportedCopies(messages: SessionMessage[]): SessionMessage[] {
  const candidates: DuplicateCandidate[] = []
  for (let importedIndex = 0; importedIndex < messages.length; importedIndex++) {
    const imported = messages[importedIndex]
    if (imported.metadata?.imported !== true) continue
    for (let canonicalIndex = 0; canonicalIndex < messages.length; canonicalIndex++) {
      const canonical = messages[canonicalIndex]
      if (!canMapImportedToCanonical(imported, canonical)) continue
      candidates.push({
        canonicalIndex,
        importedIndex,
        distance: Math.abs(imported.timestamp - canonical.timestamp)
      })
    }
  }

  // Claim the closest pair first. Stable index tie-breakers keep the result
  // deterministic when several same-content turns share a timestamp.
  candidates.sort(
    (a, b) =>
      a.distance - b.distance ||
      a.canonicalIndex - b.canonicalIndex ||
      a.importedIndex - b.importedIndex
  )

  const matchedCanonical = new Set<number>()
  const matchedImported = new Set<number>()
  for (const candidate of candidates) {
    if (
      matchedCanonical.has(candidate.canonicalIndex) ||
      matchedImported.has(candidate.importedIndex)
    ) {
      continue
    }
    matchedCanonical.add(candidate.canonicalIndex)
    matchedImported.add(candidate.importedIndex)
  }
  if (!matchedImported.size) return messages
  return messages.filter((_, index) => !matchedImported.has(index))
}

function capMessages(messages: SessionMessage[]): SessionMessage[] {
  return messages.length > MAX_MESSAGES ? messages.slice(-MAX_MESSAGES) : messages
}

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
    return capMessages(dedupeImportedCopies(copy))
  }
  return capMessages(dedupeImportedCopies([...messages, next]))
}

/** Merge by stable id first, then remove source-aware one-to-one imported copies. */
export function mergeHistoryLists(
  current: SessionMessage[],
  hist: SessionMessage[]
): SessionMessage[] {
  if (!hist.length) return capMessages(dedupeImportedCopies(current))
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
  return capMessages(dedupeImportedCopies(merged))
}
