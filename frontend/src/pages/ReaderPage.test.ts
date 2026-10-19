import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import type { Block, Segment } from '../api/types'
import { stubApi } from '../test/fakeApi'
import { book, project } from '../test/fixtures'
import ReaderPage from './ReaderPage.vue'

beforeEach(() => {
  Element.prototype.scrollIntoView = vi.fn()
})
afterEach(() => vi.unstubAllGlobals())

const toc = { chapters: [{ index: 0, title: 'Глава 1' }, { index: 1, title: 'Глава 2' }] }

function block(i: number, type: Block['type'], text: string, meta?: Record<string, string>): Block {
  return { node_id: `ch0001/b000${i}`, type, text, source_hash: `h${i}`, ...(meta ? { meta } : {}) }
}

const chapter1 = {
  index: 1,
  title: 'Глава 2',
  blocks: [
    block(0, 'heading', 'Down the Rabbit-Hole', undefined),
    block(1, 'paragraph', 'Alice was *very* tired. <b>no</b>'),
    block(2, 'code_block', 'print("not translated")'),
    block(3, 'image', '', { src: 'img/rabbit.png', alt: 'Кролик' }),
    block(4, 'paragraph', 'The Rabbit ran.'),
  ],
}

function seg(i: number, translated: string | null, overrides: Partial<Segment> = {}): Segment {
  return {
    id: `s${i}`, node_id: `ch0001/b000${i}`, chapter_index: 1, ordinal: i, block_type: 'paragraph',
    source_text: '', translated_text: translated, status: translated === null ? 'pending' : 'completed',
    severity: 'ok', findings: [], attempts: 1, updated_at: '2026-10-17T10:00:00Z', ...overrides,
  }
}

const segments = [
  seg(0, 'В кроличью нору'),
  seg(1, 'Алиса очень устала.', {
    severity: 'warning',
    findings: [{ code: 'markup_mismatch', severity: 'warning', message: 'Разметка «*» изменилась: было 2, стало 0.' }],
  }),
  seg(4, null),
]

function routes(overrides: Parameters<typeof stubApi>[0] = {}) {
  return {
    'GET /api/books/b1': { body: book() },
    'GET /api/books/b1/toc': { body: toc },
    'GET /api/translations': { body: [project(), project({ id: 'p2', target_lang: 'de' })] },
    'GET /api/books/b1/position': { body: null },
    'PUT /api/books/b1/position': { status: 204 },
    'GET /api/books/b1/chapters/0': { body: { index: 0, title: 'Глава 1', blocks: [block(9, 'paragraph', 'First chapter.')] } },
    'GET /api/books/b1/chapters/1': { body: chapter1 },
    'GET /api/translations/p1/segments': { body: segments },
    'GET /api/translations/p2/segments': { body: [] },
    ...overrides,
  }
}

async function mountReader(path: string, overrides: Parameters<typeof stubApi>[0] = {}) {
  const calls = stubApi(routes(overrides))
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/read/:bookId', component: ReaderPage, meta: { title: '' } },
      { path: '/:p(.*)*', component: { template: '<div />' }, meta: { title: '' } },
    ],
  })
  await router.push(path)
  const w = mount(ReaderPage, { global: { plugins: [router] }, attachTo: document.body })
  await flushPromises()
  return { w, calls, router }
}

