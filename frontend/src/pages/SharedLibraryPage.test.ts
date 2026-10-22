import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import type { LibraryBook } from '../api/types'
import { stubApi } from '../test/fakeApi'
import SharedLibraryPage from './SharedLibraryPage.vue'

afterEach(() => vi.unstubAllGlobals())

const crafting: LibraryBook = {
  id: 'b1', title: 'Crafting Interpreters', author: 'Robert Nystrom', source_format: 'markdown',
  created_at: '2026-10-20T10:00:00Z', owned: false,
  translations: [{ id: 'p1', source_lang: 'en', target_lang: 'ru', model_id: 'qwen3:8b', status: 'completed', segments_done: 6421, segments_total: 6421 }],
}
const mine: LibraryBook = {
  id: 'b2', title: 'Своя книга', source_format: 'epub', created_at: '2026-10-21T10:00:00Z', owned: true, translations: [],
}

async function mountLibrary(body: LibraryBook[]) {
  stubApi({ 'GET /api/library': { body }, 'GET /api/books/b1/exports': { body: [] } })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/:p(.*)*', component: SharedLibraryPage, meta: { title: '' } }],
  })
  await router.push('/shared')
  const w = mount(SharedLibraryPage, { global: { plugins: [router] } })
  await flushPromises()
  return w
}

describe('общая библиотека', () => {
  it('книги всех пользователей с переводами: читать и скачать', async () => {
    const w = await mountLibrary([crafting, mine])
    const book = w.find('[data-book="b1"]')
    expect(book.find('h2').text()).toBe('Crafting Interpreters')
    expect(book.text()).toContain('Robert Nystrom')
    const tr = book.find('[data-translation="p1"]')
    expect(tr.text()).toContain('qwen3:8b')
    expect(tr.find('a').attributes('href')).toBe('/read/b1?t=p1')
    expect(tr.find('a').text()).toBe('Читать')

    expect(tr.find('section.export').exists()).toBe(false)
    await tr.find('button.download').trigger('click')
    await flushPromises()
    expect(tr.find('section.export').exists()).toBe(true)
    expect(tr.find('button.download').attributes('aria-expanded')).toBe('true')
  })

  it('своя книга отмечена; без переводов — читать оригинал', async () => {
    const w = await mountLibrary([crafting, mine])
    const own = w.find('[data-book="b2"]')
    expect(own.find('.mine').text()).toBe('ваша')
    expect(w.find('[data-book="b1"] .mine').exists()).toBe(false)
    expect(own.find('a.read-original').attributes('href')).toBe('/read/b2')
  })

  it('книг пока нет — так и сказано', async () => {
    const w = await mountLibrary([])
    expect(w.text()).toContain('Книг пока нет')
  })
})
