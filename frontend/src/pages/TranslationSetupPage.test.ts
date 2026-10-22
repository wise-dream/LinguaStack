import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { glossaryFromRows } from '../components/glossary'
import { stubApi } from '../test/fakeApi'
import { book, options, project, providerConfig } from '../test/fixtures'
import { choose, optionLabels, optionValues, selected } from '../test/select'
import TranslationSetupPage from './TranslationSetupPage.vue'

afterEach(() => vi.unstubAllGlobals())

const ollama = providerConfig()
const claude = providerConfig({ id: 'c2', provider_id: 'anthropic', name: 'Claude', sends_data_externally: true, allow_external: true })
const claudeLocked = providerConfig({ id: 'c3', provider_id: 'anthropic', name: 'Claude без разрешения', sends_data_externally: true, allow_external: false })

async function mountSetup(routes: Parameters<typeof stubApi>[0] = {}, bookOverrides = {}) {
  const calls = stubApi({
    'GET /api/books/b1': { body: book(bookOverrides) },
    'GET /api/translation-options': { body: options },
    'GET /api/provider-configs': { body: [ollama, claude, claudeLocked] },
    'GET /api/provider-configs/c1/models': { body: [{ model_id: 'qwen3:8b', supports_temperature: true, supports_system_prompt: true }] },
    'GET /api/provider-configs/c2/models': {
      body: [{ model_id: 'claude-haiku-5-5', supports_temperature: false, supports_system_prompt: true, supported_languages: ['en', 'ru'] }],
    },
    'GET /api/provider-configs/c3/models': { body: [{ model_id: 'claude-haiku-5-5', supports_temperature: false, supports_system_prompt: true }] },
    ...routes,
  })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/books/:id/translate', component: TranslationSetupPage, meta: { title: '' } },
      { path: '/:p(.*)*', component: { template: '<div />' }, meta: { title: '' } },
    ],
  })
  await router.push('/books/b1/translate')
  const w = mount(TranslationSetupPage, { global: { plugins: [router] } })
  await flushPromises()
  return { w, calls, router }
}

describe('настройка перевода', () => {
  it('язык оригинала по умолчанию — язык книги, иначе автоопределение', async () => {
    const { w } = await mountSetup()
    expect(selected(w, 'source_lang')).toBe('en')
    expect(selected(w, 'target_lang')).toBe('ru')

    const other = await mountSetup({}, { language: undefined })
    expect(selected(other.w, 'source_lang')).toBe('auto')
  })

  it('языки ограничены возможностями модели (ТЗ §5.2)', async () => {
    const { w } = await mountSetup()
    // По-русски по алфавиту: английский, немецкий, русский, французский.
    expect(optionValues(w, 'target_lang')).toEqual(['en', 'de', 'ru', 'fr'])
    await choose(w, 'provider_config_id', 'c2')
    await flushPromises()
    expect(optionValues(w, 'target_lang')).toEqual(['en', 'ru'])
    expect(optionValues(w, 'source_lang')).toEqual(['auto', 'en', 'ru'])
  })

  it('стили — от нейтрального, названия языков в списках с заглавной', async () => {
    const { w } = await mountSetup()
    expect(w.findAll('input[name=style]').map((i) => i.attributes('value'))).toEqual(['neutral', 'literary', 'technical', 'simplified'])
    expect(optionLabels(w, 'target_lang')).toContain('Английский')
    expect(optionLabels(w, 'source_lang')[0]).toBe('Определить автоматически')
  })

  it('внешний сервис: предупреждение до запуска (ТЗ §6.4)', async () => {
    const { w } = await mountSetup()
    expect(w.find('.egress').exists()).toBe(false)
    await choose(w, 'provider_config_id', 'c2')
    await flushPromises()
    expect(w.find('.egress').text()).toContain('Текст книги будет отправлен в «Claude»')
    expect(w.find('button[type=submit]').attributes('disabled')).toBeUndefined()
  })

  it('без разрешения на отправку — объяснение, где его дать, и запуск недоступен', async () => {
    const { w } = await mountSetup()
    await choose(w, 'provider_config_id', 'c3')
    await flushPromises()
    const note = w.find('.egress.blocked')
    expect(note.text()).toContain('не разрешает отправку текста')
    expect(note.find('a[href="/settings/providers"]').exists()).toBe(true)
    expect(w.find('button[type=submit]').attributes('disabled')).toBeDefined()
  })

  it('запуск отправляет выбранное и ведёт в монитор', async () => {
    const { w, calls, router } = await mountSetup({
      'POST /api/books/b1/translations': { status: 201, body: project({ id: 'p9', status: 'queued' }) },
    })
    await w.find('input[value=technical]').setValue(true)
    await w.find('input[value=balanced]').setValue(true)
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(calls.find((c) => c.method === 'POST')?.body).toEqual({
      source_lang: 'en', target_lang: 'ru', provider_config_id: 'c1', model_id: 'qwen3:8b',
      style: 'technical', determinism: 'balanced', glossary: {},
    })
    expect(router.currentRoute.value.path).toBe('/translations/p9')
  })

  it('ошибка сервера показана у формы', async () => {
    const { w } = await mountSetup({
      'POST /api/books/b1/translations': { status: 429, body: { error: 'Не больше 20 активных переводов одновременно' } },
    })
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(w.find('form [role=alert]').text()).toBe('Не больше 20 активных переводов одновременно')
  })

  it('без подключений — ведёт в настройки моделей', async () => {
    const { w } = await mountSetup({ 'GET /api/provider-configs': { body: [] } })
    expect(w.find('a[href="/settings/providers"]').exists()).toBe(true)
    expect(w.find('button[type=submit]').exists()).toBe(false)
  })
})

