import { inject, ref, type InjectionKey } from 'vue'
import type { Router } from 'vue-router'

import * as authApi from './api/auth'
import { ApiError, onUnauthorized } from './api/client'
import type { User } from './api/types'

export interface AuthApi {
  me(): Promise<User>
  login(email: string, password: string): Promise<User>
  register(email: string, password: string): Promise<User>
  logout(): Promise<void>
}

/** Путь для возврата после входа: только свой сайт, иначе библиотека. */
export function safeNext(raw: unknown): string {
  if (typeof raw !== 'string' || !raw.startsWith('/') || raw.startsWith('//') || raw.startsWith('/\\')) {
    return '/'
  }
  return raw
}

export function createSession(api: AuthApi = authApi) {
  const me = ref<User | null>(null)
  let known = false

  /** Узнаёт, кто вошёл; без сессии — null. */
  async function load() {
    if (known) return
    try {
      me.value = await api.me()
      known = true
    } catch (e) {
      me.value = null
      // Сервер недоступен — спросим снова при следующем переходе.
      known = e instanceof ApiError && e.status === 401
    }
  }

  async function login(email: string, password: string) {
    me.value = await api.login(email, password)
    known = true
  }

  /** API регистрации сессию не выдаёт — сразу после неё выполняется вход. */
  async function register(email: string, password: string) {
    await api.register(email, password)
    await login(email, password)
  }

  async function logout() {
    try {
      await api.logout()
    } finally {
      me.value = null
      known = true
    }
  }

  /** Страж маршрутов и реакция на истёкшую сессию. */
  function install(router: Router) {
    router.beforeEach(async (to) => {
      await load()
      if (to.meta.public) {
        return me.value && (to.path === '/login' || to.path === '/register') ? '/' : true
      }
      return me.value ? true : { path: '/login', query: { next: to.fullPath } }
    })
    onUnauthorized(() => {
      me.value = null
      known = true
      const current = router.currentRoute.value
      if (!current.meta.public) {
        void router.push({ path: '/login', query: { next: current.fullPath } })
      }
    })
  }

  return { me, load, login, register, logout, install }
}

export type Session = ReturnType<typeof createSession>

export const SESSION: InjectionKey<Session> = Symbol('session')

export const appSession = createSession()

export function useSession(): Session {
  return inject(SESSION, appSession)
}
