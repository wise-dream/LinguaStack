import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { stubApi } from '../test/fakeApi'
import ForgotPasswordPage from './ForgotPasswordPage.vue'

afterEach(() => vi.unstubAllGlobals())

const accepted = 'Если такой аккаунт есть, ссылка для сброса пароля отправлена на почту или в Telegram.'

async function mountForgot() {
  const calls = stubApi({ 'POST /api/auth/password-reset': { status: 202, body: { message: accepted } } })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/:p(.*)*', component: ForgotPasswordPage, meta: { title: '', public: true } }],
  })
  await router.push('/forgot-password')
  const w = mount(ForgotPasswordPage, { global: { plugins: [router] } })
  return { w, calls }
}

describe('забыли пароль', () => {
  it('отправляет адрес и показывает одно и то же сообщение', async () => {
    const { w, calls } = await mountForgot()
    expect(w.find('button[type=submit]').text()).toBe('Отправить ссылку')
    await w.find('input[type=email]').setValue('  reader@example.com ')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(calls[0]?.body).toEqual({ email: 'reader@example.com' })
    expect(w.find('[role=status]').text()).toBe(accepted)
    expect(w.find('a[href="/login"]').exists()).toBe(true)
  })
})
