import { defineConfig, devices } from '@playwright/test'

// Сквозные тесты гоняют настоящие api и worker с тестовым провайдером
// (e2e/run-stack.sh, задача 12).
export default defineConfig({
  testDir: 'e2e',
  timeout: 120_000,
  use: { baseURL: 'http://127.0.0.1:8090', trace: 'retain-on-failure' },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
})
