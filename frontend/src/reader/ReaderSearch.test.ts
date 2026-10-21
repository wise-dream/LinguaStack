import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { SearchResponse, SearchResult } from '../api/types'
import { stubApi } from '../test/fakeApi'
import ReaderSearch from './ReaderSearch.vue'

afterEach(() => vi.unstubAllGlobals())

function result(node: string, chapter: number, snippet: string, hits: string[], inSide: SearchResult['in'] = 'original'): SearchResult {
  return { chapter_index: chapter, node_id: node, in: inSide, snippet, hits }
}

const three: SearchResponse = {
  results: [
    result('a', 0, 'Кошки спали на крыше.', ['Кошки']),
    result('a', 0, 'RU:Кошки спали.', ['Кошки'], 'translation'),
    result('b', 2, '…и снова кошки.', ['кошки']),
  ],
  total: 3,
  truncated: false,
}

const titles: Record<number, string> = { 0: 'Глава первая', 2: 'Глава третья' }

async function mountSearch(reply: Parameters<typeof stubApi>[0][string]) {
  const calls = stubApi({ 'GET /api/books/b1/search': reply })
  const w = mount(ReaderSearch, {
    props: { bookId: 'b1', translationId: 'p1', chapterTitle: (i: number) => titles[i] ?? '' },
    attachTo: document.body,
  })
  return { w, calls }
}

async function search(w: Awaited<ReturnType<typeof mountSearch>>['w'], q: string) {
  await w.find('input[type=search]').setValue(q)
  await w.find('form.search').trigger('submit')
  await flushPromises()
}

describe('поиск в читалке', () => {
  it('сколько найдено и где: по главам, со счётом', async () => {
    const { w } = await mountSearch({ body: three })
    await search(w, 'кошка')
    expect(w.find('.search-results .summary').text()).toBe('Найдено 3 совпадения')
    const groups = w.findAll('.search-results section')
    expect(groups.map((g) => g.find('h3').text())).toEqual(['Глава первая 2', 'Глава третья 1'])
    expect(groups[0]!.findAll('li .where').map((x) => x.text())).toEqual(['оригинал', 'перевод'])
  })

  it('найденные слова в отрывке отмечены', async () => {
    const { w } = await mountSearch({ body: three })
    await search(w, 'кошка')
    const snippet = w.find('.search-results li .snippet')
    expect(snippet.text()).toBe('Кошки спали на крыше.')
    expect(snippet.findAll('mark').map((m) => m.text())).toEqual(['Кошки'])
  })

  it('показано не всё — так и сказано', async () => {
    const { w } = await mountSearch({ body: { ...three, total: 437, truncated: true } })
    await search(w, 'кошка')
    expect(w.find('.search-results').text()).toContain('Показаны первые 3 из 437 — уточните запрос.')
  })

  it('ничего не нашлось — подсказка ищет исправленный запрос', async () => {
    const { w, calls } = await mountSearch(({ url }) =>
      url.includes(encodeURIComponent('кошкм'))
        ? { body: { results: [], total: 0, truncated: false, suggestion: 'кошки спали' } }
        : { body: three },
    )
    await search(w, 'кошкм спали')
    expect(w.find('.search-results').text()).toContain('Ничего не нашлось.')
    await w.find('button.suggestion').trigger('click')
    await flushPromises()
    expect((w.find('input[type=search]').element as HTMLInputElement).value).toBe('кошки спали')
    expect(new URL(calls.at(-1)!.url, 'http://x').searchParams.get('q')).toBe('кошки спали')
    expect(w.find('.search-results .summary').text()).toBe('Найдено 3 совпадения')
  })

  it('переход к находке закрывает список; стрелки листают находки', async () => {
    const { w } = await mountSearch({ body: three })
    await search(w, 'кошка')
    await w.findAll('.search-results li button')[0]!.trigger('click')
    expect(w.emitted('go')![0]).toEqual([three.results[0]])
    expect(w.find('.search-results').exists()).toBe(false)

    const counter = () => w.find('.stepper output').text()
    const prev = () => w.find('button[aria-label="Предыдущее совпадение"]')
    const next = () => w.find('button[aria-label="Следующее совпадение"]')
    expect(counter()).toBe('1 из 3')
    expect(prev().attributes('disabled')).toBeDefined()
    await next().trigger('click')
    expect(w.emitted('go')![1]).toEqual([three.results[1]])
    expect(counter()).toBe('2 из 3')
    await next().trigger('click')
    expect(next().attributes('disabled')).toBeDefined()
    await prev().trigger('click')
    expect(w.emitted('go')![3]).toEqual([three.results[1]])
  })

  it('новый поиск сбрасывает листание', async () => {
    const { w } = await mountSearch({ body: three })
    await search(w, 'кошка')
    await w.findAll('.search-results li button')[0]!.trigger('click')
    expect(w.find('.stepper').exists()).toBe(true)
    await search(w, 'крыша')
    expect(w.find('.stepper').exists()).toBe(false)
  })
})
