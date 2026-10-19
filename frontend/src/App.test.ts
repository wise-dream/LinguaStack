import { mount, flushPromises } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { createMemoryHistory } from 'vue-router'

import App from './App.vue'
import type { User } from './api/types'
import { createAppRouter } from './router'
import { SESSION, createSession } from './session'

const user: User = { id: 'u1', email: 'reader@example.com', role: 'user', created_at: '2026-10-19T00:00:00Z' }

async function mountAt(path: string, signedIn = true) {
  const session = createSession({
    me: async () => {
      if (!signedIn) throw new Error('нет сессии')
      return user
    },
    login: async () => user,
    register: async () => user,
    logout: async () => undefined,
  })
  await session.load()
  const router = createAppRouter(createMemoryHistory())
  await router.push(path)
  await router.isReady()
  const wrapper = mount(App, { global: { plugins: [router], provide: { [SESSION as symbol]: session } } })
  await flushPromises()
  return wrapper
}

describe('оболочка приложения', () => {
  it('показывает разделы и отмечает текущий', async () => {
    const wrapper = await mountAt('/translations')
    const links = wrapper.findAll('nav a')
    expect(links.map((a) => a.text())).toEqual(['Библиотека', 'Переводы', 'Модели'])
    const current = links.filter((a) => a.attributes('aria-current') === 'page')
    expect(current.map((a) => a.text())).toEqual(['Переводы'])
  })

  it('без входа шапки с разделами нет', async () => {
    const wrapper = await mountAt('/login', false)
    expect(wrapper.find('nav').exists()).toBe(false)
  })

  it('показывает, кто вошёл', async () => {
    const wrapper = await mountAt('/')
    expect(wrapper.text()).toContain('reader@example.com')
    expect(wrapper.find('button').text()).toBe('Выйти')
  })

  it('библиотека остаётся текущим разделом на странице книги', async () => {
    const wrapper = await mountAt('/books/123')
    const current = wrapper.findAll('nav a').filter((a) => a.attributes('aria-current') === 'page')
    expect(current.map((a) => a.text())).toEqual(['Библиотека'])
  })
})
