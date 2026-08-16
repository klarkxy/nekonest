import { KNOWN_AGENT_TYPES, isInstallGatedAgent } from '@/config/agents'
import type { AgentSession, AgentStartCapability, AgentType } from '@/types/protocol'
import type { SessionTreeProject } from '@/utils/sessionTree'
import { projectKeyFromDir, UNCATEGORIZED_PROJECT_KEY } from '@/utils/sessionTree'

/**
 * Creation is restricted to daemon-discovered project paths. A device catalog
 * is authoritative when present; legacy peers may expose only a positive
 * per-session Codex spawn flag.
 */
export type StartAgentOption = {
  agentType: AgentType
  enabled: boolean
  reason?: string
}

export function canStartInCurrentProject(
  agentType: AgentType,
  projectDir: string,
  sessions: AgentSession[],
  catalog: AgentStartCapability[] | null | undefined
): boolean {
  const key = projectKeyFromDir(projectDir)
  if (key === UNCATEGORIZED_PROJECT_KEY) return false
  const current = sessions.filter(session => projectKeyFromDir(session.project_dir) === key)
  if (current.length === 0) return false
  if (catalog !== null && catalog !== undefined) {
    const capability = catalog.find(entry => entry.agent_type === agentType)
    return Boolean(capability?.available && capability?.spawn)
  }
  return agentType === 'codex' && current.some(session =>
    session.agent_type === 'codex' && session.capabilities?.spawn === true
  )
}

export type StartOperationBindResult = 'bound' | 'unavailable' | 'persist_failed'

/**
 * Revalidate immediately before durably binding an operation ID. Keeping the
 * check and bind in one helper prevents a pre-send rejection from looking like
 * a request that may already have crossed the native boundary after reload.
 */
export function bindStartOperationIfAllowed(
  agentType: AgentType,
  projectDir: string,
  sessions: AgentSession[],
  catalog: AgentStartCapability[] | null | undefined,
  bind: () => boolean
): StartOperationBindResult {
  if (!canStartInCurrentProject(agentType, projectDir, sessions, catalog)) {
    return 'unavailable'
  }
  return bind() ? 'bound' : 'persist_failed'
}

export function projectStartOptions(
  project: SessionTreeProject,
  catalog: AgentStartCapability[] | null | undefined
): StartAgentOption[] {
  if (!project.path.trim() || project.uncategorized) return []

  if (catalog !== null && catalog !== undefined) {
    return KNOWN_AGENT_TYPES.flatMap(agentType => {
      const cap = catalog.find(entry => entry.agent_type === agentType)
      if (!cap) return []
      if (isInstallGatedAgent(agentType) && !cap.available) return []
      return [{
        agentType,
        enabled: Boolean(cap.available && cap.spawn),
        reason: cap.reason
      }]
    })
  }

  const codex = project.agents.find(agent => agent.type === 'codex')
  return codex?.sessions.some(session => session.capabilities?.spawn === true)
    ? [{ agentType: 'codex', enabled: true }]
    : []
}
