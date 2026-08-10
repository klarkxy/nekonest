import { expect, test } from '@playwright/test'
import {
  LOCAL_THREAD_ID,
  MAIN_DEVICE_ID,
  MAIN_SESSION_ID,
  NATIVE_THREAD_ID,
  assertPrimaryTouchTargets,
  capture,
  monitorPage,
  openScenario,
  sendPrompt,
  waitForComposerIdle,
  waitForConnected
} from './fixtures'

const devicePath = `/device/${MAIN_DEVICE_ID}`
const sessionPath = `${devicePath}/session/${MAIN_SESSION_ID}`
const localThreadPath = `${devicePath}/session/${LOCAL_THREAD_ID}`

test.beforeEach(async ({ page }) => {
  monitorPage(page)
})

test.describe('390px primary visual matrix', () => {
  test('setup fresh', async ({ page, request }) => {
    await openScenario(page, request, 'setup-fresh', '/setup', { authenticated: false })
    await expect(page.getByRole('heading', { name: '猫娘窝' })).toBeVisible()
    await expect(page.getByRole('button', { name: '进入猫娘乐园' })).toBeDisabled()
    await capture(page, 'setup-fresh.png')
  })

  test('setup whitespace stays invalid', async ({ page, request }) => {
    await openScenario(page, request, 'setup-whitespace', '/setup', { authenticated: false })
    await page.locator('#phone-secret').fill('   ')
    await expect(page.getByRole('button', { name: '进入猫娘乐园' })).toBeDisabled()
    await capture(page, 'setup-whitespace-invalid.png')
  })

  test('pair initial', async ({ page, request }) => {
    await openScenario(page, request, 'pair-initial', '/pair')
    await expect(page.getByRole('heading', { name: '配对电脑' })).toBeVisible()
    await capture(page, 'pair-initial.png')
  })

  test('pair fingerprint confirmation', async ({ page, request }) => {
    await openScenario(page, request, 'pair-fingerprint', '/pair')
    await page.getByLabel('QR 载荷（可选，推荐）').fill(JSON.stringify({
      code: 'a1b2c3',
      device_id: MAIN_DEVICE_ID,
      identity_fingerprint: '0123456789abcdef0123456789abcdef0123456789abcdef'
    }))
    await expect(page.getByText('0123456789abcdef0123456789abcdef')).toBeVisible()
    await capture(page, 'pair-fingerprint.png')
  })

  test('pair fingerprint failure', async ({ page, request }) => {
    await openScenario(page, request, 'pair-failure', '/pair')
    await page.getByLabel('配对码').fill('a1b2c3')
    await page.getByRole('button', { name: '完成配对' }).click()
    await expect(page.getByRole('alert')).toContainText('主机指纹不一致')
    await capture(page, 'pair-failure.png')
  })

  test('devices loading', async ({ page, request }) => {
    await openScenario(page, request, 'devices-loading', '/', { bindings: false })
    await expect(page.getByText('正在读取电脑列表')).toBeAttached()
    await capture(page, 'devices-loading.png')
  })

  test('devices empty', async ({ page, request }) => {
    await openScenario(page, request, 'devices-empty', '/', { bindings: false })
    await expect(page.getByText('还没有配对的电脑')).toBeVisible()
    await capture(page, 'devices-empty.png')
  })

  test('devices mixed online and offline', async ({ page, request }) => {
    await openScenario(page, request, 'devices-mixed', '/')
    await expect(page.getByText('书房电脑')).toBeVisible()
    await expect(page.getByText('Linux 构建机')).toBeVisible()
    await assertPrimaryTouchTargets(page)
    await capture(page, 'devices-mixed.png')
  })

  test('devices authentication error', async ({ page, request }) => {
    await openScenario(page, request, 'devices-auth-error', '/', { bindings: false })
    await expect(page.getByRole('alert')).toContainText('进不了猫娘乐园')
    await capture(page, 'devices-auth-error.png')
  })

  test('devices server error', async ({ page, request }) => {
    await openScenario(page, request, 'devices-server-error', '/', { bindings: false })
    await expect(page.getByText('电脑列表没读到')).toBeVisible()
    await capture(page, 'devices-server-error.png')
  })

  test('device full four-agent tree', async ({ page, request }) => {
    await openScenario(page, request, 'device-full', devicePath)
    await expect(page.getByText('完善 NekoNest 本地截图回归')).toBeVisible()
    await expect(page.getByRole('button', { name: /Grok Build/ })).toBeVisible()
    await capture(page, 'device-full-tree.png')
  })

  test('only populated harness groups render while the project menu starts a missing harness', async ({ page, request }) => {
    await openScenario(page, request, 'device-full', devicePath)
    const project = page.locator('.project-group').filter({ hasText: 'nekonest' }).first()
    const projectHeader = project.locator('.project-header')
    const codexStart = project.getByRole('button', { name: '使用 Codex 在“nekonest”里新建线团' })
    const startMenu = project.locator('.project-start-menu')

    await expect(codexStart).toBeVisible()
    await expect(project.locator('.agent-group').filter({ hasText: 'Claude Code' }).locator('.agent-header')).toBeVisible()
    await expect(project.locator('.agent-group').filter({ hasText: 'Kimi CLI' })).toHaveCount(0)
    await expect(startMenu).toBeVisible()
    await startMenu.locator('summary').click()
    await expect(startMenu.getByRole('button', { name: 'Kimi CLI' })).toBeVisible()

    await page.getByLabel('猫娘', { exact: true }).selectOption('kimi_cli')
    await expect(page.locator('.project-group')).toHaveCount(1)
    await expect(page.locator('.project-group').filter({ hasText: 'nekonest' })).toHaveCount(0)
    await expect(page.locator('.project-group').filter({ hasText: 'mobile-demo' })).toContainText('Kimi CLI')
    await page.getByLabel('猫娘', { exact: true }).selectOption('')

    await projectHeader.click()
    await expect(projectHeader).toHaveAttribute('aria-expanded', 'false')
    await expect(codexStart).toBeHidden()
    await startMenu.locator('summary').click()
    const kimiOption = startMenu.getByRole('button', { name: 'Kimi CLI' })
    await expect(kimiOption).toBeVisible()
    // A clipped popup can still have a non-empty visible sliver. Confirm that
    // the option's center is actually hit-testable below the collapsed card.
    expect(await kimiOption.evaluate(element => {
      const rect = element.getBoundingClientRect()
      const hit = document.elementFromPoint(rect.left + rect.width / 2, rect.top + rect.height / 2)
      return hit === element || element.contains(hit)
    })).toBe(true)
    await projectHeader.click()
    await expect(projectHeader).toHaveAttribute('aria-expanded', 'true')
    await expect(codexStart).toBeVisible()

    await codexStart.click()
    await expect(page).toHaveURL(/\/session\/local_draft_/)
  })

  test('device offline', async ({ page, request }) => {
    await openScenario(page, request, 'device-offline', devicePath)
    await expect(page.getByText('这台电脑现在没有回应。')).toBeVisible()
    await capture(page, 'device-offline.png')
  })

  test('device empty', async ({ page, request }) => {
    await openScenario(page, request, 'device-empty', devicePath)
    await expect(page.getByText('还没有可续写的线团。', { exact: true }).first()).toBeVisible()
    await capture(page, 'device-empty.png')
  })

  test('device session load error', async ({ page, request }) => {
    await openScenario(page, request, 'device-load-error', devicePath)
    await expect(page.getByRole('alert')).toContainText('线团清单没读到')
    await capture(page, 'device-load-error.png')
  })

  test('device search no result', async ({ page, request }) => {
    await openScenario(page, request, 'device-filter', devicePath)
    await expect(page.getByText('完善 NekoNest 本地截图回归')).toBeVisible()
    await page.getByPlaceholder('搜索线团或目录…').fill('不存在的线团')
    await expect(page.getByText('没有匹配的线团或目录。试试别的关键词。')).toBeVisible()
    await capture(page, 'device-search-empty.png')
  })

  test('device archived and collapsed states', async ({ page, request }) => {
    await openScenario(page, request, 'device-archived', devicePath)
    await expect(page.getByText('完善 NekoNest 本地截图回归')).toBeVisible()
    const project = page.locator('.project-group').filter({ hasText: 'nekonest' }).first()
    await project.getByRole('button', { name: '收起项目 nekonest 下的所有线团' }).click()
    await page.getByText('显示已收起').click()
    const archivedProject = page.locator('.project-group').filter({ hasText: 'nekonest' }).first()
    await expect(archivedProject.locator('.session-item.archived')).toHaveCount(2)
    await expect(
      archivedProject.getByRole('button', { name: '放回项目 nekonest 下的所有线团' })
    ).toBeVisible()
    await capture(page, 'device-archived.png')
    const projectHeader = archivedProject.locator('.project-header')
    const projectAction = archivedProject.getByRole('button', {
      name: '放回项目 nekonest 下的所有线团'
    })
    await projectHeader.click()
    await expect(projectHeader).toHaveAttribute('aria-expanded', 'false')
    await expect(projectAction).toBeVisible()
    await expect(archivedProject.locator('.project-body')).toBeHidden()
    await capture(page, 'device-collapsed.png')
  })

  test('session rich history top and bottom', async ({ page, request }) => {
    await openScenario(page, request, 'session-rich', sessionPath)
    await waitForConnected(page)
    await expect(page.getByText('390 × 844')).toBeVisible()
    const log = page.getByRole('log')
    const scrollRange = await log.evaluate(element => element.scrollHeight - element.clientHeight)
    expect(scrollRange).toBeGreaterThan(0)
    await log.evaluate(element => { element.scrollTop = 0 })
    await expect.poll(() => log.evaluate(element => element.scrollTop)).toBe(0)
    await capture(page, 'session-rich-top.png', false)
    await log.evaluate(element => { element.scrollTop = element.scrollHeight })
    await expect.poll(() => log.evaluate(element => element.scrollTop)).toBeGreaterThan(0)
    await assertPrimaryTouchTargets(page)
    await capture(page, 'session-rich-bottom.png', false)
  })

  test('prompt queued while disconnected', async ({ page, request }) => {
    await openScenario(page, request, 'prompt-queued', sessionPath)
    await expect(page.locator('.ws-pill.disconnected')).toBeVisible()
    await sendPrompt(page)
    await expect(page.getByText('排队中', { exact: true })).toBeVisible()
    await waitForComposerIdle(page)
    await capture(page, 'prompt-queued.png', false)
  })

  test('prompt sending', async ({ page, request }) => {
    await openScenario(page, request, 'prompt-sending', sessionPath)
    await waitForConnected(page)
    await sendPrompt(page)
    await expect(page.getByText('发送中', { exact: true })).toBeVisible()
    await waitForComposerIdle(page)
    await capture(page, 'prompt-sending.png', false)
  })

  test('prompt accepted', async ({ page, request }) => {
    await openScenario(page, request, 'prompt-accepted', sessionPath)
    await waitForConnected(page)
    await sendPrompt(page)
    await expect(page.getByText('已接受', { exact: true })).toBeVisible()
    await waitForComposerIdle(page)
    await capture(page, 'prompt-accepted.png', false)
  })

  test('prompt committed and answered', async ({ page, request }) => {
    await openScenario(page, request, 'prompt-committed', sessionPath)
    await waitForConnected(page)
    const input = await sendPrompt(page)
    await expect(page.getByText('已收到，视觉回归状态正常。')).toBeVisible()
    await expect(input).toHaveValue('')
    await waitForComposerIdle(page)
    await capture(page, 'prompt-committed.png', false)
  })

  test('prompt failed with retry', async ({ page, request }) => {
    await openScenario(page, request, 'prompt-failed', sessionPath)
    await waitForConnected(page)
    await sendPrompt(page)
    await expect(page.getByText('Agent 暂时不可用', { exact: true }).first()).toBeVisible()
    await expect(page.getByRole('button', { name: '重试' })).toBeVisible()
    await waitForComposerIdle(page)
    await capture(page, 'prompt-failed.png', false)
  })

  test('prompt not seen', async ({ page, request }) => {
    await openScenario(page, request, 'prompt-not-seen', sessionPath)
    await waitForConnected(page)
    await sendPrompt(page)
    await expect(page.getByText('未见', { exact: true })).toBeVisible()
    await waitForComposerIdle(page)
    await capture(page, 'prompt-not-seen.png', false)
  })

  test('session streaming', async ({ page, request }) => {
    await openScenario(page, request, 'session-streaming', sessionPath)
    await expect(page.getByText('正在回复', { exact: true })).toBeVisible()
    await expect(page.getByText('正在补充深色模式和窄屏截图…')).toBeVisible()
    await capture(page, 'session-streaming.png', false)
  })

  test('session approval', async ({ page, request }) => {
    await openScenario(page, request, 'session-approval', sessionPath)
    await expect(page.getByText('需要审批', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: '批准' })).toBeVisible()
    await expect(page.getByText('请先批准或拒绝上方请求，再继续发送。')).toBeVisible()
    await capture(page, 'session-approval.png', false)
  })

  test('session disconnected', async ({ page, request }) => {
    await openScenario(page, request, 'session-disconnected', sessionPath)
    await expect(page.locator('.ws-pill.disconnected')).toBeVisible()
    await capture(page, 'session-disconnected.png', false)
  })

  test('session unavailable capabilities', async ({ page, request }) => {
    await openScenario(page, request, 'session-unavailable', sessionPath)
    await waitForConnected(page)
    await expect(page.getByRole('button', { name: '中断当前任务' })).toHaveCount(0)
    await capture(page, 'session-unavailable.png', false)
  })

  test('session pending attachment', async ({ page, request }) => {
    await openScenario(page, request, 'session-attachment', sessionPath)
    await waitForConnected(page)
    const chooserPromise = page.waitForEvent('filechooser')
    await page.getByRole('button', { name: '添加附件' }).click()
    const chooser = await chooserPromise
    await chooser.setFiles({
      name: 'visual-check.txt',
      mimeType: 'text/plain',
      buffer: Buffer.from('deterministic visual attachment')
    })
    await expect(page.getByText('visual-check.txt')).toBeVisible()
    await capture(page, 'session-pending-attachment.png', false)
  })

  test('local draft attachment picker opens from the visible plus control', async ({ page, request }) => {
    await openScenario(page, request, 'session-attachment', localThreadPath, { localThread: true })
    await waitForConnected(page)
    const chooserPromise = page.waitForEvent('filechooser')
    await page.getByRole('button', { name: '添加附件' }).click()
    const chooser = await chooserPromise
    await chooser.setFiles({
      name: 'draft-attachment.txt',
      mimeType: 'text/plain',
      buffer: Buffer.from('local draft attachment')
    })
    await expect(page.locator('.pending-chip')).toBeVisible()
  })

  test('composer follows an overlay-keyboard visual viewport', async ({ page, request }) => {
    await openScenario(page, request, 'session-attachment', localThreadPath, { localThread: true })
    const visibleHeight = 430
    await page.evaluate((height) => {
      if (!window.visualViewport) throw new Error('VisualViewport unavailable')
      Object.defineProperty(window.visualViewport, 'height', {
        configurable: true,
        value: height
      })
      window.visualViewport.dispatchEvent(new Event('resize'))
    }, visibleHeight)

    await expect.poll(() => page.locator('.input-bar').evaluate(
      element => Math.round(element.getBoundingClientRect().bottom)
    )).toBeLessThanOrEqual(visibleHeight)
    await expect(page.locator('.input-bar textarea')).toBeVisible()
  })

  test('thread starting', async ({ page, request }) => {
    await openScenario(page, request, 'thread-starting', localThreadPath, { localThread: true })
    await waitForConnected(page)
    const input = await sendPrompt(page, 'ping ×19')
    await expect(page).toHaveURL(new RegExp(`${LOCAL_THREAD_ID}$`))
    await expect(input).toHaveValue('ping ×19')
    await expect(page.locator('.empty-title')).toHaveText('正在创建 Codex 线团…')
    await capture(page, 'thread-starting.png', false)
  })

  test('thread failed', async ({ page, request }) => {
    await openScenario(page, request, 'thread-failed', localThreadPath, { localThread: true })
    await waitForConnected(page)
    await sendPrompt(page, 'ping ×19')
    await expect(page.getByRole('alert')).toContainText('Codex app-server 当前不可用')
    await capture(page, 'thread-failed.png', false)
  })

  test('thread indeterminate with native id', async ({ page, request }) => {
    await openScenario(page, request, 'thread-indeterminate', localThreadPath, { localThread: true })
    await waitForConnected(page)
    // Pre-seed an unsent draft for the native session id that will appear in the
    // indeterminate payload. Ownership was never confirmed, so this draft must survive.
    const nativeDraftText = 'unrelated unsent for native'
    await page.evaluate(({ deviceId, nativeId, text }) => {
      const key = `${deviceId}::${nativeId}`
      localStorage.setItem('nekonest_input_drafts', JSON.stringify({
        [key]: { text, attachments: [], updatedAt: Date.now() }
      }))
    }, { deviceId: MAIN_DEVICE_ID, nativeId: NATIVE_THREAD_ID, text: nativeDraftText })
    const input = await sendPrompt(page, 'ping ×19')
    // Stay on the local draft route — never treat indeterminate as owned navigation.
    await expect(page).toHaveURL(new RegExp(`${LOCAL_THREAD_ID}$`))
    await expect(page).not.toHaveURL(new RegExp(`${NATIVE_THREAD_ID}$`))
    // No synthesized owned-thread bubble / inbox.
    await expect(page.locator('.message-bubble')).toHaveCount(0)
    await expect(page.locator('.empty-title')).toHaveText('结果不确定')
    await expect(page.locator('.empty-hint')).toContainText('等待本机发现与对账')
    await expect(page.getByRole('button', { name: '已禁用创建' })).toBeDisabled()
    await expect(page.locator('#session-attachment-input')).toBeDisabled()
    // Prompt and phone-local draft stay available for inspection, but the
    // disabled terminal state prevents any automatic replay.
    await expect(input).toHaveValue('ping ×19')
    await expect.poll(async () => page.evaluate((draftId) => {
      const raw = localStorage.getItem('nekonest_local_threads_v1')
      if (!raw) return false
      try {
        const list = JSON.parse(raw) as Array<{ id?: string }>
        return list.some(item => item.id === draftId)
      } catch {
        return false
      }
    }, LOCAL_THREAD_ID)).toBe(true)
    await expect.poll(async () => page.evaluate(({ deviceId, localId, nativeId, nativeText }) => {
      const raw = localStorage.getItem('nekonest_input_drafts')
      if (!raw) return false
      try {
        const map = JSON.parse(raw) as Record<string, { text?: string }>
        const localKey = `${deviceId}::${localId}`
        const nativeKey = `${deviceId}::${nativeId}`
        // Both drafts survive, but neither is treated as proof of native ownership.
        const local = map[localKey]
        const native = map[nativeKey]
        return !!local && local.text === 'ping ×19' && !!native && native.text === nativeText
      } catch {
        return false
      }
    }, {
      deviceId: MAIN_DEVICE_ID,
      localId: LOCAL_THREAD_ID,
      nativeId: NATIVE_THREAD_ID,
      nativeText: nativeDraftText
    })).toBe(true)
    await capture(page, 'thread-indeterminate.png', false)
  })

  test('thread owned keeps first user bubble', async ({ page, request }) => {
    await openScenario(page, request, 'thread-owned', localThreadPath, { localThread: true })
    await waitForConnected(page)
    const input = await sendPrompt(page, 'ping ×19')
    await expect(page).toHaveURL(new RegExp(`${NATIVE_THREAD_ID}$`))
    await expect(page.getByText('ping ×19', { exact: true })).toBeVisible()
    await expect(page.getByText('pong ×19 🏓', { exact: true })).toBeVisible()
    await expect(input).toHaveValue('')
    await capture(page, 'thread-owned-first-message.png', false)
  })
})

