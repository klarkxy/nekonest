import { describe, expect, it } from 'vitest'
import { formatRelativeActivity } from './time'

const NOW = Date.parse('2026-08-02T09:00:00+08:00')

describe('formatRelativeActivity', () => {
  it('returns empty for missing or zero timestamps', () => {
    expect(formatRelativeActivity(undefined, NOW, 'zh-CN')).toBe('')
    expect(formatRelativeActivity(0, NOW, 'zh-CN')).toBe('')
  })

  it('buckets Chinese and English relative labels from an injected now', () => {
    const sec = Math.floor(NOW / 1000)
    expect(formatRelativeActivity(sec - 20, NOW, 'zh-CN')).toBe('刚刚')
    expect(formatRelativeActivity(sec - 20, NOW, 'en')).toBe('just now')
    expect(formatRelativeActivity(sec - 5 * 60, NOW, 'zh-CN')).toBe('5 分钟前')
    expect(formatRelativeActivity(sec - 3 * 3600, NOW, 'en')).toBe('3h ago')

    const shiftDays = (days: number) => {
      const d = new Date(NOW)
      d.setDate(d.getDate() - days)
      d.setHours(0, 0, 0, 0)
      return Math.floor(d.getTime() / 1000)
    }
    expect(formatRelativeActivity(shiftDays(1), NOW, 'zh-CN')).toBe('昨天')
    expect(formatRelativeActivity(shiftDays(3), NOW, 'en')).toBe('3d ago')
    expect(formatRelativeActivity(shiftDays(20), NOW, 'zh-CN')).toMatch(/7/)
  })
})
