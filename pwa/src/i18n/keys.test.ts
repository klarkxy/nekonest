import { describe, expect, it } from 'vitest'
import zhCN from './locales/zh-CN'
import en from './locales/en'

function flattenKeys(obj: Record<string, unknown>, prefix = ''): string[] {
  const keys: string[] = []
  for (const [k, v] of Object.entries(obj)) {
    const path = prefix ? `${prefix}.${k}` : k
    if (v && typeof v === 'object' && !Array.isArray(v)) {
      keys.push(...flattenKeys(v as Record<string, unknown>, path))
    } else {
      keys.push(path)
    }
  }
  return keys.sort()
}

describe('i18n locale parity', () => {
  it('en and zh-CN share the same key set', () => {
    const zhKeys = flattenKeys(zhCN as unknown as Record<string, unknown>)
    const enKeys = flattenKeys(en as unknown as Record<string, unknown>)
    expect(enKeys).toEqual(zhKeys)
  })
})
