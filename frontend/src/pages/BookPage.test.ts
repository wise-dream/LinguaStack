import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { stubApi } from '../test/fakeApi'
import { book, exportJob, project } from '../test/fixtures'
import BookPage from './BookPage.vue'

afterEach(() => vi.unstubAllGlobals())

async function mountBook(routes: Parameters<typeof stubApi>[0]) {
  const calls = stubApi(routes)
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/books/:id', component: BookPage, meta: { title: '' } },
      { path: '/:p(.*)*', component: { template: '<div />' }, meta: { title: '' } },
    ],
  })
  await router.push('/books/b1')
  const w = mount(BookPage, { global: { plugins: [router] } })
  await flushPromises()
  return { w, calls }
}

const base = {
  'GET /api/books/b1': { body: book() },
  'GET /api/books/b1/exports': { body: [] },
  'GET /api/books/b1/toc': { body: { chapters: [{ index: 0, title: 'Глава 1' }, { index: 1, title: 'Глава 2' }] } },
}

describe('страница книги', () => {
  it('метаданные, оглавление и переводы с действиями', async () => {
    const { w } = await mountBook({
      ...base,
      'GET /api/translations': { body: [project(), project({ id: 'p2', target_lang: 'de', status: 'paused', segments_done: 10 })] },
    })
    expect(w.find('h1').text()).toBe('Приключения Алисы в Стране чудес')
    expect(w.text()).toContain('Льюис Кэрролл')
    expect(w.text()).toContain('Глава 2')
    expect(w.find('a[href="/books/b1/translate"]').text()).toBe('Новый перевод')
    const done = w.find('[data-project="p1"]')
    expect(done.find('a[href="/read/b1?t=p1"]').text()).toBe('Читать')
    const paused = w.find('[data-project="p2"]')
    expect(paused.find('button.resume').text()).toBe('Продолжить')
  })

  it('«Продолжить» возобновляет приостановленный или упавший перевод (ТЗ §8.2)', async () => {
    const { w, calls } = await mountBook({
      ...base,
      'GET /api/translations': { body: [project({ status: 'failed', segments_done: 477 })] },
      'POST /api/translations/p1/resume': { body: project({ status: 'queued', segments_done: 477 }) },
    })
    await w.find('[data-project="p1"] button.resume').trigger('click')
    await flushPromises()
    expect(calls.some((c) => c.method === 'POST' && c.path === '/api/translations/p1/resume')).toBe(true)
    expect(w.find('[data-project="p1"]').text()).toContain('В очереди')
    expect(w.find('[data-project="p1"] button.resume').exists()).toBe(false)
  })

  it('история экспортов книги — с языком перевода и скачиванием (ТЗ §8.2)', async () => {
    const { w } = await mountBook({
      ...base,
      'GET /api/translations': { body: [project(), project({ id: 'p2', target_lang: 'de' })] },
      'GET /api/books/b1/exports': {
        body: [exportJob(), exportJob({ id: 'e2', translation_id: 'p2', format: 'markdown', mode: 'bilingual', status: 'running' })],
      },
    })
    const history = w.find('section.exports-history')
    expect(history.find('h2').text()).toBe('Экспорты')
    const first = history.find('[data-export=e1]')
    expect(first.text()).toContain('EPUB')
    expect(first.text()).toContain('русский')
    expect(first.find('a.download').attributes('href')).toBe('/api/exports/e1/file')
    const second = history.find('[data-export=e2]')
    expect(second.text()).toContain('оригинал и перевод')
    expect(second.text()).toContain('немецкий')
    expect(second.text()).toContain('Собирается')
  })

  it('без экспортов — подсказка, где их сделать', async () => {
    const { w } = await mountBook({ ...base, 'GET /api/translations': { body: [project()] } })
    expect(w.find('section.exports-history').text()).toContain('Экспортов ещё нет')
  })

  it('просит перевод только для книги: фильтр по book_id', async () => {
    const { calls } = await mountBook({ ...base, 'GET /api/translations': { body: [] } })
    expect(calls.find((c) => c.path === '/api/translations')?.url).toBe('/api/translations?book_id=b1')
  })
})

describe('чужая книга из общей библиотеки', () => {
  it('читать и скачивать можно, переводить и продолжать — нет', async () => {
    const { w } = await mountBook({
      ...base,
      'GET /api/books/b1': { body: book({ owned: false }) },
      'GET /api/translations': { body: [project({ owned: false, status: 'paused', segments_done: 10 })] },
    })
    expect(w.text()).not.toContain('Новый перевод')
    expect(w.find('a[href="/read/b1"]').text()).toBe('Читать оригинал')
    expect(w.find('button.resume').exists()).toBe(false)
  })
})
