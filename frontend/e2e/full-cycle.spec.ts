import { readFile } from 'node:fs/promises'

import { expect, test, type Locator, type Page } from '@playwright/test'

// Полный цикл в браузере (ТЗ §17 через интерфейс): регистрация →
// подключение модели → загрузка книги → оглавление → перевод с живым
// прогрессом → экспорт и скачивание → читалка → правка → поиск → позиция
// чтения. Отдельно — импорт HTML с картинкой.

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
  // Переходы цвета у компонентов Naive — без них снимок не ловит полусостояние.
  if (shots) await page.screenshot({ path: `test-results/screens/${test.info().project.name}-${name}.png`, fullPage: true, animations: 'disabled' })
}

// Список выбора Naive UI: поле — по подписи, вариант — в выпадающем меню.
function selectField(scope: Page | Locator, label: string) {
  // Текст поля начинается с подписи, за ней — выбранное значение.
  return scope.locator('.select-field').filter({ hasText: new RegExp(`^${label}`) })
}

async function pick(page: Page, scope: Page | Locator, label: string, option: string) {
  await selectField(scope, label).locator('.n-base-selection').click()
  await page.locator('.n-base-select-menu').getByText(option, { exact: true }).click()
}

async function register(page: Page, email: string) {
  await page.goto('/register')
  await page.getByLabel('Электронная почта').fill(email)
  await page.getByLabel('Пароль').fill('correct horse battery staple')
  await shot(page, 'register')
  await page.getByRole('button', { name: 'Создать аккаунт' }).click()
  await expect(page.getByRole('heading', { name: 'Загрузите первую книгу' })).toBeVisible()
}

