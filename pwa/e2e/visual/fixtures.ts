import { expect, type APIRequestContext, type Page } from '@playwright/test'

export const FIXED_NOW_MS = Date.parse('2026-08-02T09:00:00+08:00')
export const MAIN_DEVICE_ID = 'device-windows'
export const MAIN_SESSION_ID = 'session-codex'
export const LOCAL_THREAD_ID = 'local_draft_visual'
export const NATIVE_THREAD_ID = 'native-thread-visual'

type Locale = 'zh-CN' | 'en'
type Theme = 'light' | 'dark'

type SeedOptions = {
  authenticated?: boolean
  bindings?: boolean
  locale?: Locale
  theme?: Theme
  localThread?: boolean
}

const unexpectedMessages = new WeakMap<Page, string[]>()
const expectedConsoleErrors = new WeakMap<Page, RegExp[]>()

function consoleErrorsForScenario(name: string) {
  const patterns: RegExp[] = []
  if (name === 'devices-auth-error' || name === 'devices-server-error') {
    patterns.push(/failed to fetch devices:/)
  }
  if (
    name === 'devices-auth-error' ||
    name === 'devices-server-error' ||
    name === 'device-load-error' ||
    name === 'pair-failure'
  ) {
    patterns.push(/Failed to load resource: the server responded with a status of (401|409|503)/)
  }
  if (name === 'prompt-queued' || name === 'session-disconnected') {
    patterns.push(/WebSocket connection to .* failed/)
  }
  return patterns
}

export function monitorPage(page: Page) {
  const messages: string[] = []
  unexpectedMessages.set(page, messages)
  expectedConsoleErrors.set(page, [])
  page.on('pageerror', error => messages.push(`pageerror: ${error.message}`))
  page.on('console', message => {
    if (message.type() !== 'error') return
    const text = message.text()
    if (!(expectedConsoleErrors.get(page) || []).some(pattern => pattern.test(text))) {
      messages.push(`console: ${text}`)
    }
  })
}

export async function selectScenario(request: APIRequestContext, name: string) {
  const response = await request.post('http://127.0.0.1:18080/__e2e/scenario', {
    data: { name }
  })
  expect(response.ok(), await response.text()).toBe(true)
}

export async function seedApp(page: Page, options: SeedOptions = {}) {
  const authenticated = options.authenticated !== false
  const bindings = options.bindings !== false
  const locale = options.locale || 'zh-CN'
  const theme = options.theme || 'light'
  const localThread = options.localThread === true
  await page.clock.setFixedTime(FIXED_NOW_MS)
  await page.addInitScript(({ authenticated, bindings, locale, theme, localThread, fixedNow }) => {
    localStorage.clear()
    sessionStorage.clear()
    localStorage.setItem('nekonest_locale', locale)
    localStorage.setItem('nekonest_theme', theme)
    if (authenticated) {
      localStorage.setItem('nekonest_setup_done', '1')
      localStorage.setItem('nekonest_phone_secret', 'visual-admin-secret')
      localStorage.setItem('nekonest_phone_token', 'visual-phone-token')
      localStorage.setItem('nekonest_phone_id', 'visual-phone')
      if (bindings) {
        localStorage.setItem('nekonest_last_device', 'device-windows')
        localStorage.setItem('nekonest_bound_devices', JSON.stringify([
          { id: 'device-windows', name: '书房电脑', bound_at: fixedNow },
          { id: 'device-linux', name: 'Linux 构建机', bound_at: fixedNow }
        ]))
      }
    }
    if (localThread) {
      localStorage.setItem('nekonest_local_threads_v1', JSON.stringify([
        {
          id: 'local_draft_visual',
          deviceId: 'device-windows',
          agentType: 'codex',
          projectDir: 'D:\\0 code\\nekonest',
          project: 'nekonest',
          summary: '',
          createdAt: Math.floor(fixedNow / 1000),
          lastActivity: Math.floor(fixedNow / 1000)
        }
      ]))
    }
  }, { authenticated, bindings, locale, theme, localThread, fixedNow: FIXED_NOW_MS })
}

export async function openScenario(
  page: Page,
  request: APIRequestContext,
  name: string,
  path: string,
  options: SeedOptions = {}
) {
  await selectScenario(request, name)
  expectedConsoleErrors.set(page, consoleErrorsForScenario(name))
  await seedApp(page, options)
  await page.goto(path, { waitUntil: 'domcontentloaded' })
  await page.addStyleTag({
    content: [
      '*, *::before, *::after {',
      '  animation-delay: 0s !important;',
      '  animation-duration: 0s !important;',
      '  transition-delay: 0s !important;',
      '  transition-duration: 0s !important;',
      '  caret-color: transparent !important;',
      '}'
    ].join('\n')
  })
}

async function settle(page: Page) {
  await page.evaluate(async () => {
    await document.fonts.ready
    await Promise.all(Array.from(document.images).map(image => {
      if (image.complete) return Promise.resolve()
      return new Promise<void>(resolve => {
        image.addEventListener('load', () => resolve(), { once: true })
        image.addEventListener('error', () => resolve(), { once: true })
      })
    }))
    await new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve())))
  })
  await page.waitForTimeout(80)
}

export async function expectNoHorizontalOverflow(page: Page) {
  const sizes = await page.evaluate(() => ({
    viewport: document.documentElement.clientWidth,
    document: document.documentElement.scrollWidth
  }))
  expect(sizes.document).toBeLessThanOrEqual(sizes.viewport + 1)
}

export async function capture(page: Page, name: string, fullPage = true) {
  await settle(page)
  await expectNoHorizontalOverflow(page)
  if (new URL(page.url()).pathname.includes('/session/')) {
    await expect(page.locator('.input-bar textarea')).toBeVisible()
  }
  await assertPrimaryTouchTargets(page)
  await expect(page).toHaveScreenshot(name, { fullPage })
  expect(unexpectedMessages.get(page) || []).toEqual([])
}

export async function waitForConnected(page: Page) {
  await expect(page.locator('.sr-only[role="status"]')).toContainText(/通道畅通|Channel OK/)
}

export async function sendPrompt(page: Page, prompt = '视觉状态测试') {
  const input = page.locator('.input-bar textarea')
  await expect(input).toBeVisible()
  await input.fill(prompt)
  await page.locator('.send-btn').click()
  return input
}

export async function waitForComposerIdle(page: Page) {
  await expect(page.locator('.input-bar textarea')).toBeEnabled()
}

export async function assertPrimaryTouchTargets(page: Page) {
  for (const selector of ['.send-btn', '.attachment-picker', '.device-entry']) {
    const locator = page.locator(selector).filter({ visible: true }).first()
    if (await locator.count()) {
      const box = await locator.boundingBox()
      expect(box?.height || 0).toBeGreaterThanOrEqual(43)
    }
  }
}
