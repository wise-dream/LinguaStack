import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import type { TelegramState } from '../api/types'
import { stubApi } from '../test/fakeApi'
import SettingsPage from './SettingsPage.vue'

beforeEach(() => vi.useFakeTimers())
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

const me = { id: 'u1', email: 'reader@example.com', role: 'user', created_at: '2026-10-01T00:00:00Z' }

function state(overrides: Partial<TelegramState> = {}): TelegramState {
  return { enabled: true, bot_username: 'linguastack_bot', linked: false, notify_chapters: true, notify_completed: true, notify_errors: true, ...overrides }
}

async function mountSettings(routes: Parameters<typeof stubApi>[0]) {
  const calls = stubApi({ 'GET /api/me': { body: me }, ...routes })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/:p(.*)*', component: SettingsPage, meta: { title: '' } }],
  })
  await router.push('/settings')
  const w = mount(SettingsPage, { global: { plugins: [router] } })
  await flushPromises()
  return { w, calls }
}

describe('настройки', () => {
  it('бот не настроен — так и сказано, кнопок нет', async () => {
    const { w } = await mountSettings({ 'GET /api/telegram': { body: state({ enabled: false, bot_username: undefined }) } })
    expect(w.text()).toContain('reader@example.com')
    expect(w.find('section.telegram').text()).toContain('Telegram-бот на этом сервере не настроен')
    expect(w.find('section.telegram button').exists()).toBe(false)
  })

  it('привязка: ссылка на бота и ожидание подтверждения', async () => {
    vi.setSystemTime(new Date('2026-10-19T11:00:00Z'))
    let polls = 0
    const { w, calls } = await mountSettings({
      'GET /api/telegram': () => {
        polls++
        return { body: polls < 3 ? state() : state({ linked: true, username: 'kamil', linked_at: '2026-10-19T11:00:00Z' }) }
      },
      'POST /api/telegram/link': { body: { url: 'https://t.me/linguastack_bot?start=abc', expires_at: '2026-10-19T11:15:00Z' } },
    })
    const bind = w.find('section.telegram button.primary')
    expect(bind.text()).toBe('Привязать Telegram')
    await bind.trigger('click')
    await flushPromises()
    expect(calls.some((c) => c.method === 'POST' && c.path === '/api/telegram/link')).toBe(true)
    const open = w.find('a.open-bot')
    expect(open.text()).toBe('Открыть бота')
    expect(open.attributes('href')).toBe('https://t.me/linguastack_bot?start=abc')
    expect(w.text()).toContain('нажмите «Старт»')

    await vi.advanceTimersByTimeAsync(3000)
    await vi.advanceTimersByTimeAsync(3000)
    expect(w.find('section.telegram').text()).toContain('@kamil')
    expect(w.find('[role=status]').text()).toBe('Telegram привязан')
    const before = polls
    await vi.advanceTimersByTimeAsync(9000)
    expect(polls).toBe(before) // привязан — опрос остановлен
  })

  it('ссылка устарела — опрос останавливается, можно получить новую', async () => {
    vi.setSystemTime(new Date('2026-10-19T11:00:00Z'))
    let polls = 0
    const { w } = await mountSettings({
      'GET /api/telegram': () => {
        polls++
        return { body: state() }
      },
      'POST /api/telegram/link': { body: { url: 'https://t.me/linguastack_bot?start=abc', expires_at: '2026-10-19T11:00:05Z' } },
    })
    await w.find('section.telegram button.primary').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(9000)
    expect(w.find('a.open-bot').exists()).toBe(false)
    expect(w.text()).toContain('Ссылка устарела')
    expect(w.find('section.telegram button.primary').text()).toBe('Привязать Telegram')
    const before = polls
    await vi.advanceTimersByTimeAsync(30000)
    expect(polls).toBe(before)
  })

  it('привязан: переключатели уведомлений и отвязка с подтверждением', async () => {
    const { w, calls } = await mountSettings({
      'GET /api/telegram': { body: state({ linked: true, username: 'kamil', linked_at: '2026-10-19T11:00:00Z' }) },
      'PATCH /api/telegram': (call) => ({ body: state({ linked: true, username: 'kamil', ...(call.body as object) }) }),
      'DELETE /api/telegram': { status: 204 },
    })
    const labels = w.findAll('section.telegram input[type=checkbox]').map((i) => i.element.closest('label')?.textContent?.trim())
    expect(labels).toEqual(['Глава переведена', 'Книга переведена', 'Перевод остановлен из-за ошибки'])
    await w.find('input[name=notify_chapters]').setValue(false)
    await flushPromises()
    expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ notify_chapters: false })

    const unlink = w.find('section.telegram button.danger')
    expect(unlink.text()).toBe('Отвязать Telegram')
    await unlink.trigger('click')
    expect(calls.some((c) => c.method === 'DELETE')).toBe(false)
    await unlink.trigger('click')
    await flushPromises()
    expect(calls.some((c) => c.method === 'DELETE' && c.path === '/api/telegram')).toBe(true)
    expect(w.find('section.telegram button.primary').text()).toBe('Привязать Telegram')
  })

  it('ошибка сервера показана', async () => {
    const { w } = await mountSettings({
      'GET /api/telegram': { body: state() },
      'POST /api/telegram/link': { status: 503, body: { error: 'Telegram-бот не настроен на этом сервере' } },
    })
    await w.find('section.telegram button.primary').trigger('click')
    await flushPromises()
    expect(w.find('[role=alert]').text()).toBe('Telegram-бот не настроен на этом сервере')
  })
})

