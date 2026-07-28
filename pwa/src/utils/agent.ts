import type { AgentSession, AgentStatus, AgentType } from '@/types/protocol'
import { agentOrder, getAgentMeta } from '@/config/agents'

export type AgentGroup = {
  type: AgentType
  label: string
  icon: string
  color: string
  sessions: AgentSession[]
}

export function agentIcon(type: AgentType | string): string {
  return getAgentMeta(type).symbol
}

export function agentLabel(type: AgentType | string): string {
  return getAgentMeta(type).label
}

export function agentColor(type: AgentType | string): string {
  return getAgentMeta(type).color
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
      return order || a.label.localeCompare(b.label, 'zh-CN')
    })
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
