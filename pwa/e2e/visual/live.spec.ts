import { expect, test } from '@playwright/test'

const deviceId = (process.env.NEKONEST_VISUAL_DEVICE_ID || '').trim()
const sessionId = (process.env.NEKONEST_VISUAL_SESSION_ID || '').trim()
const sendText = (process.env.NEKONEST_VISUAL_SEND_PROMPT || '').trim()

test.beforeEach(async ({ page }) => {
  const credentials = {
    adminSecret: process.env.NEKONEST_VISUAL_ADMIN_SECRET || '',
    phoneToken: process.env.NEKONEST_VISUAL_PHONE_TOKEN || '',
    phoneId: process.env.NEKONEST_VISUAL_PHONE_ID || ''
  }
  await page.addInitScript(({ adminSecret, phoneToken, phoneId }) => {
    localStorage.setItem('nekonest_setup_done', '1')
    localStorage.setItem('nekonest_locale', 'zh-CN')
    localStorage.setItem('nekonest_theme', 'light')
    if (adminSecret) localStorage.setItem('nekonest_phone_secret', adminSecret)
    if (phoneToken) localStorage.setItem('nekonest_phone_token', phoneToken)
    if (phoneId) localStorage.setItem('nekonest_phone_id', phoneId)
  }, credentials)
})

test('capture real local stack without mutating it by default', async ({ page }, testInfo) => {
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  await expect(page.locator('body')).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('devices.png'), fullPage: true })

  if (!deviceId) return
  await page.goto(`/device/${encodeURIComponent(deviceId)}`, { waitUntil: 'domcontentloaded' })
  await page.screenshot({ path: testInfo.outputPath('device.png'), fullPage: true })

  if (!sessionId) return
  await page.goto(
    `/device/${encodeURIComponent(deviceId)}/session/${encodeURIComponent(sessionId)}`,
    { waitUntil: 'domcontentloaded' }
  )
  await expect(page.getByRole('log')).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('session.png'), fullPage: false })

  if (!sendText) return
  const input = page.getByRole('textbox', { name: /消息输入|Message input/ })
  await input.fill(sendText)
  await page.getByRole('button', { name: /发送|Send/ }).click()
  await expect(input).toHaveValue('')
  await page.screenshot({ path: testInfo.outputPath('session-after-send.png'), fullPage: false })
})
