import type { AgentSession, AgentStatus, AgentType } from '@/types/protocol'

export type AgentGroup = {
  type: AgentType
  label: string
  icon: string
  color: string
  sessions: AgentSession[]
}

const ORDER: AgentType[] = ['kilo', 'claude_code', 'codex']

export function agentIcon(type: AgentType | string): string {
  switch (type) {
    case 'claude_code': return '🟣'
    case 'kilo': return '🔴'
    case 'codex': return '🟢'
    default: return '🐱'
  }
}

export function agentLabel(type: AgentType | string): string {
  switch (type) {
    case 'claude_code': return 'Claude Code'
    case 'kilo': return 'Kilo'
    case 'codex': return 'Codex'
    default: return String(type)
  }
}

export function agentColor(type: AgentType | string): string {
  switch (type) {
    case 'claude_code': return '#8B7EC8'
    case 'kilo': return '#E07070'
    case 'codex': return '#5BBF8A'
    default: return '#B8A9E8'
  }
}

export function statusLabel(status: AgentStatus | string): string {
  return ({ running: '运行中', idle: '空闲', waiting_approval: '等待审批' } as Record<string, string>)[status] || status
}

export function statusTagType(status: AgentStatus | string): 'success' | 'default' | 'warning' {
  return ({ running: 'success', idle: 'default', waiting_approval: 'warning' } as Record<string, 'success' | 'default' | 'warning'>)[status] || 'default'
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
  const groups: AgentGroup[] = []
  for (const t of ORDER) {
    const list = map.get(t)
    if (list?.length) {
      groups.push({
        type: t,
        label: agentLabel(t),
        icon: agentIcon(t),
        color: agentColor(t),
        sessions: list
      })
      map.delete(t)
    }
  }
  for (const [t, list] of map) {
    groups.push({
      type: t as AgentType,
      label: agentLabel(t),
      icon: agentIcon(t),
      color: agentColor(t),
      sessions: list
    })
  }
  return groups
}

export function shortSummary(text: string | undefined, max = 48): string {
  if (!text) return '空闲会话'
  const t = text.replace(/\s+/g, ' ').trim()
  if (t.length <= max) return t
  return t.slice(0, max) + '…'
}

/** Display project name + optional shortened path. */
export function projectDisplay(s: AgentSession): { name: string; path: string } | null {
  const path = (s.project_dir || '').trim()
  const name = (s.project || '').trim() || leafName(path)
  if (!name && !path) return null
  return { name: name || '项目', path }
}

function leafName(p: string): string {
  if (!p) return ''
  const n = p.replace(/\\/g, '/').replace(/\/+$/, '')
  const i = n.lastIndexOf('/')
  return i >= 0 ? n.slice(i + 1) : n
}