describe('глоссарий', () => {
  it('пустые строки отбрасываются, пробелы обрезаются', () => {
    expect(glossaryFromRows([
      { source: ' lease ', target: ' аренда ' },
      { source: '', target: 'пусто' },
      { source: 'worker', target: '   ' },
      { source: 'queue', target: 'очередь' },
    ])).toEqual({ lease: 'аренда', queue: 'очередь' })
  })
})

describe('примерная цена перевода', () => {
  const priced = {
    'GET /api/provider-configs/c2/models': {
      body: [
        { model_id: 'claude-haiku-5-5', display_name: 'Claude Haiku 5.5', supports_temperature: false, supports_system_prompt: true, input_cost_per_mtok: 0.1, output_cost_per_mtok: 0.5 },
        { model_id: 'claude-opus-5-5', display_name: 'Claude Opus 5.5', supports_temperature: false, supports_system_prompt: true, input_cost_per_mtok: 4, output_cost_per_mtok: 20 },
      ],
    },
    'GET /api/books/b1/estimate': { body: { source_chars: 1_000_000, input_tokens: 1_000_000, output_tokens: 1_400_000 } },
  }

  it('у модели с ценами — цена книги по оценке объёма; другая модель — своя цена', async () => {
    const { w, calls } = await mountSetup(priced)
    await choose(w, 'provider_config_id', 'c2')
    await flushPromises()
    // Haiku: 1 млн × $0.10 + 1,4 млн × $0.50.
    expect(w.find('.estimate').text()).toBe('Примерно $0.80 за книгу — оценка по объёму текста')
    const asked = calls.filter((c) => c.path === '/api/books/b1/estimate').at(-1)!
    expect(new URL(asked.url, 'http://x').searchParams.get('target_lang')).toBe('ru')

    await choose(w, 'model_id', 'claude-opus-5-5')
    await flushPromises()
    // Opus: 1 млн × $4 + 1,4 млн × $20.
    expect(w.find('.estimate').text()).toContain('$32.00')
  })

  it('у модели без цены (Ollama на сервере) — цены нет', async () => {
    const { w } = await mountSetup(priced)
    expect(selected(w, 'provider_config_id')).toBe('c1')
    expect(w.find('.estimate').exists()).toBe(false)
  })

  it('оценка не пришла — страница работает без цены', async () => {
    const { w } = await mountSetup({ ...priced, 'GET /api/books/b1/estimate': { status: 500, body: { error: 'сбой' } } })
    await choose(w, 'provider_config_id', 'c2')
    await flushPromises()
    expect(w.find('.estimate').exists()).toBe(false)
    expect(w.text()).not.toContain('сбой')
  })
})
