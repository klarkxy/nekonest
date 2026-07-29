import { describe, expect, it } from 'vitest'
import {
  agentColor,
  agentIcon,
  agentLabel,
  groupSessionsByAgent,
  projectDisplay,
  sessionActivityPresentation,
  shortSummary,
  statusLabel,
  statusTagType
} from './agent'
import { getAgentMeta } from '@/config/agents'
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
    expect(agentIcon('claude_code')).toBe('🟠')
    expect(agentColor('codex')).toMatch(/^#/)
    expect(getAgentMeta('kimi_cli').avatar).toBe('/agents/kimi-cli.webp')
    expect(getAgentMeta('grok_build').label).toBe('Grok Build')
    expect(getAgentMeta('future_agent').avatar).toBe('/agents/unknown.webp')
    expect(getAgentMeta('unknown').label).toBe('未知猫娘')
    expect(agentLabel('other')).toBe('other')
  })

  it('status maps', () => {
    expect(statusLabel('running')).toBe('忙碌中')
    expect(sessionActivityPresentation('running')).toEqual({
      icon: '🐾',
      label: '忙碌中',
      headline: '线团还在转',
      detail: '家里的猫娘还在干活，新消息会同步过来。',
      tone: 'active'
    })
    expect(sessionActivityPresentation('idle').label).toBe('待命')
    expect(sessionActivityPresentation('waiting_approval').label).toBe('电脑待批')
    expect(sessionActivityPresentation('waiting_approval', true).label).toBe('电脑待批')
    expect(sessionActivityPresentation('idle', true).label).toBe('回复中')
    expect(sessionActivityPresentation('nope').tone).toBe('unknown')
    expect(statusTagType('waiting_approval')).toBe('warning')
    expect(statusTagType('nope')).toBe('default')
  })

  it('shortSummary', () => {
    expect(shortSummary(undefined)).toBe('未命名线团')
    expect(shortSummary('  a   b  ')).toBe('a b')
    expect(shortSummary('x'.repeat(60), 10)).toBe('x'.repeat(10) + '…')
  })

  it('groupSessionsByAgent order', () => {
    const groups = groupSessionsByAgent([
      s({ id: '1', agent_type: 'codex', last_activity: 1 }),
      s({ id: '2', agent_type: 'kilo', last_activity: 2 }),
      s({ id: '3', agent_type: 'claude_code', last_activity: 3 }),
      s({ id: '4', agent_type: 'other', last_activity: 4 })
    ])
    expect(groups.map(g => g.type)).toEqual(['claude_code', 'codex', 'kilo', 'other'])
  })

  it('projectDisplay', () => {
    expect(projectDisplay(s({ id: '1', agent_type: 'kilo' }))).toBeNull()
    const p = projectDisplay(
      s({ id: '1', agent_type: 'kilo', project_dir: 'D:\\nekonest\\app' })
    )
    expect(p?.name).toBe('app')
  })
})
