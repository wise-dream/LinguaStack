import { expect, test, type Page } from '@playwright/test'

// Полный цикл в браузере (ТЗ §17 через интерфейс): регистрация →
// подключение модели → загрузка книги → оглавление → перевод с живым
// прогрессом → читалка → правка → поиск → позиция чтения.

const BOOK = [
  '# Глава первая',
  '',
  'Alice was beginning to get very tired.',
  '',
  'Call `run()` before reading [the guide](https://example.com).',
  '',
  '```go',
  'fmt.Println("not translated")',
  '```',
  '',
  '# Глава вторая',
  '',
  'The Rabbit ran away.',
  '',
].join('\n')

const shots = process.env.SCREENSHOTS === '1'

async function shot(page: Page, name: string) {
  if (shots) await page.screenshot({ path: `test-results/screens/${test.info().project.name}-${name}.png`, fullPage: true })
}

test('импорт → перевод → чтение', async ({ page }, info) => {
  const email = `e2e-${info.project.name}-${Date.now()}@example.com`

  await page.goto('/register')
  await page.getByLabel('Электронная почта').fill(email)
  await page.getByLabel('Пароль').fill('correct horse battery staple')
  await shot(page, 'register')
  await page.getByRole('button', { name: 'Создать аккаунт' }).click()
  await expect(page.getByRole('heading', { name: 'Загрузите первую книгу' })).toBeVisible()
  await shot(page, 'library-empty')

  // Подключение тестовой модели.
  await page.getByRole('link', { name: 'Модели' }).click()
  await page.getByLabel('Провайдер').selectOption('fake')
  await page.getByLabel('Название').fill('Тестовая модель')
  await page.getByRole('button', { name: 'Добавить подключение' }).click()
  await expect(page.getByText('Тестовая модель', { exact: true })).toBeVisible()
  await shot(page, 'providers')

  // Загрузка книги и оглавление.
  await page.getByRole('link', { name: 'Библиотека' }).click()
  await page.getByRole('link', { name: 'Загрузить книгу' }).first().click()
  await page.locator('input[type=file]').setInputFiles({ name: 'alice.md', mimeType: 'text/markdown', buffer: Buffer.from(BOOK) })
  await expect(page.getByText('Глава вторая')).toBeVisible()
  await expect(page.getByText('2 главы')).toBeVisible()
  await shot(page, 'import')

  // Настройка и запуск перевода.
  await page.getByRole('link', { name: 'Настроить перевод' }).click()
  await expect(page.getByLabel('Модель')).toHaveValue('fake-1')
  await expect(page.getByLabel('Язык перевода')).toHaveValue('ru')
  await shot(page, 'setup')
  await page.getByRole('button', { name: 'Начать перевод' }).click()
  await expect(page).toHaveURL(/\/translations\/[0-9a-f-]+$/)

  // Живой прогресс до конца: fake отвечает с задержкой, батчи по 2.
  await expect(page.getByText('Готово', { exact: true })).toBeVisible({ timeout: 60_000 })
  await expect(page.getByText('5 из 5')).toBeVisible()
  await shot(page, 'translation-done')

  // Читалка: оригинал и перевод рядом, код не тронут, ссылка — ссылка.
  await page.getByRole('link', { name: 'Читать' }).click()
  const alice = page.locator('[data-node]', { hasText: 'Alice was beginning' })
  await expect(alice.locator('.original')).toHaveText('Alice was beginning to get very tired.')
  await expect(alice.locator('.translation')).toHaveText('RU:Alice was beginning to get very tired.')
  await expect(page.locator('pre')).toHaveText('fmt.Println("not translated")')
  await expect(page.locator('.translation a[href="https://example.com"]')).toHaveText('the guide')
  await expect(page.locator('.translation code')).toHaveText('run()')
  await shot(page, 'reader')

  // Правка фрагмента: Ctrl+Enter сохраняет.
  // Действия фрагмента появляются по касанию строки (на телефоне нет наведения).
  await alice.locator('.original').click()
  await alice.getByRole('button', { name: 'Править' }).click()
  await alice.locator('textarea').fill('Алиса начала очень уставать.')
  await alice.locator('textarea').press('Control+Enter')
  await expect(alice.locator('.translation')).toHaveText('Алиса начала очень уставать.')

  // Поиск находит правку в переводе.
  await page.getByPlaceholder('Поиск по книге').fill('уставать')
  await page.getByPlaceholder('Поиск по книге').press('Enter')
  await page.locator('.search-results button').first().click()
  await expect(alice).toHaveClass(/found/)

  // Вторая глава и возврат к ней после перезагрузки (позиция на сервере).
  if (info.project.name === 'mobile') await page.getByRole('button', { name: 'Оглавление' }).click()
  await page.locator('nav.reader-toc').getByRole('button', { name: 'Глава вторая' }).click()
  await expect(page.locator('.translation', { hasText: 'RU:The Rabbit ran away.' })).toBeVisible()
  await shot(page, 'reader-chapter2')
  const bookPath = new URL(page.url()).pathname
  await page.waitForTimeout(2_000) // позиция сохраняется с задержкой 1,5 с
  await page.goto(bookPath)
  await expect(page.locator('.translation', { hasText: 'RU:The Rabbit ran away.' })).toBeVisible()

  if (info.project.name === 'mobile') {
    // На узком экране оригинал и перевод — столбцом, без горизонтальной прокрутки.
    const row = page.locator('[data-node]', { hasText: 'The Rabbit ran away.' }).first()
    const original = await row.locator('.original').boundingBox()
    const translation = await row.locator('.translation').boundingBox()
    expect(translation!.y).toBeGreaterThan(original!.y)
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
    expect(overflow).toBeLessThanOrEqual(0)
  }
})
