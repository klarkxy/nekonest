import type { AgentSession, AgentStatus, AgentType } from '@/types/protocol'
import { agentOrder, getAgentMeta } from '@/config/agents'

export type AgentGroup = {
  type: AgentType
  label: string
  icon: string
  color: string
  sessions: AgentSession[]
}

export type SessionActivityTone = 'active' | 'idle' | 'waiting' | 'unknown'

export type SessionActivityPresentation = {
  icon: string
  label: string
  headline: string
  detail: string
  tone: SessionActivityTone
}

const SESSION_ACTIVITY: Record<AgentStatus, SessionActivityPresentation> = {
  running: {
    icon: '🐾',
    label: '忙碌中',
    headline: '线团还在转',
    detail: '家里的猫娘还在干活，新消息会同步过来。',
    tone: 'active'
  },
  idle: {
    icon: '🌙',
    label: '待命',
    headline: '线团空闲',
    detail: '随时可以继续说一句。',
    tone: 'idle'
  },
  waiting_approval: {
    icon: '🔔',
    label: '电脑待批',
    headline: '等电脑点头',
    detail: '请到家里电脑的终端批准或拒绝；手机不能代点。',
    tone: 'waiting'
  }
}

const STREAMING_ACTIVITY: SessionActivityPresentation = {
  icon: '🐾',
  label: '回复中',
  headline: '正在回复',
  detail: '新内容会陆续出现在这里。',
  tone: 'active'
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

export function sessionActivityPresentation(
  status: AgentStatus | string,
  streaming = false
): SessionActivityPresentation {
  if (status === 'waiting_approval') return SESSION_ACTIVITY.waiting_approval
  if (streaming) return STREAMING_ACTIVITY
  return SESSION_ACTIVITY[status as AgentStatus] || {
    icon: '✦',
    label: status || '未知',
    headline: '状态还不清楚',
    detail: '等下一次同步。',
    tone: 'unknown'
  }
}

export function statusLabel(status: AgentStatus | string): string {
  return sessionActivityPresentation(status).label
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
  if (!text) return '未命名线团'
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