describe('читалка (ТЗ §8.3)', () => {
  it('оригинал и перевод рядом, выровнены по фрагментам', async () => {
    const { w } = await mountReader('/read/b1?t=p1&ch=1')
    const row = w.find('[data-node="ch0001/b0001"]')
    expect(row.find('.original').html()).toContain('<em>very</em>')
    expect(row.find('.original').text()).toContain('<b>no</b>')
    expect(row.find('.original b').exists()).toBe(false)
    expect(row.find('.translation').text()).toBe('Алиса очень устала.')
    // Код и картинка не переводятся — во всю ширину.
    expect(w.find('[data-node="ch0001/b0002"]').classes()).toContain('span')
    expect(w.find('[data-node="ch0001/b0002"] pre').text()).toBe('print("not translated")')
    expect(w.find('[data-node="ch0001/b0003"] img').attributes('src')).toBe('/api/books/b1/assets/img/rabbit.png')
    expect(w.find('[data-node="ch0001/b0003"] img').attributes('alt')).toBe('Кролик')
    expect(w.find('[data-node="ch0001/b0004"] .translation').text()).toBe('Ещё не переведено')
  })

  it('замечания проверки — пометками на полях', async () => {
    const { w } = await mountReader('/read/b1?t=p1&ch=1')
    expect(w.find('[data-node="ch0001/b0001"] .proof').text()).toContain('Разметка «*» изменилась')
    expect(w.find('[data-node="ch0001/b0000"] .proof').exists()).toBe(false)
  })

  it('режимы «Оригинал» и «Перевод» показывают одну версию', async () => {
    const { w } = await mountReader('/read/b1?t=p1&ch=1')
    await w.find('button[data-mode="original"]').trigger('click')
    expect(w.find('[data-node="ch0001/b0001"] .translation').exists()).toBe(false)
    expect(w.find('[data-node="ch0001/b0001"] .original').exists()).toBe(true)
    await w.find('button[data-mode="translation"]').trigger('click')
    expect(w.find('[data-node="ch0001/b0001"] .original').exists()).toBe(false)
    expect(w.find('[data-node="ch0001/b0001"] .translation').exists()).toBe(true)
  })

  it('переключение языковой версии — переводы книги', async () => {
    const { w, router, calls } = await mountReader('/read/b1?t=p1&ch=1')
    const options = w.findAll('select[name=translation] option').map((o) => o.text())
    expect(options).toEqual(['Без перевода', 'английский → русский', 'английский → немецкий'])
    await w.find('select[name=translation]').setValue('p2')
    await flushPromises()
    expect(router.currentRoute.value.query.t).toBe('p2')
    expect(calls.some((c) => c.path === '/api/translations/p2/segments')).toBe(true)
  })

  it('правка: Ctrl+Enter сохраняет, Esc отменяет', async () => {
    const { w, calls } = await mountReader('/read/b1?t=p1&ch=1', {
      'PATCH /api/segments/s1': { body: seg(1, 'Алиса *очень* устала.') },
    })
    const row = () => w.find('[data-node="ch0001/b0001"]')
    await row().find('button.edit').trigger('click')
    const area = row().find('textarea')
    expect((area.element as HTMLTextAreaElement).value).toBe('Алиса очень устала.')
    await area.trigger('keydown', { key: 'Escape' })
    expect(row().find('textarea').exists()).toBe(false)

    await row().find('button.edit').trigger('click')
    await row().find('textarea').setValue('Алиса *очень* устала.')
    await row().find('textarea').trigger('keydown', { key: 'Enter', ctrlKey: true })
    await flushPromises()
    expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ translated_text: 'Алиса *очень* устала.' })
    expect(row().find('.translation').html()).toContain('<em>очень</em>')
    expect(row().find('textarea').exists()).toBe(false)
  })

  it('ошибка правки и повторного перевода — текстом сервера', async () => {
    const { w } = await mountReader('/read/b1?t=p1&ch=1', {
      'PATCH /api/segments/s1': { status: 422, body: { error: 'Перевод не может быть пустым' } },
      'POST /api/segments/s1/retry': { status: 409, body: { error: 'Повторить можно, когда перевод не выполняется' } },
    })
    const row = () => w.find('[data-node="ch0001/b0001"]')
    await row().find('button.edit').trigger('click')
    await row().find('textarea').setValue('   ')
    await row().find('textarea').trigger('keydown', { key: 'Enter', ctrlKey: true })
    await flushPromises()
    expect(row().find('[role=alert]').text()).toBe('Перевод не может быть пустым')
    expect(row().find('textarea').exists()).toBe(true)

    await row().find('textarea').trigger('keydown', { key: 'Escape' })
    await row().find('button.retry').trigger('click')
    await flushPromises()
    expect(row().find('[role=alert]').text()).toBe('Повторить можно, когда перевод не выполняется')
  })

  it('поиск переходит к главе и подсвечивает фрагмент', async () => {
    const { w, router } = await mountReader('/read/b1?t=p1&ch=0', {
      'GET /api/books/b1/search': {
        body: { results: [{ chapter_index: 1, node_id: 'ch0001/b0004', in: 'original', snippet: 'The Rabbit ran.' }], truncated: false },
      },
    })
    await w.find('input[type=search]').setValue('rabbit')
    await w.find('form.search').trigger('submit')
    await flushPromises()
    await w.find('.search-results button').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query.ch).toBe('1')
    expect(w.find('[data-node="ch0001/b0004"]').classes()).toContain('found')
  })

  it('без главы в адресе открывается сохранённая позиция', async () => {
    const { w, router } = await mountReader('/read/b1', {
      'GET /api/books/b1/position': { body: { chapter_index: 1, node_id: 'ch0001/b0004', translation_id: 'p1', mode: 'translation' } },
    })
    expect(router.currentRoute.value.query).toMatchObject({ ch: '1', t: 'p1' })
    expect(w.find('button[data-mode="translation"]').attributes('aria-pressed')).toBe('true')
    expect(w.find('[data-node="ch0001/b0004"]').exists()).toBe(true)
  })

  it('оглавление переключает главы', async () => {
    const { w, router } = await mountReader('/read/b1?t=p1&ch=1')
    await w.find('nav.reader-toc button[data-chapter="0"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query.ch).toBe('0')
    expect(w.text()).toContain('First chapter.')
  })
})
