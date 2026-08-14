import { describe, expect, it } from 'vitest'
import { deviceThreadStatCount } from './threadCount'

describe('deviceThreadStatCount', () => {
  it('does not substitute a leftover active_agents hint when the live list is empty', () => {
    expect(deviceThreadStatCount(0, 0)).toBe(0)
    expect(deviceThreadStatCount(0, 2)).toBe(2)
    expect(deviceThreadStatCount(3, 1)).toBe(4)
  })
})