for (const sample of [
  { label: 'narrow', width: 360, height: 800 },
  { label: 'desktop', width: 1280, height: 800 }
]) {
  test.describe(`${sample.label} responsive samples`, () => {
    test.beforeEach(async ({ page }) => {
      await page.setViewportSize({ width: sample.width, height: sample.height })
    })

    test('devices', async ({ page, request }) => {
      await openScenario(page, request, 'devices-mixed', '/')
      await expect(page.getByText('书房电脑')).toBeVisible()
      await capture(page, `${sample.label}-devices.png`)
    })

    test('device tree', async ({ page, request }) => {
      await openScenario(page, request, 'device-full', devicePath)
      await expect(page.getByText('完善 NekoNest 本地截图回归')).toBeVisible()
      await capture(page, `${sample.label}-device-tree.png`)
    })

    test('session', async ({ page, request }) => {
      await openScenario(page, request, 'session-rich', sessionPath)
      await expect(page.getByText('390 × 844')).toBeVisible()
      await capture(page, `${sample.label}-session.png`, false)
    })

    test('first message regression', async ({ page, request }) => {
      await openScenario(page, request, 'thread-owned', localThreadPath, { localThread: true })
      await waitForConnected(page)
      const input = await sendPrompt(page, 'ping ×19')
      await expect(page.getByText('ping ×19', { exact: true })).toBeVisible()
      await expect(page.getByText('pong ×19 🏓', { exact: true })).toBeVisible()
      await expect(input).toHaveValue('')
      await capture(page, `${sample.label}-thread-owned.png`, false)
    })
  })
}

