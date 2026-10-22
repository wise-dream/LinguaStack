import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import type { LibraryPage as Page } from '../api/types'
import { SEARCH_DELAY_MS } from '../lib/library'
import { stubApi, type FakeCall } from '../test/fakeApi'
import { libraryBook, libraryTranslation } from '../test/fixtures'
import LibraryPage from './LibraryPage.vue'

afterEach(() => vi.unstubAllGlobals())

const page = (books: Page['books'], extra: Partial<Page> = {}): Page => ({ books, total: books.length, ...extra })

async function mountLibrary(path = '/') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/:p(.*)*', component: { template: '<div />' }, meta: { title: '' } }],
  })
  await router.push(path)
  const w = mount(LibraryPage, { global: { plugins: [router] }, attachTo: document.body })
  await flushPromises()
  return { w, router }
}

const params = (c: FakeCall) => new URL(c.url, 'http://x').searchParams

describe('библиотека', () => {
  it('строка книги: название, автор, формат, дата, языки и состояние (ТЗ §8.2)', async () => {
    const calls = stubApi({
      'GET /api/library': {
        body: page([
          libraryBook({
            translations: [libraryTranslation(), libraryTranslation({ id: 'p2', target_lang: 'de', status: 'running' })],
          }),
        ]),
      },
    })
    const { w } = await mountLibrary()
    expect(params(calls[0]!).get('mine')).toBe('1')
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
    expect(row.find('a[href="/translations/p2"]').exists()).toBe(true)
    w.unmount()
  })

  it('из строки книги — сразу к чтению и к новому переводу', async () => {
    stubApi({ 'GET /api/library': { body: page([libraryBook()]) } })
    const { w } = await mountLibrary()
    const row = w.find('[data-book="b1"]')
    expect(row.find('a[href="/read/b1"]').text()).toBe('Читать')
    expect(row.find('a[href="/books/b1/translate"]').text()).toBe('Перевести')
    w.unmount()
  })

  it('пустая библиотека приглашает загрузить книгу, поиска в ней нет', async () => {
    stubApi({ 'GET /api/library': { body: page([]) } })
    const { w } = await mountLibrary()
    expect(w.text()).toContain('Загрузите первую книгу')
    expect(w.find('a[href="/import"]').exists()).toBe(true)
    expect(w.find('form[role=search]').exists()).toBe(false)
    w.unmount()
  })

  it('удаляет книгу только после подтверждения', async () => {
    const calls = stubApi({
      'GET /api/library': { body: page([libraryBook(), libraryBook({ id: 'b2', title: 'Вторая' })], { total: 30, next: 'c1' }) },
      'DELETE /api/books/b1': { status: 204 },
    })
    const { w } = await mountLibrary()
    const del = () => w.find('[data-book="b1"] button.danger')
    await del().trigger('click')
    expect(calls.some((c) => c.method === 'DELETE')).toBe(false)
    await del().trigger('click')
    await flushPromises()
    expect(calls.some((c) => c.method === 'DELETE' && c.path === '/api/books/b1')).toBe(true)
    expect(w.find('[data-book="b1"]').exists()).toBe(false)
    expect(w.find('.load-more').text()).toContain('Показано 1 из 29')
    w.unmount()
  })

  it('ошибка загрузки списка показана текстом сервера', async () => {
    stubApi({ 'GET /api/library': { status: 500, body: { error: 'Внутренняя ошибка сервера' } } })
    const { w } = await mountLibrary()
    expect(w.find('[role=alert]').text()).toBe('Внутренняя ошибка сервера')
    w.unmount()
  })

  it('поиск по названию и автору: запрос — на сервер и в адрес страницы', async () => {
    const calls = stubApi({
      'GET /api/library': (c) =>
        ({ body: params(c).get('q') === 'нет такой' ? page([]) : page([libraryBook()]) }),
    })
    const { w, router } = await mountLibrary()
    const field = w.find('form[role=search] input')
    expect(field.attributes('placeholder')).toBe('Название или автор')
    await field.setValue('нет такой')
    await w.find('form[role=search]').trigger('submit')
    await flushPromises()
    expect(params(calls.at(-1)!).get('q')).toBe('нет такой')
    expect(params(calls.at(-1)!).get('mine')).toBe('1')
    expect(router.currentRoute.value.query.q).toBe('нет такой')
    expect(w.text()).toContain('Ничего не нашлось по запросу «нет такой».')
    expect(w.text()).not.toContain('Загрузите первую книгу')
    expect(w.find('form[role=search]').exists(), 'поле не пропадает вместе с результатами').toBe(true)
    w.unmount()
  })

  it('набор ищет сам после паузы, а не на каждую букву', async () => {
    const calls = stubApi({ 'GET /api/library': { body: page([libraryBook()]) } })
    const { w } = await mountLibrary()
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    try {
      const field = w.find('form[role=search] input')
      await field.setValue('ал')
      await field.setValue('алиса')
      vi.advanceTimersByTime(SEARCH_DELAY_MS - 1)
      expect(calls).toHaveLength(1)
      vi.advanceTimersByTime(1)
      await flushPromises()
      expect(calls).toHaveLength(2)
      expect(params(calls[1]!).get('q')).toBe('алиса')
    } finally {
      vi.useRealTimers()
      w.unmount()
    }
  })

  it('запрос из адреса страницы ищется сразу', async () => {
    const calls = stubApi({ 'GET /api/library': { body: page([libraryBook()]) } })
    const { w } = await mountLibrary('/?q=алиса')
    expect(params(calls[0]!).get('q')).toBe('алиса')
    expect((w.find('form[role=search] input').element as HTMLInputElement).value).toBe('алиса')
    expect(w.text()).toContain('Найдена 1 книга')
    w.unmount()
  })
})
