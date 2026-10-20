import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { stubApi } from '../test/fakeApi'
import ResetPasswordPage from './ResetPasswordPage.vue'

afterEach(() => vi.unstubAllGlobals())

async function mountReset(path: string, routes: Parameters<typeof stubApi>[0] = {}) {
  const calls = stubApi(routes)
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/login', component: { template: '<p>вход</p>' }, meta: { title: '', public: true } },
      { path: '/reset-password', component: ResetPasswordPage, meta: { title: '', public: true } },
    ],
  })
  await router.push(path)
  const w = mount(ResetPasswordPage, { global: { plugins: [router] } })
  return { w, calls, router }
}

describe('новый пароль по ссылке', () => {
  it('задаёт пароль и ведёт на вход', async () => {
    const { w, calls, router } = await mountReset('/reset-password?token=abc', {
      'POST /api/auth/password-reset/confirm': { status: 204 },
    })
    expect(w.find('button[type=submit]').text()).toBe('Сохранить пароль')
    await w.find('input[type=password]').setValue('new-password-123')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(calls[0]?.body).toEqual({ token: 'abc', password: 'new-password-123' })
    expect(router.currentRoute.value.path).toBe('/login')
    expect(router.currentRoute.value.query.reset).toBe('done')
  })

  it('устаревшая ссылка — ошибка и путь к новой', async () => {
    const { w } = await mountReset('/reset-password?token=old', {
      'POST /api/auth/password-reset/confirm': {
        status: 422, body: { error: 'Ссылка для сброса пароля недействительна или устарела. Запросите новую.' },
      },
    })
    await w.find('input[type=password]').setValue('new-password-123')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(w.find('[role=alert]').text()).toContain('устарела')
    expect(w.find('a[href="/forgot-password"]').text()).toBe('Запросить новую ссылку')
  })

  it('ссылка без кода — формы нет', async () => {
    const { w } = await mountReset('/reset-password')
    expect(w.find('form').exists()).toBe(false)
    expect(w.text()).toContain('В ссылке нет кода')
    expect(w.find('a[href="/forgot-password"]').exists()).toBe(true)
  })
})
