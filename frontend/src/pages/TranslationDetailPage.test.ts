import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import type { Segment } from '../api/types'
import { stubApi } from '../test/fakeApi'
import { stubEventSource } from '../test/fakeEventSource'
import { project } from '../test/fixtures'
import TranslationDetailPage from './TranslationDetailPage.vue'

afterEach(() => vi.unstubAllGlobals())

function segment(overrides: Partial<Segment> = {}): Segment {
  return {
    id: 's1', node_id: 'ch0001/b0002', chapter_index: 1, ordinal: 2, block_type: 'paragraph',
    source_text: 'Alice was beginning to get very tired.', translated_text: 'Алиса начала уставать.',
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
    expect(row.text()).toContain('Алиса начала уставать.')
    expect(row.text()).toContain('Разметка «*» изменилась')
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
})
