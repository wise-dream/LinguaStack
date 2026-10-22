import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import type { Segment } from '../api/types'
import { stubApi } from '../test/fakeApi'
import { stubEventSource } from '../test/fakeEventSource'
import { project, providerConfig } from '../test/fixtures'
import { choose, selected } from '../test/select'
import TranslationDetailPage from './TranslationDetailPage.vue'

afterEach(() => vi.unstubAllGlobals())

function segment(overrides: Partial<Segment> = {}): Segment {
  return {
    id: 's1', node_id: 'ch0001/b0002', chapter_index: 1, ordinal: 2, block_type: 'paragraph',
    source_text: 'Alice was beginning to get `very` tired.', translated_text: 'Алиса начала уставать.',
    status: 'completed', severity: 'ok', findings: [], attempts: 1, updated_at: '2026-10-17T10:00:00Z',
    ...overrides,
  }
}

async function mountDetail(routes: Parameters<typeof stubApi>[0]) {
  const streams = stubEventSource()
  const calls = stubApi(routes)
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/translations/:id', component: TranslationDetailPage, meta: { title: '' } },
      { path: '/:p(.*)*', component: { template: '<div />' }, meta: { title: '' } },
    ],
  })
  await router.push('/translations/p1')
  const w = mount(TranslationDetailPage, { global: { plugins: [router] } })
  await flushPromises()
  return { w, calls, streams }
}