describe('профиль', () => {
  const telegramOff = { 'GET /api/telegram': { body: state({ enabled: false, bot_username: undefined }) } }

  it('смена пароля: ошибка текущего пароля показана, успех — поля очищены', async () => {
    let attempt = 0
    const { w, calls } = await mountSettings({
      ...telegramOff,
      'POST /api/me/password': () =>
        ++attempt === 1 ? { status: 403, body: { error: 'Неверный текущий пароль' } } : { status: 204 },
    })
    const form = w.find('form.password-form')
    expect(form.find('button[type=submit]').text()).toBe('Сменить пароль')
    await form.find('input[name=current_password]').setValue('wrong-password-1')
    await form.find('input[name=new_password]').setValue('new-password-123')
    await form.trigger('submit')
    await flushPromises()
    expect(form.find('[role=alert]').text()).toBe('Неверный текущий пароль')

    await form.find('input[name=current_password]').setValue('right-password-1')
    await form.trigger('submit')
    await flushPromises()
    expect(calls.filter((c) => c.path === '/api/me/password').at(-1)?.body).toEqual({
      current_password: 'right-password-1', new_password: 'new-password-123',
    })
    expect(form.find('[role=alert]').exists()).toBe(false)
    expect(form.find('[role=status]').text()).toContain('Пароль изменён')
    expect((form.find('input[name=current_password]').element as HTMLInputElement).value).toBe('')
    expect((form.find('input[name=new_password]').element as HTMLInputElement).value).toBe('')
  })

  it('смена почты: новый адрес виден сразу', async () => {
    const { w, calls } = await mountSettings({
      ...telegramOff,
      'PATCH /api/me': { body: { ...me, email: 'new@example.com' } },
    })
    const form = w.find('form.email-form')
    await form.find('input[name=email]').setValue('new@example.com')
    await form.find('input[name=email_password]').setValue('right-password-1')
    await form.trigger('submit')
    await flushPromises()
    expect(calls.find((c) => c.method === 'PATCH' && c.path === '/api/me')?.body).toEqual({
      email: 'new@example.com', current_password: 'right-password-1',
    })
    expect(w.find('section.account dd').text()).toBe('new@example.com')
    expect(form.find('[role=status]').text()).toContain('Почта изменена')
  })
})
