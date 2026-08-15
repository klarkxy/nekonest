import { describe, expect, it } from 'vitest'
import { liveThinkingMessageId } from './thinking'

describe('liveThinkingMessageId', () => {
  it('is empty when the turn is not streaming', () => {
    expect(liveThinkingMessageId(
      [{ id: 't1', type: 'thinking' }],
      false
    )).toBe('')
  })

  it('is the last thinking message while that bubble is still growing', () => {
    expect(liveThinkingMessageId(
      [
        { id: 'u1', type: 'text' },
        { id: 't1', type: 'thinking' }
      ],
      true
    )).toBe('t1')
  })

  it('clears once assistant text has started after thinking', () => {
    expect(liveThinkingMessageId(
      [
        { id: 't1', type: 'thinking' },
        { id: 'a1', type: 'text' }
      ],
      true
    )).toBe('')
  })
})