describe('страница перевода', () => {
  it('панель экспорта этого перевода (ТЗ §3.2)', async () => {
    const { w, calls } = await mountDetail({
      'GET /api/translations/p1': { body: project() },
      'GET /api/translations/p1/segments': { body: [] },
      'GET /api/books/b1/exports': { body: [] },
    })
    const panel = w.find('section.export')
    expect(panel.find('h2').text()).toBe('Экспорт')
    expect(panel.find('button[type=submit]').text()).toBe('Экспортировать')
    expect(calls.some((c) => c.path === '/api/books/b1/exports')).toBe(true)
    expect(panel.find('[role=alert]').exists()).toBe(false)
  })

  it('живой прогресс из потока событий', async () => {
    const { w, streams } = await mountDetail({
      'GET /api/translations/p1': { body: project({ status: 'running', segments_done: 10 }) },
      'GET /api/translations/p1/segments': { body: [] },
    })
    expect(w.text()).toContain('10 из 873')
    streams[0]?.emit({ ...project({ status: 'running', segments_done: 11 }) })
    await flushPromises()
    expect(w.text()).toContain('11 из 873')
  })

  it('кнопки по состоянию; отмена — только после подтверждения', async () => {
    const { w, calls } = await mountDetail({
      'GET /api/translations/p1': { body: project({ status: 'running', segments_done: 10 }) },
      'GET /api/translations/p1/segments': { body: [] },
      'POST /api/translations/p1/cancel': { body: project({ status: 'cancelled', segments_done: 10 }) },
    })
    expect(w.find('button.pause').text()).toBe('Приостановить')
    expect(w.find('button.resume').exists()).toBe(false)
    const cancel = w.find('button.danger')
    await cancel.trigger('click')
    expect(calls.some((c) => c.path.endsWith('/cancel'))).toBe(false)
    await cancel.trigger('click')
    await flushPromises()
    expect(calls.some((c) => c.path.endsWith('/cancel'))).toBe(true)
    expect(w.text()).toContain('Отменён')
    expect(w.find('button.danger').exists()).toBe(false)
  })

  it('фильтры сегментов уходят в запрос', async () => {
    const { w, calls } = await mountDetail({
      'GET /api/translations/p1': { body: project() },
      'GET /api/translations/p1/segments': { body: [segment()] },
    })
    const lastSegments = () => calls.filter((c) => c.path.endsWith('/segments')).at(-1)?.url
    expect(lastSegments()).toBe('/api/translations/p1/segments?limit=50&offset=0')
    await w.find('button[data-filter="failed"]').trigger('click')
    await flushPromises()
    expect(lastSegments()).toBe('/api/translations/p1/segments?status=failed&limit=50&offset=0')
    await w.find('button[data-filter="warning"]').trigger('click')
    await flushPromises()
    expect(lastSegments()).toBe('/api/translations/p1/segments?severity=warning&limit=50&offset=0')
  })

  it('сегмент с замечанием: оригинал, перевод, замечание и повтор; 409 — текстом сервера', async () => {
    const { w } = await mountDetail({
      'GET /api/translations/p1': { body: project({ status: 'running' }) },
      'GET /api/translations/p1/segments': {
        body: [segment({ severity: 'warning', findings: [{ code: 'markup_mismatch', severity: 'warning', message: 'Разметка «*» изменилась: было 2, стало 0.' }] })],
      },
      'POST /api/segments/s1/retry': { status: 409, body: { error: 'Повторить можно, когда перевод не выполняется' } },
    })
    const row = w.find('[data-segment="s1"]')
    expect(row.text()).toContain('Alice was beginning')
    expect(row.find('.original code').text()).toBe('very')
    expect(row.text()).toContain('Алиса начала уставать.')
    expect(row.text()).toContain('Разметка «*» изменилась')
    // Текст фрагмента — через renderInline, как в читалке.
    expect(row.find('.original').html()).not.toContain('`')
    expect(row.find('a[href="/read/b1?t=p1&ch=1&n=ch0001/b0002"]').exists()).toBe(true)
    await row.find('button.retry').trigger('click')
    await flushPromises()
    expect(w.find('[data-segment="s1"] [role=alert]').text()).toBe('Повторить можно, когда перевод не выполняется')
  })

  it('«Показать ещё» дочитывает следующую страницу', async () => {
    const page = Array.from({ length: 50 }, (_, i) => segment({ id: `s${i}`, node_id: `n${i}` }))
    const { w, calls } = await mountDetail({
      'GET /api/translations/p1': { body: project() },
      'GET /api/translations/p1/segments': (c) => ({ body: c.url.includes('offset=50') ? [segment({ id: 'last', node_id: 'last' })] : page }),
    })
    await w.find('button.more').trigger('click')
    await flushPromises()
    expect(calls.filter((c) => c.path.endsWith('/segments')).at(-1)?.url).toContain('offset=50')
    expect(w.findAll('[data-segment]')).toHaveLength(51)
    expect(w.find('button.more').exists()).toBe(false)
  })

  it('фрагменты перечитываются, когда перевод начинается и заканчивается', async () => {
    let call = 0
    const { streams, calls } = await mountDetail({
      'GET /api/translations/p1': { body: project({ status: 'queued', segments_total: 0, segments_done: 0 }) },
      'GET /api/translations/p1/segments': () => ({ body: call++ === 0 ? [] : [segment()] }),
    })
    const segmentLoads = () => calls.filter((c) => c.path.endsWith('/segments')).length
    expect(segmentLoads()).toBe(1)
    streams[0]?.emit({ ...project({ status: 'running', segments_done: 0 }) })
    await flushPromises()
    expect(segmentLoads()).toBe(2)
    streams[0]?.emit({ ...project({ status: 'running', segments_done: 3 }) })
    await flushPromises()
    expect(segmentLoads()).toBe(2) // прогресс внутри перевода список не дёргает
    streams[0]?.emit({ ...project({ status: 'completed', segments_done: 873 }) })
    await flushPromises()
    expect(segmentLoads()).toBe(3)
  })
})

