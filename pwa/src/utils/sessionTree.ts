import { agentOrder, getAgentMeta } from '@/config/agents'
import { collatorLocale, tGlobal } from '@/i18n'
import type { AgentSession, AgentType } from '@/types/protocol'

export const UNCATEGORIZED_PROJECT_KEY = '__uncategorized__'

export function uncategorizedProjectLabel(): string {
  return tGlobal('agent.uncategorized')
}

/** @deprecated use uncategorizedProjectLabel() for locale-aware label */
export const UNCATEGORIZED_PROJECT_LABEL = '未分类'

export type SessionTreeAgent = {
  key: string
  type: AgentType
  label: string
  avatar: string
  color: string
  softColor: string
  lastActivity: number
  sessions: AgentSession[]
}

export type SessionTreeProject = {
  key: string
  label: string
  path: string
  uncategorized: boolean
  lastActivity: number
  sessionCount: number
  agents: SessionTreeAgent[]
}

type MutableAgent = {
  type: AgentType
  sessions: AgentSession[]
}

type MutableProject = {
  key: string
  label: string
  path: string
  uncategorized: boolean
  agents: Map<string, MutableAgent>
}

/** Normalize a Windows workspace path without losing its UNC prefix. */
export function normalizeProjectDir(value: string | undefined): string {
  const raw = (value || '').trim()
  if (!raw) return ''

  const hadUncPrefix = raw.replace(/\\/g, '/').startsWith('//')
  let normalized = raw.replace(/\\/g, '/').replace(/\/+/g, '/')
  if (hadUncPrefix && !normalized.startsWith('//')) {
    normalized = `/${normalized}`
  }
  if (/^[a-zA-Z]:$/.test(normalized)) {
    normalized += '/'
  }
  if (normalized !== '/' && !/^[a-zA-Z]:\/$/.test(normalized)) {
    normalized = normalized.replace(/\/+$/, '')
  }
  return normalized
}

export function projectKeyFromDir(projectDir: string | undefined): string {
  const normalized = normalizeProjectDir(projectDir)
  return normalized
    ? `path:${normalized.toLocaleLowerCase('en-US')}`
    : UNCATEGORIZED_PROJECT_KEY
}

export function buildSessionTree(
  sessions: AgentSession[],
  sortSessions?: (sessions: AgentSession[]) => AgentSession[]
): SessionTreeProject[] {
  const projects = new Map<string, MutableProject>()

  for (const session of sessions) {
    const path = normalizeProjectDir(session.project_dir)
    const key = projectKeyFromDir(path)
    const uncategorized = key === UNCATEGORIZED_PROJECT_KEY
    let project = projects.get(key)
    if (!project) {
      project = {
        key,
        label: uncategorized ? uncategorizedProjectLabel() : projectLabel(path),
        path,
        uncategorized,
        agents: new Map()
      }
      projects.set(key, project)
    }

    const type = (String(session.agent_type || '').trim() || 'unknown') as AgentType
    let agent = project.agents.get(type)
    if (!agent) {
      agent = { type, sessions: [] }
      project.agents.set(type, agent)
    }
    agent.sessions.push(session)
  }

  const tree = [...projects.values()].map<SessionTreeProject>((project) => {
    const agents = [...project.agents.values()]
      .map<SessionTreeAgent>((agent) => {
        const meta = getAgentMeta(agent.type)
        const sorted = sortSessions
          ? sortSessions([...agent.sessions])
          : [...agent.sessions].sort(
              (a, b) => (b.last_activity || 0) - (a.last_activity || 0)
            )
        return {
          key: `${project.key}::${agent.type}`,
          type: agent.type,
          label: meta.label,
          avatar: meta.avatar,
          color: meta.color,
          softColor: meta.softColor,
          lastActivity: maxActivity(sorted),
          sessions: sorted
        }
      })
      .sort((a, b) => {
        const order = agentOrder(a.type) - agentOrder(b.type)
        return order || a.label.localeCompare(b.label, collatorLocale())
      })

    const allSessions = agents.flatMap(agent => agent.sessions)
    return {
      key: project.key,
      label: project.label,
      path: project.path,
      uncategorized: project.uncategorized,
      lastActivity: maxActivity(allSessions),
      sessionCount: allSessions.length,
      agents
    }
  })

  return tree.sort((a, b) => {
    const activity = b.lastActivity - a.lastActivity
    if (activity !== 0) return activity
    if (a.uncategorized !== b.uncategorized) return a.uncategorized ? 1 : -1
    const label = a.label.localeCompare(b.label, collatorLocale())
    return label || a.path.localeCompare(b.path, collatorLocale())
  })
}

function projectLabel(path: string): string {
  if (/^[a-zA-Z]:\/$/.test(path)) return path
  const parts = path.split('/').filter(Boolean)
  return parts[parts.length - 1] || path
}

function maxActivity(sessions: AgentSession[]): number {
  return sessions.reduce((latest, session) => {
    return Math.max(latest, session.last_activity || 0)
  }, 0)
}
