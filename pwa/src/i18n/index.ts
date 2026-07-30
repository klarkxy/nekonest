import { createI18n } from 'vue-i18n'
import zhCN from './locales/zh-CN'
import en from './locales/en'

export const LOCALE_STORAGE_KEY = 'nekonest_locale'
export type AppLocale = 'zh-CN' | 'en'
export const SUPPORTED_LOCALES: AppLocale[] = ['zh-CN', 'en']

export type MessageSchema = typeof zhCN

function isVitest(): boolean {
  try {
    return Boolean(import.meta.env?.VITEST || import.meta.env?.MODE === 'test')
  } catch {
    return false
  }
}

function normalizeLocale(raw: string | null | undefined): AppLocale | null {
  if (!raw) return null
  const v = raw.trim()
  if (v === 'zh-CN' || v === 'zh' || v.toLowerCase().startsWith('zh')) return 'zh-CN'
  if (v === 'en' || v.toLowerCase().startsWith('en')) return 'en'
  return null
}

export function detectLocale(): AppLocale {
  try {
    const stored = normalizeLocale(localStorage.getItem(LOCALE_STORAGE_KEY))
    if (stored) return stored
  } catch {
    /* ignore */
  }
  // Vitest / unit tests: stable default regardless of host OS language.
  if (isVitest()) return 'zh-CN'
  if (typeof navigator !== 'undefined') {
    const fromNav = normalizeLocale(navigator.language)
    if (fromNav) return fromNav
  }
  return 'zh-CN'
}

const initialLocale: AppLocale = isVitest() ? 'zh-CN' : detectLocale()

export const i18n = createI18n({
  legacy: false,
  locale: initialLocale,
  fallbackLocale: 'zh-CN',
  messages: {
    'zh-CN': zhCN as MessageSchema,
    en: en as unknown as MessageSchema
  }
})

export function tGlobal(key: string, params?: Record<string, unknown>): string {
  const g = i18n.global as { t: (k: string, p?: Record<string, unknown>) => string }
  return g.t(key, params ?? {})
}

export function getLocale(): AppLocale {
  const g = i18n.global as { locale: { value: string } }
  return g.locale.value as AppLocale
}

export function setLocale(locale: AppLocale): void {
  if (!SUPPORTED_LOCALES.includes(locale)) return
  const g = i18n.global as { locale: { value: string } }
  g.locale.value = locale
  try {
    localStorage.setItem(LOCALE_STORAGE_KEY, locale)
  } catch {
    /* ignore */
  }
  if (typeof document !== 'undefined') {
    document.documentElement.lang = locale
  }
}

export function collatorLocale(): string {
  return getLocale() === 'en' ? 'en' : 'zh-CN'
}

/** Apply initial html lang as early as possible. */
export function applyDocumentLang(): void {
  if (typeof document !== 'undefined') {
    document.documentElement.lang = getLocale()
  }
}

export default i18n
