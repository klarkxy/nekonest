import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './e2e/visual',
  testMatch: /live\.spec\.ts/,
  outputDir: 'test-results/visual-live',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 45_000,
  reporter: [['list']],
  use: {
    baseURL: process.env.NEKONEST_VISUAL_BASE_URL || 'http://127.0.0.1:5173',
    browserName: 'chromium',
    viewport: { width: 390, height: 844 },
    deviceScaleFactor: 1,
    locale: 'zh-CN',
    timezoneId: 'Asia/Shanghai',
    colorScheme: 'light',
    reducedMotion: 'reduce',
    trace: 'off',
    screenshot: 'off',
    video: 'off'
  }
})
