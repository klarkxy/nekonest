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
})
