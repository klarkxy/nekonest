import { describe, expect, it } from 'vitest'
import { tGlobal } from '@/i18n'
import {
  agentColor,
  agentIcon,
  agentLabel,
  groupSessionsByAgent,
  projectDisplay,
  sessionActivityPresentation,
  sessionSearchHaystack,
  shortSummary,
  statusLabel,
  threadDisplayTitle,
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
    expect(agentLabel('grok_build')).toBe('Grok Build')
    expect(agentIcon('claude_code')).toBe('🟠')
    expect(agentColor('codex')).toMatch(/^#/)
    expect(getAgentMeta('kimi_cli').avatar).toBe('/agents/kimi-cli.webp')
    expect(getAgentMeta('grok_build').label).toBe('Grok Build')
    expect(getAgentMeta('future_agent').avatar).toBe('/agents/unknown.webp')
    expect(agentLabel('unknown')).toBe(tGlobal('agent.unknown'))
    expect(agentLabel('other')).toBe('other')
  })

  it('status maps', () => {
    expect(statusLabel('running')).toBe(tGlobal('status.running.label'))
    expect(sessionActivityPresentation('running')).toEqual({
      icon: tGlobal('status.running.icon'),
      label: tGlobal('status.running.label'),
      headline: tGlobal('status.running.headline'),
      detail: tGlobal('status.running.detail'),
      tone: 'active'
    })
    expect(sessionActivityPresentation('idle').label).toBe(tGlobal('status.idle.label'))
    expect(sessionActivityPresentation('waiting_approval').label).toBe(
      tGlobal('status.waiting_approval.label')
    )
    expect(sessionActivityPresentation('waiting_approval', true).label).toBe(
      tGlobal('status.waiting_approval.label')
    )
    expect(sessionActivityPresentation('idle', true).label).toBe(tGlobal('status.streaming.label'))
    expect(sessionActivityPresentation('nope').tone).toBe('unknown')
    expect(statusTagType('waiting_approval')).toBe('warning')
    expect(statusTagType('nope')).toBe('default')
  })

  it('shortSummary', () => {
    expect(shortSummary(undefined)).toBe(tGlobal('agent.untitledThread'))
    expect(shortSummary('  a   b  ')).toBe('a b')
    expect(shortSummary('x'.repeat(60), 10)).toBe('x'.repeat(10) + '…')
    expect(shortSummary('255d65ae-b684-44de-b181-60aacf81df0a')).toBe(
      tGlobal('agent.untitledThread')
    )
    expect(shortSummary('019fff63-3feb-7571-be27-9a57f4a47a1d')).toBe(
      tGlobal('agent.untitledThread')
    )
    expect(shortSummary('第一行标题\n后面还有很多助手输出')).toBe('第一行标题')
  })

  it('threadDisplayTitle falls back to project and time instead of a wall of untitled', () => {
    expect(threadDisplayTitle('019fff63-3feb-7571-be27-9a57f4a47a1d', ['nekonest', '3 分钟前'])).toBe(
      'nekonest · 3 分钟前'
    )
    expect(threadDisplayTitle('修好目录门闩', ['nekonest'])).toBe('修好目录门闩')
  })

  it('sessionSearchHaystack matches agent labels and untitled copy', () => {
    const hay = sessionSearchHaystack({
      id: 's1',
      summary: '',
      project: 'nekonest',
      project_dir: 'D:/code/nekonest',
      agent_type: 'codex'
    }, ['Codex', tGlobal('agent.untitledThread')])
    expect(hay).toContain('codex')
    expect(hay).toContain(tGlobal('agent.untitledThread').toLowerCase())
    expect(hay).toContain('nekonest')
  })

  it('groupSessionsByAgent order', () => {
    const groups = groupSessionsByAgent([
      s({ id: '1', agent_type: 'codex', last_activity: 1 }),
      s({ id: '2', agent_type: 'kimi_cli', last_activity: 2 }),
      s({ id: '3', agent_type: 'claude_code', last_activity: 3 }),
      s({ id: '4', agent_type: 'other', last_activity: 4 })
    ])
    expect(groups.map(g => g.type)).toEqual(['claude_code', 'codex', 'kimi_cli', 'other'])
  })

  it('projectDisplay', () => {
    expect(projectDisplay(s({ id: '1', agent_type: 'grok_build' }))).toBeNull()
    const p = projectDisplay(
      s({ id: '1', agent_type: 'grok_build', project_dir: 'D:\\nekonest\\app' })
    )
    expect(p?.name).toBe('app')
  })
})
