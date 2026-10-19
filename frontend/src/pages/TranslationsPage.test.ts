import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { stubApi } from '../test/fakeApi'
import { project } from '../test/fixtures'
import TranslationsPage from './TranslationsPage.vue'

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

async function mountMonitor() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/:p(.*)*', component: { template: '<div />' }, meta: { title: '' } }],
  })
  await router.push('/translations')
  const w = mount(TranslationsPage, { global: { plugins: [router] } })
  await flushPromises()
  return w
}

const list = [
  project({ id: 'run', status: 'running', segments_done: 497, segments_failed: 2, warnings_count: 37,
    started_at: '2026-10-17T10:00:00Z', finished_at: undefined, input_tokens: 120000, output_tokens: 60000, provider_calls: 41 }),
  project({ id: 'wait', status: 'queued', segments_done: 0, started_at: undefined, finished_at: undefined }),
  project({ id: 'pause', status: 'paused', segments_done: 100 }),
  project({ id: 'done', status: 'completed_with_warnings', cost_usd: 0.0123 }),
  project({ id: 'fail', status: 'failed', last_error: 'провайдер недоступен' }),
]

describe('монитор переводов (ТЗ §8.4)', () => {
  it('делит переводы на идущие, ждущие и завершённые', async () => {
    stubApi({ 'GET /api/translations': { body: list } })
    const w = await mountMonitor()
    const ids = (group: string) => w.findAll(`[data-group="${group}"] [data-project]`).map((r) => r.attributes('data-project'))
    expect(ids('active')).toEqual(['run'])
    expect(ids('waiting')).toEqual(['wait', 'pause'])
    expect(ids('finished')).toEqual(['done', 'fail'])
  })

  it('строка: книга, языки, модель, прогресс, ошибки, время и статистика провайдера', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-10-17T12:05:00Z'))
    stubApi({ 'GET /api/translations': { body: list } })
    const w = await mountMonitor()
    const row = w.find('[data-project="run"]').text().replace(/\s+/g, ' ')
    expect(row).toContain('Приключения Алисы в Стране чудес')
    expect(row).toContain('английский → русский')
    expect(row).toContain('qwen3:8b')
    expect(row).toContain('497 из 873')
    expect(row).toContain('2 с ошибкой')
    expect(row).toContain('37 с замечаниями')
    expect(row).toContain('2 ч 05 мин')
    expect(row).toContain('41 запрос')
    expect(row).toContain('120 000 токенов на входе, 60 000 на выходе')
    expect(w.find('[data-project="done"]').text()).toContain('$0.0123')
    expect(w.find('[data-project="fail"]').text()).toContain('провайдер недоступен')
  })

  it('пока есть активные переводы, список обновляется сам', async () => {
    vi.useFakeTimers()
    const calls = stubApi({ 'GET /api/translations': { body: list } })
    await mountMonitor()
    const before = calls.length
    await vi.advanceTimersByTimeAsync(5_000)
    expect(calls.length).toBe(before + 1)
  })

  it('пустой монитор объясняет, откуда берутся переводы', async () => {
    stubApi({ 'GET /api/translations': { body: [] } })
    const w = await mountMonitor()
    expect(w.text()).toContain('Переводов пока нет')
    expect(w.find('a[href="/"]').exists()).toBe(true)
  })
})
