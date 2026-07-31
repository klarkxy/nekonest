import type { AgentSession, AgentStatus, AgentType } from '@/types/protocol'
import { agentOrder, getAgentMeta } from '@/config/agents'
import { collatorLocale, tGlobal } from '@/i18n'

export type AgentGroup = {
  type: AgentType
  label: string
  icon: string
  color: string
  sessions: AgentSession[]
}

export type SessionActivityTone = 'active' | 'idle' | 'waiting' | 'error' | 'unknown'

export type SessionActivityPresentation = {
  icon: string
  label: string
  headline: string
  detail: string
  tone: SessionActivityTone
}

const UUID_RE =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i

function activityFromKeys(
  prefix:
    | 'status.running'
    | 'status.idle'
    | 'status.waiting_approval'
    | 'status.waiting_user'
    | 'status.error'
    | 'status.streaming'
    | 'status.unknown',
  tone: SessionActivityTone
): SessionActivityPresentation {
  return {
    icon: tGlobal(`${prefix}.icon`),
    label: tGlobal(`${prefix}.label`),
    headline: tGlobal(`${prefix}.headline`),
    detail: tGlobal(`${prefix}.detail`),
    tone
  }
}

export function agentIcon(type: AgentType | string): string {
  return getAgentMeta(type).symbol
}

export function agentLabel(type: AgentType | string): string {
  const normalized = String(type || '').trim()
  if (!normalized || normalized === 'unknown') return tGlobal('agent.unknown')
  return getAgentMeta(type).label
}

export function agentColor(type: AgentType | string): string {
  return getAgentMeta(type).color
}

export function sessionActivityPresentation(
  status: AgentStatus | string,
  streaming = false
): SessionActivityPresentation {
  if (status === 'waiting_approval') {
    return activityFromKeys('status.waiting_approval', 'waiting')
  }
  if (status === 'waiting_user') {
    return activityFromKeys('status.waiting_user', 'waiting')
  }
  if (status === 'error') {
    return activityFromKeys('status.error', 'error')
  }
  if (streaming) return activityFromKeys('status.streaming', 'active')
  if (status === 'running') return activityFromKeys('status.running', 'active')
  if (status === 'idle') return activityFromKeys('status.idle', 'idle')
  const unknown = activityFromKeys('status.unknown', 'unknown')
  return {
    ...unknown,
    label: status || unknown.label
  }
}

export function statusLabel(status: AgentStatus | string): string {
  return sessionActivityPresentation(status).label
}

export function statusTagType(status: AgentStatus | string): 'success' | 'default' | 'warning' | 'error' {
  return (
    {
      running: 'success',
      idle: 'default',
      waiting_approval: 'warning',
      waiting_user: 'warning',
      error: 'error'
    } as Record<string, 'success' | 'default' | 'warning' | 'error'>
  )[status] || 'default'
}

/** Group sessions by agent_type; known agents first, then others. */
export function groupSessionsByAgent(
  sessions: AgentSession[],
  sortFn?: (list: AgentSession[]) => AgentSession[]
): AgentGroup[] {
  const map = new Map<string, AgentSession[]>()
  for (const s of sessions) {
    const key = s.agent_type || 'unknown'
    if (!map.has(key)) map.set(key, [])
    map.get(key)!.push(s)
  }
  for (const [k, list] of map) {
    map.set(k, sortFn ? sortFn(list) : [...list].sort((a, b) => (b.last_activity || 0) - (a.last_activity || 0)))
  }
  return [...map.entries()]
    .map<AgentGroup>(([type, list]) => ({
      type,
      label: agentLabel(type),
      icon: agentIcon(type),
      color: agentColor(type),
      sessions: list
    }))
    .sort((a, b) => {
      const order = agentOrder(a.type) - agentOrder(b.type)
      return order || a.label.localeCompare(b.label, collatorLocale())
    })
}

function looksLikeOpaqueId(text: string): boolean {
  const t = text.trim()
  if (!t) return true
  if (UUID_RE.test(t)) return true
  // long hex / bare session ids without spaces
  if (/^[0-9a-f]{16,}$/i.test(t)) return true
  return false
}

export function shortSummary(text: string | undefined, max = 48): string {
  if (!text || looksLikeOpaqueId(text)) return tGlobal('agent.untitledThread')
  const t = text.replace(/\s+/g, ' ').trim()
  if (looksLikeOpaqueId(t)) return tGlobal('agent.untitledThread')
  if (t.length <= max) return t
  return t.slice(0, max) + '…'
}

/** Display project name + optional shortened path. */
export function projectDisplay(s: AgentSession): { name: string; path: string } | null {
  const path = (s.project_dir || '').trim()
  const name = (s.project || '').trim() || leafName(path)
  if (!name && !path) return null
  return { name: name || tGlobal('common.project'), path }
}

/** Basename for cramped headers. */
export function projectBaseName(pathOrName: string): string {
  const n = pathOrName.replace(/\\/g, '/').replace(/\/+$/, '')
  const i = n.lastIndexOf('/')
  return i >= 0 ? n.slice(i + 1) : n
}

function leafName(p: string): string {
  if (!p) return ''
  const n = p.replace(/\\/g, '/').replace(/\/+$/, '')
  const i = n.lastIndexOf('/')
  return i >= 0 ? n.slice(i + 1) : n
}
