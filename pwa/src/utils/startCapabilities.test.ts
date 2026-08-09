import { describe, expect, it } from 'vitest'
import type { SessionTreeProject } from './sessionTree'
import { canStartInCurrentProject, projectStartOptions } from './startCapabilities'

const project: SessionTreeProject = {
  key: 'path:d:/repo',
  label: 'repo',
  path: 'D:/repo',
  uncategorized: false,
  lastActivity: 1,
  sessionCount: 1,
  agents: [{
    key: 'path:d:/repo::codex',
    type: 'codex',
    label: 'Codex',
    avatar: '',
    color: '',
    softColor: '',
    lastActivity: 1,
    sessions: [{
      id: 'codex-a', device_id: 'device-a', agent_type: 'codex', status: 'idle',
      summary: '', last_activity: 1, project_dir: 'D:/repo', capabilities: { spawn: true }
    }]
  }]
}

describe('project start capabilities', () => {
  it('lists all known agents from a catalog and disables unavailable entries', () => {
    const options = projectStartOptions(project, [
      { agent_type: 'claude_code', available: true, spawn: true },
      { agent_type: 'grok_build', available: false, spawn: false, reason: 'CLI missing' }
    ])

    expect(options).toHaveLength(4)
    expect(options).toContainEqual({ agentType: 'claude_code', enabled: true, reason: undefined })
    expect(options).toContainEqual({ agentType: 'grok_build', enabled: false, reason: 'CLI missing' })
  })

  it('uses only a positive legacy Codex session capability when the catalog is absent', () => {
    expect(projectStartOptions(project, null)).toEqual([{ agentType: 'codex', enabled: true }])
    expect(projectStartOptions({ ...project, uncategorized: true }, null)).toEqual([])
  })

  it('rejects stale phone-only paths even when the agent catalog is available', () => {
    const catalog = [{ agent_type: 'kimi_cli' as const, available: true, spawn: true }]
    const sessions = project.agents[0].sessions
    expect(canStartInCurrentProject('kimi_cli', 'D:/repo', sessions, catalog)).toBe(true)
    expect(canStartInCurrentProject('kimi_cli', 'D:/stale', sessions, catalog)).toBe(false)
  })
})