test('импорт → перевод → экспорт → чтение', async ({ page }, info) => {
  await register(page, `e2e-${info.project.name}-${Date.now()}@example.com`)
  await shot(page, 'library-empty')

  // Подключение тестовой модели.
  await page.getByRole('link', { name: 'Модели' }).click()
  await pick(page, page, 'Провайдер', 'Тестовый провайдер (только для сквозных тестов)')
  await page.getByLabel('Название').fill('Тестовая модель')
  await page.getByRole('button', { name: 'Добавить подключение' }).click()
  await expect(page.getByText('Тестовая модель', { exact: true })).toBeVisible()
  await shot(page, 'providers')
  // Длинное название провайдера в списке не распирает страницу вбок.
  expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(0)

  // Загрузка книги и оглавление.
  await page.getByRole('link', { name: 'Библиотека' }).click()
  await page.getByRole('link', { name: 'Загрузить книгу' }).first().click()
  await page.locator('input[type=file]').setInputFiles({ name: 'alice.md', mimeType: 'text/markdown', buffer: Buffer.from(BOOK) })
  await expect(page.getByText('Глава вторая')).toBeVisible()
  await expect(page.getByText('2 главы')).toBeVisible()
  await shot(page, 'import')

  // Настройка и запуск перевода.
  await page.getByRole('link', { name: 'Настроить перевод' }).click()
  await expect(selectField(page, 'Модель')).toContainText('Fake')
  await expect(selectField(page, 'Язык перевода')).toContainText('Русский')
  await shot(page, 'setup')
  await page.getByRole('button', { name: 'Начать перевод' }).click()
  await expect(page).toHaveURL(/\/translations\/[0-9a-f-]+$/)

  // Живой прогресс до конца: fake отвечает с задержкой, батчи по 2.
  await expect(page.getByText('Готово', { exact: true })).toBeVisible({ timeout: 60_000 })
  await expect(page.getByText('5 из 5')).toBeVisible()
  await shot(page, 'translation-done')

  // Экспорт: файл собирает worker, панель ждёт его и даёт скачать.
  const exportPanel = page.locator('section.export')
  await pick(page, exportPanel, 'Формат файла', 'Markdown')
  await exportPanel.getByRole('button', { name: 'Экспортировать' }).click()
  const md = exportPanel.locator('[data-export]').first()
  await expect(md.getByRole('link', { name: 'Скачать' })).toBeVisible({ timeout: 30_000 })
  const [mdDownload] = await Promise.all([page.waitForEvent('download'), md.getByRole('link', { name: 'Скачать' }).click()])
  expect(mdDownload.suggestedFilename()).toBe('Глава первая (ru).md')
  const mdText = await readFile(await mdDownload.path(), 'utf8')
  expect(mdText).toContain('RU:Alice was beginning to get very tired.')
  expect(mdText).toContain('fmt.Println("not translated")')

  await pick(page, exportPanel, 'Формат файла', 'EPUB')
  await exportPanel.getByText('Оригинал и перевод', { exact: true }).click()
  await exportPanel.getByRole('button', { name: 'Экспортировать' }).click()
  const epub = exportPanel.locator('[data-export]').first()
  await expect(epub).toContainText('EPUB')
  await expect(epub.getByRole('link', { name: 'Скачать' })).toBeVisible({ timeout: 30_000 })
  const [epubDownload] = await Promise.all([page.waitForEvent('download'), epub.getByRole('link', { name: 'Скачать' }).click()])
  expect(epubDownload.suggestedFilename()).toBe('Глава первая (ru).epub')
  const epubBytes = await readFile(await epubDownload.path())
  expect(epubBytes.subarray(30, 38).toString()).toBe('mimetype')
  await shot(page, 'export')

  // История экспортов — и на странице книги.
  await page.locator('.crumb a').click()
  await expect(page.locator('section.exports-history [data-export]')).toHaveCount(2)
  await shot(page, 'book-exports')
  await page.goBack()
  await expect(page.getByText('5 из 5')).toBeVisible()

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
  // Действия фрагмента доступны с клавиатуры и на узком экране (масштаб
  // 400% на компьютере даёт тот же макет).
  const edit = alice.getByRole('button', { name: 'Править' })
  await edit.focus()
  await expect(edit).toBeFocused()
  await expect(edit).toBeVisible()
  await edit.blur()
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

// Импорт HTML: главы по заголовкам, картинка из data: — ресурс книги.
const PIXEL = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=='
const PAGE = `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Web Book</title></head>
<body><nav>Menu</nav><main><h1>First</h1><p>Hello <b>world</b>.</p><p><img alt="dot" src="data:image/png;base64,${PIXEL}"></p>
<h1>Second</h1><p>Bye.</p></main></body></html>`

test('импорт HTML', async ({ page }, info) => {
  await register(page, `e2e-html-${info.project.name}-${Date.now()}@example.com`)
  await page.getByRole('link', { name: 'Загрузить книгу' }).first().click()
  await page.locator('input[type=file]').setInputFiles({ name: 'web.html', mimeType: 'text/html', buffer: Buffer.from(PAGE) })
  await expect(page.getByRole('heading', { name: 'Web Book' })).toBeVisible()
  await expect(page.getByText('2 главы')).toBeVisible()
  await expect(page.getByText('Second')).toBeVisible()
  await expect(page.getByText('Menu')).toHaveCount(0)
  await shot(page, 'import-html')

  // Книга без переводов: из читалки и из библиотеки — прямо к новому переводу.
  await page.getByRole('link', { name: 'Библиотека' }).click()
  const row = page.locator('tr', { hasText: 'Web Book' })
  await row.getByRole('link', { name: 'Читать' }).click()
  await expect(page.getByRole('link', { name: 'Перевести книгу' })).toBeVisible()
  await shot(page, 'reader-untranslated')
  expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(0)
  await page.getByRole('link', { name: 'Перевести книгу' }).click()
  await expect(page.getByRole('heading', { name: 'Новый перевод' })).toBeVisible()
  await page.getByRole('link', { name: 'Библиотека' }).click()
  await expect(row.getByRole('link', { name: 'Перевести' })).toBeVisible()
  await shot(page, 'library-book')
  await row.getByRole('link', { name: 'Перевести' }).click()
  await expect(page.getByRole('heading', { name: 'Новый перевод' })).toBeVisible()
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(0)
})

test('настройки Telegram', async ({ page }, info) => {
  const email = `e2e-tg-${info.project.name}-${Date.now()}@example.com`
  await register(page, email)
  await page.getByRole('link', { name: 'Настройки' }).click()
  await expect(page.getByRole('heading', { name: 'Настройки', level: 1 })).toBeVisible()
  await expect(page.locator('main').getByText(email)).toBeVisible()
  await page.getByRole('button', { name: 'Привязать Telegram' }).click()
  const open = page.getByRole('link', { name: 'Открыть бота' })
  await expect(open).toHaveAttribute('href', /^https:\/\/t\.me\/linguastack_e2e_bot\?start=[A-Za-z0-9_-]{43}$/)
  await shot(page, 'settings-telegram')
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(0)
})

test('смена пароля и «Забыли пароль?»', async ({ page }, info) => {
  const email = `e2e-pw-${info.project.name}-${Date.now()}@example.com`
  const next = 'brand new password 42'
  await register(page, email)
  await page.getByRole('link', { name: 'Настройки' }).click()

  const form = page.locator('form.password-form')
  await form.getByLabel('Текущий пароль').fill('not my password at all')
  await form.getByLabel('Новый пароль').fill(next)
  await form.getByRole('button', { name: 'Сменить пароль' }).click()
  await expect(form.getByRole('alert')).toHaveText('Неверный текущий пароль')
  await form.getByLabel('Текущий пароль').fill('correct horse battery staple')
  await form.getByRole('button', { name: 'Сменить пароль' }).click()
  await expect(form.getByRole('status')).toContainText('Пароль изменён')
  await shot(page, 'settings-password')
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(0)

  // Выход и вход: старый пароль больше не подходит, новый — да.
  await page.getByRole('button', { name: 'Выйти' }).click()
  await expect(page.getByRole('heading', { name: 'Вход' })).toBeVisible()
  await page.getByLabel('Электронная почта').fill(email)
  await page.getByLabel('Пароль').fill('correct horse battery staple')
  await page.getByRole('button', { name: 'Войти' }).click()
  await expect(page.getByRole('alert')).toHaveText('Неверный адрес или пароль')
  await page.getByLabel('Пароль').fill(next)
  await page.getByRole('button', { name: 'Войти' }).click()
  await expect(page.getByRole('heading', { name: 'Загрузите первую книгу' })).toBeVisible()

  // «Забыли пароль?» отвечает одинаково — есть аккаунт или нет.
  await page.getByRole('button', { name: 'Выйти' }).click()
  await page.getByRole('link', { name: 'Забыли пароль?' }).click()
  await expect(page.getByRole('heading', { name: 'Восстановление доступа' })).toBeVisible()
  await page.getByLabel('Электронная почта').fill(email)
  await page.getByRole('button', { name: 'Отправить ссылку' }).click()
  await expect(page.getByRole('status')).toContainText('Если такой аккаунт есть')
  await shot(page, 'forgot-password')
})

test('администрирование', async ({ page }) => {
  // Администратора заводит e2e/run-stack.sh командой api -set-role.
  await page.goto('/login')
  await page.getByLabel('Электронная почта').fill('admin@e2e.example')
  await page.getByLabel('Пароль').fill('e2e admin password')
  await page.getByRole('button', { name: 'Войти' }).click()
  await page.getByRole('link', { name: 'Администрирование' }).click()
  await expect(page.getByRole('heading', { name: 'Обзор' })).toBeVisible()
  await expect(page.locator('section.overview')).toContainText('Ollama сервера')
  const self = page.locator('tbody', { hasText: 'admin@e2e.example' })
  await expect(self).toContainText('это вы')
  await self.getByRole('button', { name: 'Квоты' }).click()
  await expect(self.locator('form.quota-form')).toBeVisible()
  await shot(page, 'admin')
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(0)
})
