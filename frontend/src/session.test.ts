import { flushPromises } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { ApiError, request } from './api/client'
import type { User } from './api/types'
import { createSession, safeNext, type AuthApi } from './session'

const user: User = { id: 'u1', email: 'a@b.c', role: 'user', created_at: '2026-10-19T00:00:00Z' }

function fakeApi(signedIn: boolean): AuthApi {
  return {
    me: vi.fn(async () => {
      if (!signedIn) throw new ApiError(401, 'Требуется вход')
      return user
    }),
    login: vi.fn(async () => user),
    register: vi.fn(async () => user),
    logout: vi.fn(async () => undefined),
  }
}

function routerWith(session: ReturnType<typeof createSession>) {
  const page = { template: '<div />' }
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: page, meta: { title: 'Библиотека' } },
      { path: '/books/:id', component: page, meta: { title: 'Книга' } },
      { path: '/login', component: page, meta: { title: 'Вход', public: true } },
    ],
  })
  session.install(router)
  return router
}

describe('сессия', () => {
  it('без входа страница ведёт на вход с возвратом', async () => {
    const router = routerWith(createSession(fakeApi(false)))
    await router.push('/books/1?ch=2')
    expect(router.currentRoute.value.path).toBe('/login')
    expect(router.currentRoute.value.query.next).toBe('/books/1?ch=2')
  })

  it('вошедший пользователь со страницы входа уходит в библиотеку', async () => {
    const router = routerWith(createSession(fakeApi(true)))
    await router.push('/login')
    expect(router.currentRoute.value.path).toBe('/')
  })

  it('истёкшая сессия посреди работы — на вход с возвратом (Review Focus №2)', async () => {
    const session = createSession(fakeApi(true))
    const router = routerWith(session)
    await router.push('/books/7')
    expect(session.me.value).toEqual(user)

    vi.stubGlobal('fetch', vi.fn(async () => new Response('{"error":"Требуется вход"}', {
      status: 401, headers: { 'Content-Type': 'application/json' },
    })))
    await request('GET', '/api/books/7').catch(() => undefined)
    await flushPromises()
    vi.unstubAllGlobals()

    expect(session.me.value).toBeNull()
    expect(router.currentRoute.value.path).toBe('/login')
    expect(router.currentRoute.value.query.next).toBe('/books/7')
  })

  it('проверка сессии без входа не считается истёкшей сессией', async () => {
    // Сквозной тест: /register уводил на /login — 401 от /api/me при
    // первой проверке срабатывал как «сессия истекла».
    vi.stubGlobal('fetch', vi.fn(async () => new Response('{"error":"Требуется вход"}', {
      status: 401, headers: { 'Content-Type': 'application/json' },
    })))
    const session = createSession({
      me: () => request<User>('GET', '/api/me'),
      login: async () => user,
      register: async () => user,
      logout: async () => undefined,
    })
    const page = { template: '<div />' }
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/', component: page, meta: { title: 'Библиотека' } },
        { path: '/login', component: page, meta: { title: 'Вход', public: true } },
        { path: '/register', component: page, meta: { title: 'Регистрация', public: true } },
      ],
    })
    session.install(router)
    await router.push('/register')
    await flushPromises()
    vi.unstubAllGlobals()
    expect(router.currentRoute.value.path).toBe('/register')
  })

  it('регистрация сразу выполняет вход: API регистрации сессию не выдаёт', async () => {
    const api = fakeApi(false)
    const session = createSession(api)
    await session.register('a@b.c', 'correct horse battery')
    expect(api.register).toHaveBeenCalledWith('a@b.c', 'correct horse battery')
    expect(api.login).toHaveBeenCalledWith('a@b.c', 'correct horse battery')
    expect(session.me.value).toEqual(user)
  })

  it('выход очищает пользователя', async () => {
    const session = createSession(fakeApi(true))
    await session.load()
    await session.logout()
    expect(session.me.value).toBeNull()
  })
})

describe('safeNext', () => {
  it('пускает только пути своего сайта', () => {
    expect(safeNext('/books/1?ch=2')).toBe('/books/1?ch=2')
    expect(safeNext('//evil.com')).toBe('/')
    expect(safeNext('/\\evil.com')).toBe('/')
    expect(safeNext('https://evil.com')).toBe('/')
    expect(safeNext(undefined)).toBe('/')
    expect(safeNext(['/a'])).toBe('/')
  })
})