test.describe('theme and locale samples', () => {
  test('dark device tree', async ({ page, request }) => {
    await page.emulateMedia({ colorScheme: 'dark', reducedMotion: 'reduce' })
    await openScenario(page, request, 'device-full', devicePath, { theme: 'dark' })
    await expect(page.getByText('完善 NekoNest 本地截图回归')).toBeVisible()
    await capture(page, 'dark-device-tree.png')
  })

  test('dark session', async ({ page, request }) => {
    await page.emulateMedia({ colorScheme: 'dark', reducedMotion: 'reduce' })
    await openScenario(page, request, 'session-rich', sessionPath, { theme: 'dark' })
    await expect(page.getByText('390 × 844')).toBeVisible()
    await capture(page, 'dark-session.png', false)
  })

  test('English setup', async ({ page, request }) => {
    await openScenario(page, request, 'setup-fresh', '/setup', {
      authenticated: false,
      locale: 'en'
    })
    await expect(page.getByRole('button', { name: 'Enter nest' })).toBeDisabled()
    await capture(page, 'english-setup.png')
  })

  test('English device tree', async ({ page, request }) => {
    await openScenario(page, request, 'device-full', devicePath, { locale: 'en' })
    await expect(page.getByPlaceholder('Search threads or folders…')).toBeVisible()
    await capture(page, 'english-device-tree.png')
  })

  test('English session', async ({ page, request }) => {
    await openScenario(page, request, 'session-rich', sessionPath, { locale: 'en' })
    await expect(page.getByRole('textbox', { name: 'Message the agent…' })).toBeVisible()
    await capture(page, 'english-session.png', false)
  })
})
