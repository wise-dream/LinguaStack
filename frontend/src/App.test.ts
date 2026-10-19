import { mount, flushPromises } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { createMemoryHistory } from 'vue-router'

import App from './App.vue'
import { createAppRouter } from './router'

async function mountAt(path: string) {
  const router = createAppRouter(createMemoryHistory())
  await router.push(path)
  await router.isReady()
  const wrapper = mount(App, { global: { plugins: [router] } })
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

  it('библиотека остаётся текущим разделом на странице книги', async () => {
    const wrapper = await mountAt('/books/123')
    const current = wrapper.findAll('nav a').filter((a) => a.attributes('aria-current') === 'page')
    expect(current.map((a) => a.text())).toEqual(['Библиотека'])
  })
})
