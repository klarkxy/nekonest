import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './e2e/visual',
  testMatch: /visual\.spec\.ts/,
  globalSetup: './e2e/visual/global-setup.ts',
  outputDir: 'test-results/visual',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 30_000,
  expect: {
    timeout: 8_000,
    toHaveScreenshot: {
      animations: 'disabled',
      caret: 'hide',
      threshold: 0.15,
      maxDiffPixelRatio: 0.001
    }
  },
  reporter: [
    ['list'],
    ['html', { outputFolder: 'playwright-report', open: 'never' }]
  ],
  use: {
    baseURL: 'http://127.0.0.1:5173',
    browserName: 'chromium',
    viewport: { width: 390, height: 844 },
    deviceScaleFactor: 1,
    locale: 'zh-CN',
    timezoneId: 'Asia/Shanghai',
    colorScheme: 'light',
    reducedMotion: 'reduce',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure'
  },
  projects: [
    {
      name: 'chromium-win'
    }
  ]
})