describe('перевести заново с ошибками и предупреждениями', () => {
  const routes = (p: ReturnType<typeof project>) => ({
    'GET /api/translations/p1': { body: p },
    'GET /api/translations/p1/segments': { body: [] },
    'GET /api/books/b1/exports': { body: [] },
  })

  it('кнопка со счётом; нажатие ставит их в очередь и перезапускает прогресс', async () => {
    const before = project({ status: 'failed', segments_failed: 7, warnings_count: 2 })
    const { w, calls, streams } = await mountDetail({
      ...routes(before),
      'POST /api/translations/p1/retry-problems': {
        status: 202,
        body: { queued: 9, project: { ...before, status: 'queued', segments_failed: 0 } },
      },
    })
    const button = w.find('button.retry-problems')
    expect(button.text()).toBe('Перевести заново с ошибками и предупреждениями (9)')
    const streamsBefore = streams.length
    await button.trigger('click')
    await flushPromises()
    expect(calls.some((c) => c.method === 'POST' && c.path === '/api/translations/p1/retry-problems')).toBe(true)
    expect(w.find('[role=status]').text()).toBe('9 фрагментов поставлены в очередь на перевод.')
    expect(streams.length).toBeGreaterThan(streamsBefore)
    expect(w.find('button.retry-problems').exists()).toBe(false)
  })

  it('нечего повторять — кнопки нет', async () => {
    const { w } = await mountDetail(routes(project({ status: 'completed', segments_failed: 0, warnings_count: 0 })))
    expect(w.find('button.retry-problems').exists()).toBe(false)
  })

  it('пока перевод в очереди или идёт — кнопки нет (сервер ответил бы «перевод идёт»)', async () => {
    for (const status of ['running', 'queued'] as const) {
      const { w } = await mountDetail(routes(project({ status, segments_failed: 1, warnings_count: 0 })))
      expect(w.find('button.retry-problems').exists()).toBe(false)
    }
  })
})

describe('поиск фрагмента по тексту', () => {
  it('запрос уходит вместе с фильтром; ничего не нашлось — так и сказано; очистка возвращает всё', async () => {
    const { w, calls } = await mountDetail({
      'GET /api/translations/p1': { body: project() },
      'GET /api/translations/p1/segments': ({ url }) =>
        url.includes('q=') ? { body: url.includes('zebra') ? [] : [segment()] } : { body: [segment(), segment({ id: 's2' })] },
      'GET /api/books/b1/exports': { body: [] },
    })
    const form = w.find('form.segment-search')
    expect(form.find('input').attributes('placeholder')).toBe('Найти фрагмент по тексту')
    await form.find('input').setValue('tired')
    await form.trigger('submit')
    await flushPromises()
    const last = () => new URL(calls.filter((c) => c.path === '/api/translations/p1/segments').at(-1)!.url, 'http://x').searchParams
    expect(last().get('q')).toBe('tired')
    expect(w.findAll('[data-segment]')).toHaveLength(1)

    await w.find('[data-filter="warning"]').trigger('click')
    await flushPromises()
    expect(last().get('q')).toBe('tired')
    expect(last().get('severity')).toBe('warning')

    await form.find('input').setValue('zebra')
    await form.trigger('submit')
    await flushPromises()
    expect(w.find('.segments').text()).toContain('Ничего не нашлось.')

    await form.find('input').setValue('')
    await form.trigger('submit')
    await flushPromises()
    expect(last().get('q')).toBeNull()
  })
})

describe('чужой перевод из общей библиотеки', () => {
  it('читать, искать и скачивать можно; управлять и повторять — нет', async () => {
    const { w } = await mountDetail({
      'GET /api/translations/p1': { body: project({ owned: false, status: 'failed', segments_failed: 2 }) },
      'GET /api/translations/p1/segments': { body: [segment({ status: 'failed', translated_text: null, severity: 'error' })] },
      'GET /api/books/b1/exports': { body: [] },
    })
    for (const cls of ['pause', 'resume', 'retry-problems', 'retry']) {
      expect(w.find(`button.${cls}`).exists(), cls).toBe(false)
    }
    expect(w.text()).not.toContain('Отменить перевод')
    expect(w.find('section.export').exists()).toBe(true)
    expect(w.find('form.segment-search').exists()).toBe(true)
  })
})

