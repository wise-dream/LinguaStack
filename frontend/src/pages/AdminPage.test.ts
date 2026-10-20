import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import type { AdminOverview, AdminUser, AdminUsers } from '../api/types'
import { stubApi } from '../test/fakeApi'
import { SESSION, createSession } from '../session'
import AdminPage from './AdminPage.vue'

afterEach(() => vi.unstubAllGlobals())

/** Текст без неразрывных пробелов — их ставит formatBytes. */
const plain = (s: string) => s.replace(/\s+/g, ' ')

const admin = { id: 'a1', email: 'admin@example.com', role: 'admin' as const, created_at: '2026-10-01T00:00:00Z' }

const overview: AdminOverview = {
  metrics: {
    users: 12, books: 34,
    projects: { running: 2, queued: 3, paused: 1, failed: 1, completed: 20, cancelled: 0, draft: 0, completed_with_warnings: 4 },
    tasks: { pending: 3, running: 2, completed: 40, failed: 1, cancelled: 0 },
    exports: { pending: 0, running: 1, completed: 9, failed: 0 },
    notifications: { pending: 5, sent: 100, skipped: 2, failed: 0 },
    cost_usd: 1.5, input_tokens: 120000, output_tokens: 98000,
  },
  providers: [{ id: 'ollama', base_url: 'http://ollama:11434', available: true, models: ['qwen3:8b', 'gemma3:4b'] }],
}

function user(overrides: Partial<AdminUser> = {}): AdminUser {
  return {
    id: 'u1', email: 'reader@example.com', role: 'user', is_active: true, created_at: '2026-10-02T00:00:00Z',
    last_seen_at: '2026-10-19T08:00:00Z', books: 3, translations: 5, storage_bytes: 5 * 1024 * 1024,
    max_active_projects: null, max_concurrent_tasks: null, storage_quota_bytes: null, ...overrides,
  }
}

function users(list: AdminUser[]): AdminUsers {
  return { users: list, defaults: { max_active_projects: 20, max_concurrent_tasks: 3, storage_quota_bytes: 2 * 1024 ** 3 } }
}

async function mountAdmin(routes: Parameters<typeof stubApi>[0]) {
  const calls = stubApi(routes)
  const session = createSession({
    me: async () => admin, login: async () => admin, register: async () => admin, logout: async () => undefined,
  })
  await session.load()
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:p(.*)*', component: AdminPage, meta: { title: '' } }] })
  await router.push('/admin')
  const w = mount(AdminPage, { global: { plugins: [router], provide: { [SESSION as symbol]: session } } })
  await flushPromises()
  return { w, calls }
}

const base = {
  'GET /api/admin/overview': { body: overview },
  'GET /api/admin/users': { body: users([user(), user({ id: 'a1', email: 'admin@example.com', role: 'admin' })]) },
}

describe('админ-панель', () => {
  it('обзор: счётчики и Ollama сервера', async () => {
    const { w } = await mountAdmin(base)
    const o = w.find('section.overview')
    expect(o.text()).toContain('12')
    expect(o.text()).toContain('34')
    expect(o.text()).toMatch(/Идут\s*2/)
    expect(o.text()).toMatch(/В очереди\s*3/)
    expect(o.text()).toContain('qwen3:8b')
    expect(o.text()).toContain('доступен')
  })

  it('Ollama недоступен — так и сказано, с причиной', async () => {
    const { w } = await mountAdmin({
      ...base,
      'GET /api/admin/overview': {
        body: { ...overview, providers: [{ id: 'ollama', base_url: 'http://ollama:11434', available: false, detail: 'connection refused', models: [] }] },
      },
    })
    expect(w.find('section.overview').text()).toContain('недоступен')
    expect(w.find('section.overview').text()).toContain('connection refused')
  })

  it('таблица пользователей: почта, роль, объём из квоты, счётчики', async () => {
    const { w } = await mountAdmin(base)
    const row = w.find('[data-user="u1"]')
    expect(row.text()).toContain('reader@example.com')
    expect(plain(row.text())).toContain('5 МБ из 2 ГБ')
    expect(row.text()).toMatch(/3\s*книг/)
    expect(row.text()).toMatch(/5\s*переводов/)
    expect(w.find('[data-user="a1"]').text()).toContain('администратор')
  })

  it('отключение — через подтверждение', async () => {
    const { w, calls } = await mountAdmin({
      ...base,
      'PATCH /api/admin/users/u1': { body: user({ is_active: false }) },
    })
    const off = w.find('[data-user="u1"] button.danger')
    expect(off.text()).toBe('Отключить')
    await off.trigger('click')
    expect(calls.some((c) => c.method === 'PATCH')).toBe(false)
    expect(off.text()).toBe('Точно отключить?')
    await off.trigger('click')
    await flushPromises()
    const patch = calls.find((c) => c.method === 'PATCH')
    expect(patch?.body).toEqual({ is_active: false })
    expect(w.find('[data-user="u1"]').text()).toContain('отключён')
    expect(w.find('[data-user="u1"]').text()).toContain('Включить')
  })

  it('себя не отключить и не разжаловать', async () => {
    const { w } = await mountAdmin(base)
    const self = w.find('[data-user="a1"]')
    expect(self.text()).toContain('это вы')
    expect(self.find('button.danger').exists()).toBe(false)
    expect(self.find('button.role').exists()).toBe(false)
  })

  it('квоты: пустое поле — по умолчанию, хранилище в ГБ', async () => {
    const { w, calls } = await mountAdmin({
      ...base,
      'PATCH /api/admin/users/u1': ({ body }) => ({ body: user(body as Partial<AdminUser>) }),
    })
    await w.find('[data-user="u1"] button.quotas').trigger('click')
    const form = w.find('[data-user="u1"] form.quota-form')
    expect(form.find('input[name=max_active_projects]').attributes('placeholder')).toBe('20')
    await form.find('input[name=max_active_projects]').setValue('5')
    await form.find('input[name=storage_quota_gb]').setValue('0,5')
    await form.trigger('submit')
    await flushPromises()
    expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({
      max_active_projects: 5, max_concurrent_tasks: null, storage_quota_bytes: 512 * 1024 ** 2,
    })
    expect(plain(w.find('[data-user="u1"]').text())).toContain('5 МБ из 512 МБ')
  })

  it('ссылка сброса пароля для передачи вручную', async () => {
    const { w } = await mountAdmin({
      ...base,
      'POST /api/admin/users/u1/password-reset': { body: { url: '/reset-password?token=abc', expires_at: '2026-10-20T12:00:00Z' } },
    })
    await w.find('[data-user="u1"] button.reset').trigger('click')
    await flushPromises()
    const link = w.find('[data-user="u1"] .reset-link input')
    expect((link.element as HTMLInputElement).value).toBe(`${window.location.origin}/reset-password?token=abc`)
    expect(w.find('[data-user="u1"]').text()).toContain('сработает один раз')
  })

  it('не администратору раздела нет', async () => {
    const { w } = await mountAdmin({
      'GET /api/admin/overview': { status: 404, body: { error: 'Не найдено' } },
      'GET /api/admin/users': { status: 404, body: { error: 'Не найдено' } },
    })
    expect(w.find('h1').text()).toBe('Страница не найдена')
    expect(w.find('table').exists()).toBe(false)
  })
})
