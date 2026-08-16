import { describe, expect, it } from 'vitest'
import { NEAR_BOTTOM_THRESHOLD_PX, isNearBottomValue } from './scroll'

describe('isNearBottomValue', () => {
  // max scrollTop = scrollHeight - clientHeight = 900 in these fixtures
  it('true when the distance to the bottom is within the threshold', () => {
    expect(isNearBottomValue(900, 100, 1000)).toBe(true)
    expect(isNearBottomValue(900 - NEAR_BOTTOM_THRESHOLD_PX, 100, 1000)).toBe(true)
  })
  it('false when the user scrolled further up', () => {
    expect(isNearBottomValue(900 - NEAR_BOTTOM_THRESHOLD_PX - 1, 100, 1000)).toBe(false)
    expect(isNearBottomValue(0, 100, 1000)).toBe(false)
  })
  it('treats a non-scrollable container as pinned', () => {
    expect(isNearBottomValue(0, 0, 0)).toBe(true)
    expect(isNearBottomValue(0, 300, 300)).toBe(true)
  })
  it('honors a custom threshold', () => {
    expect(isNearBottomValue(850, 100, 1000, 40)).toBe(false)
    expect(isNearBottomValue(870, 100, 1000, 40)).toBe(true)
  })
})
