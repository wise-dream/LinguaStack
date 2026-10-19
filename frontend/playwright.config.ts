import { defineConfig, devices } from '@playwright/test'

// Сквозные тесты гоняют собранное приложение и настоящие api и worker с
// тестовым провайдером fake (e2e/run-stack.sh).
export default defineConfig({
  testDir: 'e2e',
  timeout: 120_000,
  expect: { timeout: 15_000 },
  fullyParallel: false,
  workers: 1,
  reporter: process.env.CI ? 'github' : 'list',
  use: { baseURL: 'http://127.0.0.1:8090', trace: 'retain-on-failure' },
  webServer: {
    command: 'bash e2e/run-stack.sh',
    url: 'http://127.0.0.1:8090/api/health',
    timeout: 240_000,
    reuseExistingServer: false,
    stdout: 'pipe',
    stderr: 'pipe',
  },
  projects: [
    { name: 'desktop', use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 } } },
    { name: 'mobile', use: { ...devices['Desktop Chrome'], viewport: { width: 390, height: 844 }, isMobile: false, hasTouch: true } },
  ],
})
