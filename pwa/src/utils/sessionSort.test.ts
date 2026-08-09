import { describe, expect, it } from 'vitest'
import { ensureManualOrder, sortSessionsByMode } from './sessionSort'
import type { AgentSession } from '@/types/protocol'

const s = (id: string, extra: Partial<AgentSession> = {}): AgentSession =>
  ({
    id,
    device_id: 'd',
    agent_type: 'grok_build',
    status: 'idle',
    summary: id,
    last_activity: 0,
    ...extra
  }) as AgentSession

describe('sortSessionsByMode', () => {
  const list = [
    s('a', { last_activity: 1, summary: 'beta', project: 'z' }),
    s('b', { last_activity: 3, summary: 'alpha', project: 'a' }),
    s('c', { last_activity: 2, summary: 'gamma', project: 'a' })
  ]

  it('recent', () => {
    expect(sortSessionsByMode(list, 'recent').map(x => x.id)).toEqual(['b', 'c', 'a'])
  })

  it('name', () => {
    expect(sortSessionsByMode(list, 'name').map(x => x.id)).toEqual(['b', 'a', 'c'])
  })

  it('project then recent', () => {
    expect(sortSessionsByMode(list, 'project').map(x => x.id)).toEqual(['b', 'c', 'a'])
  })

  it('manual order', () => {
    expect(sortSessionsByMode(list, 'manual', ['c', 'a', 'b']).map(x => x.id)).toEqual([
      'c',
      'a',
      'b'
    ])
  })
})

describe('ensureManualOrder', () => {
  it('appends without dropping other groups', () => {
    expect(ensureManualOrder(['a', 'b', 'gone'], ['b', 'c'])).toEqual(['a', 'b', 'gone', 'c'])
  })
})
