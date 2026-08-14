export const PAIR_COMMANDS = {
  windows: {
    register: '.\\nekonest-daemon.exe -register -name "家里电脑"',
    pair: '.\\nekonest-daemon.exe -pair gen'
  },
  linux: {
    register: './nekonest-daemon -register -name "家里电脑"',
    pair: './nekonest-daemon -pair gen'
  }
} as const

/** @deprecated use PAIR_COMMANDS.windows */
export const WINDOWS_PAIR_COMMANDS = PAIR_COMMANDS.windows

export function normalizePhoneSecret(value: string): string | null {
  const normalized = value.trim()
  return normalized || null
}

export function normalizePairCode(value: string): string {
  return value
    .toLowerCase()
    .replace(/[^0-9a-f]/g, '')
    .slice(0, 6)
}
