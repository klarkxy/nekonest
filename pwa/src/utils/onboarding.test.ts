import { describe, expect, it } from 'vitest'
import {
  normalizePairCode,
  normalizePhoneSecret,
  WINDOWS_PAIR_COMMANDS
} from './onboarding'

describe('onboarding helpers', () => {
  it('rejects a blank phone secret instead of inventing a development key', () => {
    expect(normalizePhoneSecret('   ')).toBeNull()
    expect(normalizePhoneSecret('  actual-secret  ')).toBe('actual-secret')
  })

  it('keeps the Windows pairing commands concrete', () => {
    expect(WINDOWS_PAIR_COMMANDS.register).toBe(
      '.\\nekonest-daemon.exe -register -name "书房电脑"'
    )
    expect(WINDOWS_PAIR_COMMANDS.pair).toBe('.\\nekonest-daemon.exe -pair gen')
  })

  it('normalizes pasted pair codes before validation', () => {
    expect(normalizePairCode(' AB-CD ef ')).toBe('abcdef')
    expect(normalizePairCode('12 34 56 78')).toBe('123456')
  })
})
