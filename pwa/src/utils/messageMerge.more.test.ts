import { describe, expect, it } from 'vitest'
import { mergeHistoryLists, upsertMessageList } from './messageMerge'
import type { SessionMessage } from '@/types/protocol'

const msg = (
  p: Partial<SessionMessage> & Pick<SessionMessage, 'id' | 'role' | 'content'>
): SessionMessage => ({
  timestamp: 0,
  type: 'text',
  ...p
})

describe('upsert cap', () => {
  it('caps at 500 messages', () => {
    let list: SessionMessage[] = []
    for (let i = 0; i < 505; i++) {
      list = upsertMessageList(list, msg({ id: `m${i}`, role: 'user', content: String(i) }))
    }
    expect(list.length).toBe(500)
    expect(list[0].id).toBe('m5')
    expect(list[list.length - 1].id).toBe('m504')
  })
})

describe('mergeHistory edge', () => {
  it('prefers longer local stream content', () => {
    const cur = [msg({ id: 'a1', role: 'assistant', content: 'hello world', timestamp: 1 })]
    const hist = [msg({ id: 'a1', role: 'assistant', content: 'hello', timestamp: 1 })]
    expect(mergeHistoryLists(cur, hist)[0].content).toBe('hello world')
  })
})

describe('streaming hot path with imported rows', () => {
  it('a live delta patch does not resurrect a removed imported duplicate', () => {
    // History import produced a canonical copy plus a leftover imported row
    // that matched nothing yet.
    const canonical = msg({
      id: 'msg_1',
      role: 'user',
      content: 'ping',
      timestamp: 100,
      metadata: { agent_type: 'codex' }
    })
    const imported = msg({
      id: 'prt_1',
      role: 'user',
      content: 'ping',
      timestamp: 101,
      metadata: { imported: true, agent_type: 'codex' }
    })
    // The canonical upsert must fold the imported copy away (existing contract).
    let list = upsertMessageList([imported], canonical)
    expect(list.map(m => m.id)).toEqual(['msg_1'])
    // Streaming deltas on other messages must not bring prt_1 back.
    list = upsertMessageList(list, msg({ id: 'a1', role: 'assistant', content: 'pong', timestamp: 102 }))
    expect(list.map(m => m.id)).toEqual(['msg_1', 'a1'])
    list = upsertMessageList(list, msg({ id: 'a1', role: 'assistant', content: 'pong!', timestamp: 102 }))
    expect(list.map(m => m.id)).toEqual(['msg_1', 'a1'])
    expect(list[1].content).toBe('pong!')
  })

  it('keeps an unmatched imported row across pure-live appends', () => {
    const imported = msg({
      id: 'prt_old',
      role: 'user',
      content: '继续',
      timestamp: 10,
      metadata: { imported: true }
    })
    const list = upsertMessageList(
      [imported],
      msg({ id: 'live_1', role: 'assistant', content: 'ok', timestamp: 200 })
    )
    expect(list.map(m => m.id)).toEqual(['prt_old', 'live_1'])
  })

  it('still dedupes when a live patch lands on a list with imported rows', () => {
    // A canonical arrives whose content transiently equals a surviving
    // imported copy within the 15s window; the pairwise dedupe must run.
    const imported = msg({
      id: 'prt_1',
      role: 'assistant',
      content: 'partial',
      timestamp: 100,
      metadata: { imported: true }
    })
    const streaming = msg({
      id: 'msg_1',
      role: 'assistant',
      content: 'part',
      timestamp: 100
    })
    let list = upsertMessageList([imported], streaming)
    expect(list.map(m => m.id)).toEqual(['prt_1', 'msg_1'])
    list = upsertMessageList(list, msg({ id: 'msg_1', role: 'assistant', content: 'partial', timestamp: 100 }))
    expect(list.map(m => m.id)).toEqual(['msg_1'])
  })
})
