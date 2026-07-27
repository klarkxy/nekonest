import { describe, expect, it } from 'vitest'
import { mergeHistoryLists, upsertMessageList } from './messageMerge'
import type { SessionMessage } from '@/types/protocol'

const msg = (p: Partial<SessionMessage> & Pick<SessionMessage, 'id' | 'role' | 'content'>): SessionMessage => ({
  timestamp: 0,
  type: 'text',
  ...p
})

describe('upsertMessageList', () => {
  it('mints id when missing', () => {
    const out = upsertMessageList([], msg({ id: '', role: 'user', content: 'a' }), () => 'fixed')
    expect(out[0].id).toBe('fixed')
  })

  it('prefers longer content on patch', () => {
    const base = [msg({ id: '1', role: 'assistant', content: 'hello' })]
    const short = upsertMessageList(base, msg({ id: '1', role: 'assistant', content: 'hi' }))
    expect(short[0].content).toBe('hello')
    const long = upsertMessageList(base, msg({ id: '1', role: 'assistant', content: 'hello world' }))
    expect(long[0].content).toBe('hello world')
  })

  it('appends new messages', () => {
    const out = upsertMessageList(
      [msg({ id: '1', role: 'user', content: 'a' })],
      msg({ id: '2', role: 'assistant', content: 'b' })
    )
    expect(out.map(m => m.id)).toEqual(['1', '2'])
  })

  it('maps an imported copy to the canonical message', () => {
    const imported = msg({
      id: 'prt_1',
      role: 'user',
      content: 'ping',
      timestamp: 105,
      metadata: { imported: true, agent_type: 'codex' }
    })
    const canonical = msg({
      id: 'msg_1',
      role: 'user',
      content: 'ping',
      timestamp: 100,
      metadata: { agent_type: 'codex', marker: 'canonical' }
    })

    const out = upsertMessageList([imported], canonical)

    expect(out).toHaveLength(1)
    expect(out[0].id).toBe('msg_1')
    expect(out[0].metadata?.marker).toBe('canonical')
  })
})

