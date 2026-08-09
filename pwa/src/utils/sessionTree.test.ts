import { describe, expect, it } from 'vitest'
import type { AgentSession, AgentType } from '@/types/protocol'
import { tGlobal } from '@/i18n'
import {
  buildSessionTree,
  normalizeProjectDir,
  UNCATEGORIZED_PROJECT_KEY
} from './sessionTree'

function session(
  id: string,
  agentType: AgentType,
  projectDir?: string,
  extra: Partial<AgentSession> = {}
): AgentSession {
  return {
    id,
    device_id: 'device-1',
    agent_type: agentType,
    status: 'idle',
    summary: id,
    last_activity: 0,
    project_dir: projectDir,
    ...extra
  }
}

describe('normalizeProjectDir', () => {
  it('normalizes Windows separators, duplicate slashes and trailing separators', () => {
    expect(normalizeProjectDir('  D:\\nekonest\\\\pwa\\  ')).toBe('D:/nekonest/pwa')
    expect(normalizeProjectDir('D:')).toBe('D:/')
    expect(normalizeProjectDir('\\\\server\\share\\\\repo\\')).toBe('//server/share/repo')
  })
})

describe('buildSessionTree', () => {
  it('groups by normalized full project_dir and keeps same leaf names separate', () => {
    const tree = buildSessionTree([
      session('a', 'codex', 'D:\\one\\app'),
      session('b', 'claude_code', 'd:/one/app/'),
      session('c', 'codex', 'D:\\two\\app')
    ])

    expect(tree).toHaveLength(2)
    expect(tree[0].sessionCount).toBe(2)
    expect(tree[0].agents.map(agent => agent.type)).toEqual(['claude_code', 'codex'])
    expect(tree[1].sessionCount).toBe(1)
    expect(tree.map(project => project.path)).toEqual(['D:/one/app', 'D:/two/app'])
  })

  it('strictly sends sessions without project_dir to uncategorized even with project name', () => {
    const tree = buildSessionTree([
      session('named-only', 'kimi_cli', undefined, { project: 'looks-like-a-project' }),
      session('blank-dir', 'grok_build', '   ')
    ])

    expect(tree).toHaveLength(1)
    expect(tree[0].key).toBe(UNCATEGORIZED_PROJECT_KEY)
    expect(tree[0].label).toBe(tGlobal('agent.uncategorized'))
    expect(tree[0].agents.map(agent => agent.type)).toEqual(['kimi_cli', 'grok_build'])
  })

  it('creates only agents that have sessions in each project and puts uncategorized last', () => {
    const tree = buildSessionTree([
      session('orphan', 'grok_build'),
      session('codex-only', 'codex', 'D:\\workspace\\solo')
    ])

    expect(tree.map(project => project.key)).toEqual([
      'path:d:/workspace/solo',
      UNCATEGORIZED_PROJECT_KEY
    ])
    expect(tree[0].agents.map(agent => agent.type)).toEqual(['codex'])
  })

  it('sorts projects by their most recent session activity instead of project name', () => {
    const tree = buildSessionTree([
      session('alphabetically-first', 'codex', 'D:\\workspace\\alpha', { last_activity: 10 }),
      session('most-recent', 'grok_build', 'D:\\workspace\\zulu', { last_activity: 30 }),
      session('middle', 'claude_code', 'D:\\workspace\\middle', { last_activity: 20 }),
      session('newest-orphan', 'grok_build', undefined, { last_activity: 40 })
    ])

    expect(tree.map(project => project.key)).toEqual([
      UNCATEGORIZED_PROJECT_KEY,
      'path:d:/workspace/zulu',
      'path:d:/workspace/middle',
      'path:d:/workspace/alpha'
    ])
  })

  it('sorts within each project-agent leaf without mutating the input', () => {
    const input = [
      session('old', 'codex', 'D:\\repo', { last_activity: 1 }),
      session('new', 'codex', 'D:\\repo', { last_activity: 3 })
    ]
    const originalIds = input.map(item => item.id)
    const tree = buildSessionTree(input, list => [...list].reverse())

    expect(tree[0].agents[0].sessions.map(item => item.id)).toEqual(['new', 'old'])
    expect(input.map(item => item.id)).toEqual(originalIds)
  })
})
