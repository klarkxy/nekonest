import { afterEach, describe, expect, it, vi } from 'vitest'
import { createApprovalDecisionGuard } from './approvalDecision'

afterEach(() => {
  vi.useRealTimers()
})

describe('approval decision guard', () => {
  it('allows only one decision for the current approval id', () => {
    const guard = createApprovalDecisionGuard()

    expect(guard.begin('approval-1')).toBe(true)
    expect(guard.begin('approval-1')).toBe(false)
    expect(guard.isPending('approval-1')).toBe(true)

    guard.sync('approval-2')
    expect(guard.isPending('approval-1')).toBe(false)
    expect(guard.begin('approval-2')).toBe(true)
  })

  it('releases a stuck decision after the retry timeout', () => {
    vi.useFakeTimers()
    const guard = createApprovalDecisionGuard(100)

    expect(guard.begin('approval-1')).toBe(true)
    vi.advanceTimersByTime(100)
    expect(guard.isPending('approval-1')).toBe(false)
    expect(guard.begin('approval-1')).toBe(true)

    guard.dispose()
  })
})
