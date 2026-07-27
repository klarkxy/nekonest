import { describe, expect, it } from 'vitest'
import { notificationTag } from './notification'

describe('notificationTag', () => {
  it('keeps the complete server-provided tag without duplicating identity', () => {
    expect(notificationTag({
      tag: 'nekonest:device-a:session-a',
      device_id: 'device-a',
      session_id: 'session-a'
    })).toBe('nekonest:device-a:session-a')
  })

  it('derives a device and session scoped fallback', () => {
    expect(notificationTag({
      device_id: 'device-a',
      session_id: 'session-a'
    })).toBe('nekonest:device-a:session-a')
  })
})
