import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { ApiError } from '../api/client'
import type { User } from '../api/types'
import { SESSION, createSession, type AuthApi } from '../session'
import LoginPage from './LoginPage.vue'
import RegisterPage from './RegisterPage.vue'

const user: User = { id: 'u1', email: 'a@b.c', role: 'user', created_at: '2026-10-19T00:00:00Z' }

async function mountPage(component: object, api: AuthApi, path: string) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' }, meta: { title: 'Библиотека' } },
      { path: '/books/:id', component: { template: '<div />' }, meta: { title: 'Книга' } },
      { path: '/login', component: LoginPage, meta: { title: 'Вход', public: true } },
      { path: '/register', component: RegisterPage, meta: { title: 'Регистрация', public: true } },
      { path: '/forgot-password', component: { template: '<div />' }, meta: { title: '', public: true } },
      { path: '/terms', component: { template: '<div />' }, meta: { title: '', public: true } },
    ],
  })
  await router.push(path)
  const session = createSession(api)
  const wrapper = mount(component, { global: { plugins: [router], provide: { [SESSION as symbol]: session } } })
  return { wrapper, router }
}

const api = (overrides: Partial<AuthApi>): AuthApi => ({
  me: vi.fn(async () => { throw new ApiError(401, 'Требуется вход') }),
  login: vi.fn(async () => user),
  register: vi.fn(async () => user),
  logout: vi.fn(async () => undefined),
  ...overrides,
})

describe('вход', () => {
  it('показывает ошибку сервера у формы', async () => {
    const { wrapper } = await mountPage(LoginPage, api({
      login: vi.fn(async () => { throw new ApiError(401, 'Неверный адрес или пароль') }),
    }), '/login')
    await wrapper.find('input[type=email]').setValue('a@b.c')
    await wrapper.find('input[type=password]').setValue('wrong password!')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.find('[role=alert]').text()).toBe('Неверный адрес или пароль')
    expect(wrapper.find('button[type=submit]').attributes('disabled')).toBeUndefined()
  })

  it('после входа возвращает туда, откуда пришли', async () => {
    const { wrapper, router } = await mountPage(LoginPage, api({}), '/login?next=/books/9')
    await wrapper.find('input[type=email]').setValue('a@b.c')
    await wrapper.find('input[type=password]').setValue('correct horse battery')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/books/9')
  })

  it('не уводит на чужой сайт', async () => {
    const { wrapper, router } = await mountPage(LoginPage, api({}), '/login?next=//evil.com')
    await wrapper.find('input[type=email]').setValue('a@b.c')
    await wrapper.find('input[type=password]').setValue('correct horse battery')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/')
  })
})

describe('регистрация', () => {
  it('создаёт аккаунт и входит', async () => {
    const a = api({})
    const { wrapper, router } = await mountPage(RegisterPage, a, '/register')
    await wrapper.find('input[type=email]').setValue('a@b.c')
    await wrapper.find('input[type=password]').setValue('correct horse battery')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(a.register).toHaveBeenCalled()
    expect(router.currentRoute.value.fullPath).toBe('/')
  })

  it('показывает требование к паролю от сервера', async () => {
    const { wrapper } = await mountPage(RegisterPage, api({
      register: vi.fn(async () => { throw new ApiError(422, 'Пароль должен содержать от 12 до 256 символов') }),
    }), '/register')
    await wrapper.find('input[type=email]').setValue('a@b.c')
    await wrapper.find('input[type=password]').setValue('short')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.find('[role=alert]').text()).toContain('от 12 до 256')
  })
})

describe('вход после сброса пароля', () => {
  it('ссылка «Забыли пароль?» и сообщение после сброса', async () => {
    const { wrapper } = await mountPage(LoginPage, api({}), '/login?reset=done')
    expect(wrapper.find('a[href="/forgot-password"]').text()).toBe('Забыли пароль?')
    expect(wrapper.find('[role=status]').text()).toBe('Пароль изменён. Войдите с новым паролем.')
  })
})

describe('вход после удаления аккаунта', () => {
  it('сообщение, что аккаунт и данные удалены', async () => {
    const { wrapper } = await mountPage(LoginPage, api({}), '/login?deleted=1')
    expect(wrapper.find('[role=status]').text()).toBe('Аккаунт и все его данные удалены.')
  })
})

describe('регистрация и условия', () => {
  it('ссылка на условия у формы регистрации', async () => {
    const { wrapper } = await mountPage(RegisterPage, api({}), '/register')
    const link = wrapper.find('a[href="/terms"]')
    expect(link.text()).toBe('условия и правила обработки данных')
  })
})