describe('mergeHistoryLists', () => {
  it('no-op on empty hist', () => {
    const cur = [msg({ id: '1', role: 'user', content: 'x' })]
    expect(mergeHistoryLists(cur, [])).toBe(cur)
  })

  it('merges an acknowledged prompt only by its stable id', () => {
    const cur = [
      msg({ id: 'msg_1', role: 'user', content: 'hello', timestamp: 1 }),
      msg({ id: 'a1', role: 'assistant', content: 'hi', timestamp: 2 })
    ]
    const hist = [
      msg({ id: 'msg_1', role: 'user', content: 'hello', timestamp: 1 }),
      msg({ id: 'a1', role: 'assistant', content: 'hi!', timestamp: 2 })
    ]
    const out = mergeHistoryLists(cur, hist)
    expect(out.find(m => m.id === 'msg_1')?.content).toBe('hello')
    expect(out.find(m => m.id === 'a1')?.content).toBe('hi!')
  })

  it('keeps longer local content for same id', () => {
    const cur = [msg({ id: 'm1', role: 'assistant', content: 'streamed more', timestamp: 1 })]
    const hist = [msg({ id: 'm1', role: 'assistant', content: 'stream', timestamp: 1 })]
    const out = mergeHistoryLists(cur, hist)
    expect(out[0].content).toBe('streamed more')
  })

  it('sorts by timestamp', () => {
    const cur = [msg({ id: 'b', role: 'user', content: 'b', timestamp: 20 })]
    const hist = [msg({ id: 'a', role: 'user', content: 'a', timestamp: 10 })]
    expect(mergeHistoryLists(cur, hist).map(m => m.id)).toEqual(['a', 'b'])
  })

  it('keeps second pending until second hist arrives', () => {
    const cur = [
      msg({ id: 'msg_1', role: 'user', content: '继续', timestamp: 1 }),
      msg({ id: 'msg_2', role: 'user', content: '继续', timestamp: 2 })
    ]
    const hist = [msg({ id: 'msg_1', role: 'user', content: '继续', timestamp: 1 })]
    const out = mergeHistoryLists(cur, hist)
    expect(out.map(m => m.id).sort()).toEqual(['msg_1', 'msg_2'].sort())
  })

  it('keeps old history and a pending prompt with identical content but different ids', () => {
    const cur = [
      msg({ id: 'msg_new', role: 'user', content: '继续', timestamp: 2 })
    ]
    const hist = [msg({ id: 'user_old', role: 'user', content: '继续', timestamp: 1 })]
    const out = mergeHistoryLists(cur, hist)
    expect(out.map(m => m.id)).toEqual(['user_old', 'msg_new'])
  })

  it('collapses the latest four canonical/imported records into ping and pong', () => {
    const current = [
      msg({
        id: 'msg_ping',
        role: 'user',
        content: 'ping',
        timestamp: 100,
        metadata: { agent_type: 'codex' }
      }),
      msg({
        id: 'msg_pong',
        role: 'assistant',
        content: 'pong',
        timestamp: 102,
        metadata: { agent_type: 'codex' }
      })
    ]
    const hist = [
      msg({
        id: 'codex_ping',
        role: 'user',
        content: 'ping',
        timestamp: 101,
        metadata: { imported: true, agent_type: 'codex' }
      }),
      msg({
        id: 'codex_pong',
        role: 'assistant',
        content: 'pong',
        timestamp: 103,
        metadata: { imported: true, agent_type: 'codex' }
      })
    ]

    const out = mergeHistoryLists(current, hist)

    expect(out.map(m => m.id)).toEqual(['msg_ping', 'msg_pong'])
  })

  it('keeps two consecutive same-content canonical messages after one-to-one matching', () => {
    const current = [
      msg({ id: 'msg_1', role: 'user', content: '继续', timestamp: 100 }),
      msg({ id: 'msg_2', role: 'user', content: '继续', timestamp: 110 })
    ]
    const hist = [
      msg({
        id: 'prt_1',
        role: 'user',
        content: '继续',
        timestamp: 101,
        metadata: { imported: true }
      }),
      msg({
        id: 'prt_2',
        role: 'user',
        content: '继续',
        timestamp: 111,
        metadata: { imported: true }
      })
    ]

    const out = mergeHistoryLists(current, hist)

    expect(out.map(m => m.id)).toEqual(['msg_1', 'msg_2'])
  })

  it('drops only the closest imported copy when one canonical has two candidates', () => {
    const current = [
      msg({ id: 'msg_1', role: 'user', content: '继续', timestamp: 100 })
    ]
    const hist = [
      msg({
        id: 'prt_near',
        role: 'user',
        content: '继续',
        timestamp: 102,
        metadata: { imported: true }
      }),
      msg({
        id: 'prt_far',
        role: 'user',
        content: '继续',
        timestamp: 110,
        metadata: { imported: true }
      })
    ]

    const out = mergeHistoryLists(current, hist)

    expect(out.map(m => m.id)).toEqual(['msg_1', 'prt_far'])
  })

  it('does not merge an imported message outside the 15-second window', () => {
    const current = [
      msg({ id: 'msg_1', role: 'user', content: 'ping', timestamp: 100 })
    ]
    const hist = [
      msg({
        id: 'prt_1',
        role: 'user',
        content: 'ping',
        timestamp: 116,
        metadata: { imported: true }
      })
    ]

    expect(mergeHistoryLists(current, hist).map(m => m.id)).toEqual(['msg_1', 'prt_1'])
  })

  it('does not merge two non-imported messages', () => {
    const current = [
      msg({ id: 'msg_1', role: 'user', content: 'ping', timestamp: 100 })
    ]
    const hist = [
      msg({ id: 'msg_2', role: 'user', content: 'ping', timestamp: 101 })
    ]

    expect(mergeHistoryLists(current, hist).map(m => m.id)).toEqual(['msg_1', 'msg_2'])
  })

  it('does not merge messages with conflicting agent types', () => {
    const current = [
      msg({
        id: 'msg_1',
        role: 'user',
        content: 'ping',
        timestamp: 100,
        metadata: { agent_type: 'codex' }
      })
    ]
    const hist = [
      msg({
        id: 'prt_1',
        role: 'user',
        content: 'ping',
        timestamp: 101,
        metadata: { imported: true, agent_type: 'kilo' }
      })
    ]

    expect(mergeHistoryLists(current, hist).map(m => m.id)).toEqual(['msg_1', 'prt_1'])
  })
})
