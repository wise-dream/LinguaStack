import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { stubApi } from '../test/fakeApi'
import { book, project } from '../test/fixtures'
import LibraryPage from './LibraryPage.vue'

afterEach(() => vi.unstubAllGlobals())

async function mountLibrary() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/:p(.*)*', component: { template: '<div />' }, meta: { title: '' } }],
  })
  await router.push('/')
  const w = mount(LibraryPage, { global: { plugins: [router] } })
  await flushPromises()
  return w
}

describe('библиотека', () => {
  it('строка книги: название, автор, формат, дата, языки и состояние (ТЗ §8.2)', async () => {
    stubApi({
      'GET /api/books': { body: [book()] },
      'GET /api/translations': {
        body: [project(), project({ id: 'p2', target_lang: 'de', status: 'running', segments_done: 100 })],
      },
    })
    const w = await mountLibrary()
    const row = w.find('[data-book="b1"]')
    expect(row.text()).toContain('Приключения Алисы в Стране чудес')
    expect(row.text()).toContain('Льюис Кэрролл')
    expect(row.text()).toContain('EPUB')
    expect(row.text()).toContain('16 окт. 2026 г.')
    expect(row.text()).toContain('английский → русский')
    expect(row.text()).toContain('Готово')
    expect(row.text()).toContain('английский → немецкий')
    expect(row.text()).toContain('Переводится')
    expect(row.find('a[href="/books/b1"]').exists()).toBe(true)
  })

  it('из строки книги — сразу к чтению и к новому переводу', async () => {
    stubApi({ 'GET /api/books': { body: [book()] }, 'GET /api/translations': { body: [] } })
    const w = await mountLibrary()
    const row = w.find('[data-book="b1"]')
    expect(row.find('a[href="/read/b1"]').text()).toBe('Читать')
    expect(row.find('a[href="/books/b1/translate"]').text()).toBe('Перевести')
  })

  it('пустая библиотека приглашает загрузить книгу', async () => {
    stubApi({ 'GET /api/books': { body: [] }, 'GET /api/translations': { body: [] } })
    const w = await mountLibrary()
    expect(w.text()).toContain('Загрузите первую книгу')
    expect(w.find('a[href="/import"]').exists()).toBe(true)
  })

  it('удаляет книгу только после подтверждения', async () => {
    const calls = stubApi({
      'GET /api/books': { body: [book()] },
      'GET /api/translations': { body: [] },
      'DELETE /api/books/b1': { status: 204 },
    })
    const w = await mountLibrary()
    const del = () => w.find('[data-book="b1"] button.danger')
    await del().trigger('click')
    expect(calls.some((c) => c.method === 'DELETE')).toBe(false)
    await del().trigger('click')
    await flushPromises()
    expect(calls.some((c) => c.method === 'DELETE' && c.path === '/api/books/b1')).toBe(true)
    expect(w.find('[data-book="b1"]').exists()).toBe(false)
  })

  it('ошибка загрузки списка показана текстом сервера', async () => {
    stubApi({ 'GET /api/books': { status: 500, body: { error: 'Внутренняя ошибка сервера' } }, 'GET /api/translations': { body: [] } })
    const w = await mountLibrary()
    expect(w.find('[role=alert]').text()).toBe('Внутренняя ошибка сервера')
  })
})
