import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { stubApi } from '../test/fakeApi'
import { choose } from '../test/select'
import { kinds, providerConfig } from '../test/fixtures'
import ProvidersPage from './ProvidersPage.vue'

afterEach(() => vi.unstubAllGlobals())

async function mountPage(routes: Parameters<typeof stubApi>[0]) {
  const calls = stubApi({ 'GET /api/providers': { body: kinds }, ...routes })
  const w = mount(ProvidersPage)
  await flushPromises()
  return { w, calls }
}

describe('модели и провайдеры', () => {
  it('адрес API — только у OpenAI-совместимого вида', async () => {
    const { w } = await mountPage({ 'GET /api/provider-configs': { body: [] } })
    await choose(w, 'provider_id', 'ollama')
    expect(w.find('input[name=base_url]').exists()).toBe(false)
    await choose(w, 'provider_id', 'openai_compatible')
    expect(w.find('input[name=base_url]').exists()).toBe(true)
  })

  it('ключ вводится в поле пароля, а после сохранения виден только хвост', async () => {
    const created = providerConfig({ id: 'c2', provider_id: 'anthropic', name: 'Claude', has_api_key: true,
      key_last4: 'a1b2', key_fingerprint: 'sha256:abc', sends_data_externally: true, allow_external: true })
    const { w, calls } = await mountPage({
      'GET /api/provider-configs': { body: [] },
      'POST /api/provider-configs': { status: 201, body: created },
    })
    await choose(w, 'provider_id', 'anthropic')
    expect(w.find('input[name=api_key]').attributes('type')).toBe('password')
    await w.find('input[name=name]').setValue('Claude')
    await w.find('input[name=api_key]').setValue('sk-ant-secret-a1b2')
    await w.find('.consent [role=checkbox]').trigger('click')
    await w.find('form.new-config').trigger('submit')
    await flushPromises()

    const post = calls.find((c) => c.method === 'POST')
    expect(post?.body).toMatchObject({ provider_id: 'anthropic', name: 'Claude', api_key: 'sk-ant-secret-a1b2', allow_external: true })
    const row = w.find('[data-config="c2"]')
    expect(row.text()).toContain('…a1b2')
    expect(w.html()).not.toContain('sk-ant-secret')
    expect((w.find('input[name=api_key]').element as HTMLInputElement).value).toBe('')
  })

  it('внешний сервис требует явного разрешения отправлять текст (ТЗ §6.4)', async () => {
    const { w } = await mountPage({ 'GET /api/provider-configs': { body: [] } })
    await choose(w, 'provider_id', 'anthropic')
    const consent = w.find('.consent')
    expect(consent.text()).toContain('Разрешить отправку текста книг этому сервису')
    expect(consent.text()).toContain('покинет этот сервер')
    await choose(w, 'provider_id', 'ollama')
    expect(w.find('.consent').exists()).toBe(false)
  })

  it('без Ollama сервера (не администратор) — подключить свой сервис', async () => {
    const { w } = await mountPage({
      'GET /api/providers': { body: kinds.filter((k) => k.provider_id !== 'ollama') },
      'GET /api/provider-configs': { body: [] },
    })
    const intro = w.find('.intro').text()
    expect(intro).not.toContain('Ollama')
    expect(intro).toContain('со своим ключом API')
  })

  it('с Ollama сервера — она упомянута во введении', async () => {
    const { w } = await mountPage({ 'GET /api/provider-configs': { body: [] } })
    expect(w.find('.intro').text()).toContain('локальную модель Ollama')
  })

  it('503 без ключа шифрования и 409 при удалении — понятным текстом', async () => {
    const { w } = await mountPage({
      'GET /api/provider-configs': { body: [providerConfig()] },
      'POST /api/provider-configs': { status: 503, body: { error: 'Сервер не может хранить ключи: не задан LINGUASTACK_SECRET_KEY. Обратитесь к администратору.' } },
      'DELETE /api/provider-configs/c1': { status: 409, body: { error: 'Конфигурацией пользуются проекты перевода — сначала удалите их или смените провайдера' } },
    })
    await choose(w, 'provider_id', 'anthropic')
    await w.find('input[name=api_key]').setValue('sk-ant-x')
    await w.find('form.new-config').trigger('submit')
    await flushPromises()
    expect(w.find('form.new-config [role=alert]').text()).toContain('не задан LINGUASTACK_SECRET_KEY')

    const del = w.find('[data-config="c1"] button.danger')
    await del.trigger('click')
    await del.trigger('click')
    await flushPromises()
    expect(w.find('[data-config="c1"] [role=alert]').text()).toContain('пользуются проекты перевода')
    expect(w.find('[data-config="c1"]').exists()).toBe(true)
  })

  it('«Проверить» показывает ответ сервера', async () => {
    const { w } = await mountPage({
      'GET /api/provider-configs': { body: [providerConfig()] },
      'GET /api/provider-configs/c1/health': { body: { available: true, detail: 'Ollama 0.18.2' } },
    })
    await w.find('[data-config="c1"] button.check').trigger('click')
    await flushPromises()
    expect(w.find('[data-config="c1"] [role=status]').text()).toBe('Доступен: Ollama 0.18.2')
  })

  it('разрешение на отправку наружу можно отозвать', async () => {
    const external = providerConfig({ id: 'c3', provider_id: 'anthropic', name: 'Claude', sends_data_externally: true, allow_external: true })
    const { w, calls } = await mountPage({
      'GET /api/provider-configs': { body: [external] },
      'PATCH /api/provider-configs/c3': { body: { ...external, allow_external: false } },
    })
    await w.find('[data-config="c3"] [role=switch]').trigger('click')
    await flushPromises()
    expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ allow_external: false })
  })
})
