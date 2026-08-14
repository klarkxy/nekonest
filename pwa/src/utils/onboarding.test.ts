import { describe, expect, it } from 'vitest'
import {
  normalizePairCode,
  normalizePhoneSecret,
  PAIR_COMMANDS,
  WINDOWS_PAIR_COMMANDS
} from './onboarding'

describe('onboarding helpers', () => {
  it('rejects a blank phone secret instead of inventing a development key', () => {
    expect(normalizePhoneSecret('   ')).toBeNull()
    expect(normalizePhoneSecret('  actual-secret  ')).toBe('actual-secret')
  })

  it('keeps Windows and Linux pairing commands concrete', () => {
    expect(WINDOWS_PAIR_COMMANDS).toEqual(PAIR_COMMANDS.windows)
    expect(PAIR_COMMANDS.windows.register).toBe(
      '.\\nekonest-daemon.exe -register -name "家里电脑"'
    )
    expect(PAIR_COMMANDS.windows.pair).toBe('.\\nekonest-daemon.exe -pair gen')
    expect(PAIR_COMMANDS.linux.register).toBe(
      './nekonest-daemon -register -name "家里电脑"'
    )
    expect(PAIR_COMMANDS.linux.pair).toBe('./nekonest-daemon -pair gen')
  })

  it('normalizes pasted pair codes before validation', () => {
    expect(normalizePairCode(' AB-CD ef ')).toBe('abcdef')
    expect(normalizePairCode('12 34 56 78')).toBe('123456')
  })
})
