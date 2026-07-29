export const WINDOWS_PAIR_COMMANDS = {
  register: '.\\nekonest-daemon.exe -register -name "书房电脑"',
  pair: '.\\nekonest-daemon.exe -pair gen'
} as const

export function normalizePhoneSecret(value: string): string | null {
  const normalized = value.trim()
  return normalized || null
}
