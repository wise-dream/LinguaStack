import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { stubApi } from '../test/fakeApi'
import { book, project } from '../test/fixtures'
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

  it('просит перевод только для книги: фильтр по book_id', async () => {
    const { calls } = await mountBook({ ...base, 'GET /api/translations': { body: [] } })
    expect(calls.find((c) => c.path === '/api/translations')?.url).toBe('/api/translations?book_id=b1')
  })
})
