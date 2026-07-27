import { describe, expect, it } from 'vitest'
import {
  agentColor,
  agentIcon,
  agentLabel,
  groupSessionsByAgent,
  projectDisplay,
  shortSummary,
  statusLabel,
  statusTagType
} from './agent'
import type { AgentSession } from '@/types/protocol'

const s = (p: Partial<AgentSession> & Pick<AgentSession, 'id' | 'agent_type'>): AgentSession =>
  ({
    device_id: 'd',
    status: 'idle',
    summary: '',
    last_activity: 0,
    ...p
  }) as AgentSession

describe('agent helpers', () => {
  it('labels icons colors', () => {
    expect(agentLabel('kilo')).toBe('Kilo')
    expect(agentIcon('claude_code')).toBe('🟣')
    expect(agentColor('codex')).toMatch(/^#/)
    expect(agentLabel('other')).toBe('other')
  })

  it('status maps', () => {
    expect(statusLabel('running')).toBe('运行中')
    expect(statusTagType('waiting_approval')).toBe('warning')
    expect(statusTagType('nope')).toBe('default')
  })

  it('shortSummary', () => {
    expect(shortSummary(undefined)).toBe('空闲会话')
    expect(shortSummary('  a   b  ')).toBe('a b')
    expect(shortSummary('x'.repeat(60), 10)).toBe('x'.repeat(10) + '…')
  })

  it('groupSessionsByAgent order', () => {
    const groups = groupSessionsByAgent([
      s({ id: '1', agent_type: 'codex', last_activity: 1 }),
      s({ id: '2', agent_type: 'kilo', last_activity: 2 }),
      s({ id: '3', agent_type: 'claude_code', last_activity: 3 }),
      s({ id: '4', agent_type: 'other' as never, last_activity: 4 })
    ])
    expect(groups.map(g => g.type)).toEqual(['kilo', 'claude_code', 'codex', 'other'])
  })

  it('projectDisplay', () => {
    expect(projectDisplay(s({ id: '1', agent_type: 'kilo' }))).toBeNull()
    const p = projectDisplay(
      s({ id: '1', agent_type: 'kilo', project_dir: 'D:\\nekonest\\app' })
    )
    expect(p?.name).toBe('app')
  })
})