describe('смена модели', () => {
  const configs = [
    providerConfig(),
    providerConfig({ id: 'c2', provider_id: 'anthropic', name: 'Anthropic', sends_data_externally: true, allow_external: true }),
    providerConfig({ id: 'c3', provider_id: 'anthropic', name: 'Без разрешения', sends_data_externally: true }),
  ]
  const routes = (status: 'running' | 'cancelled' = 'running', owned = true) => ({
    'GET /api/translations/p1': { body: project({ status, owned }) },
    'GET /api/translations/p1/segments': { body: [] },
    'GET /api/books/b1/exports': { body: [] },
    'GET /api/provider-configs': { body: configs },
    'GET /api/provider-configs/c1/models': { body: [{ model_id: 'qwen3:8b' }, { model_id: 'qwen2.5-coder:14b' }] },
    'GET /api/provider-configs/c2/models': { body: [{ model_id: 'claude-haiku-5-5', display_name: 'Claude Haiku 5.5' }] },
    'GET /api/provider-configs/c3/models': { body: [{ model_id: 'claude-haiku-5-5' }] },
    'PATCH /api/translations/p1': (c: { body: unknown }) => ({
      body: project({ status, ...(c.body as { provider_config_id: string; model_id: string }) }),
    }),
  })

  it('форма открыта на текущей модели; смена уходит на сервер, страница показывает новую', async () => {
    const { w, calls } = await mountDetail(routes())
    await w.find('button.change-model').trigger('click')
    await flushPromises()
    const form = w.find('form.model-change')
    expect(selected(w, 'provider_config_id')).toBe('c1')
    expect(selected(w, 'model_id')).toBe('qwen3:8b')
    expect(form.find('button[type=submit]').attributes('disabled'), 'та же модель — менять нечего').toBeDefined()
    expect(w.find('button.change-model').exists(), 'форма открыта — второй кнопки нет').toBe(false)

    await choose(w, 'provider_config_id', 'c2')
    await flushPromises()
    expect(selected(w, 'model_id')).toBe('claude-haiku-5-5')
    expect(form.text()).toContain('Текст книги будет отправлен в «Anthropic»')
    await form.trigger('submit')
    await flushPromises()

    const patch = calls.find((c) => c.method === 'PATCH')
    expect(patch?.body).toEqual({ provider_config_id: 'c2', model_id: 'claude-haiku-5-5' })
    expect(w.find('form.model-change').exists()).toBe(false)
    expect(w.find('.model').text()).toBe('claude-haiku-5-5')
    expect(w.find('[role=status]').text()).toContain('Следующие фрагменты переведёт claude-haiku-5-5')
  })

  it('подключение без разрешения на передачу наружу — сменить нельзя', async () => {
    const { w } = await mountDetail(routes())
    await w.find('button.change-model').trigger('click')
    await flushPromises()
    await choose(w, 'provider_config_id', 'c3')
    await flushPromises()
    expect(w.find('form.model-change').text()).toContain('не разрешает отправку текста книг наружу')
    expect(w.find('form.model-change button[type=submit]').attributes('disabled')).toBeDefined()
  })

  it('чужой и отменённый перевод — без смены модели', async () => {
    expect((await mountDetail(routes('running', false))).w.find('button.change-model').exists()).toBe(false)
    expect((await mountDetail(routes('cancelled'))).w.find('button.change-model').exists()).toBe(false)
  })
})

describe('экспорт на странице перевода', () => {
  it('экспортов ещё нет — так и сказано', async () => {
    const { w } = await mountDetail({
      'GET /api/translations/p1': { body: project() },
      'GET /api/translations/p1/segments': { body: [] },
      'GET /api/books/b1/exports': { body: [] },
    })
    expect(w.find('section.export .no-exports').text()).toBe('Экспортов ещё нет — готовые файлы появятся здесь.')
  })
})
