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
