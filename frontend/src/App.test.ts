import { mount, flushPromises } from '@vue/test-utils'
import { NConfigProvider, NPopselect, darkTheme } from 'naive-ui'
import { describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import { createMemoryHistory } from 'vue-router'

import App from './App.vue'
import type { User } from './api/types'
import { createAppRouter } from './router'
import { SESSION, createSession } from './session'
import { themeOverrides } from './styles/theme'
import { THEME, createTheme, type Theme } from './theme'

/** Тема без браузера: не трогает <html> и localStorage общего jsdom. */
const isolatedTheme = () => createTheme({ storage: null, media: null, root: document.createElement('html') })

const user: User = { id: 'u1', email: 'reader@example.com', role: 'user', created_at: '2026-10-19T00:00:00Z' }

async function mountAt(path: string, signedIn = true, who: User = user, theme: Theme = isolatedTheme()) {
  const session = createSession({
    me: async () => {
      if (!signedIn) throw new Error('нет сессии')
      return who
    },
    login: async () => user,
    register: async () => user,
    logout: async () => undefined,
  })
  await session.load()
  const router = createAppRouter(createMemoryHistory())
  await router.push(path)
  await router.isReady()
  const wrapper = mount(App, {
    global: { plugins: [router], provide: { [SESSION as symbol]: session, [THEME as symbol]: theme } },
  })
  await flushPromises()
  return wrapper
}

describe('оболочка приложения', () => {
  it('показывает разделы и отмечает текущий', async () => {
    const wrapper = await mountAt('/translations')
    const links = wrapper.findAll('nav a')
    expect(links.map((a) => a.text())).toEqual(['Библиотека', 'Переводы', 'Модели', 'Настройки'])
    const current = links.filter((a) => a.attributes('aria-current') === 'page')
    expect(current.map((a) => a.text())).toEqual(['Переводы'])
  })

  it('настройки — свой раздел, «Модели» — отдельно', async () => {
    const wrapper = await mountAt('/settings')
    const current = wrapper.findAll('nav a').filter((a) => a.attributes('aria-current') === 'page')
    expect(current.map((a) => a.text())).toEqual(['Настройки'])
  })

  it('без входа шапки с разделами нет', async () => {
    const wrapper = await mountAt('/login', false)
    expect(wrapper.find('nav').exists()).toBe(false)
  })

  it('показывает, кто вошёл', async () => {
    const wrapper = await mountAt('/')
    expect(wrapper.text()).toContain('reader@example.com')
    expect(wrapper.findAll('button').map((b) => b.text())).toContain('Выйти')
  })

  it('библиотека остаётся текущим разделом на странице книги', async () => {
    const wrapper = await mountAt('/books/123')
    const current = wrapper.findAll('nav a').filter((a) => a.attributes('aria-current') === 'page')
    expect(current.map((a) => a.text())).toEqual(['Библиотека'])
  })
})

describe('администрирование', () => {
  it('раздел виден только администратору', async () => {
    const admin = await mountAt('/', true, { ...user, role: 'admin' })
    expect(admin.findAll('nav a').map((a) => a.text())).toContain('Администрирование')
    const plain = await mountAt('/')
    expect(plain.findAll('nav a').map((a) => a.text())).not.toContain('Администрирование')
  })
})

describe('тема оформления', () => {
  it('переключатель в шапке; Naive UI получает тёмную тему', async () => {
    const theme = isolatedTheme()
    const wrapper = await mountAt('/', true, user, theme)
    const button = wrapper.find('button.theme-switch')
    expect(button.attributes('aria-label')).toBe('Тема: как в системе')
    const provider = wrapper.findComponent(NConfigProvider)
    expect(provider.props('theme')).toBeNull()
    expect(provider.props('themeOverrides')).toBe(themeOverrides.light)

    wrapper.findComponent(NPopselect).vm.$emit('update:value', 'dark')
    await nextTick()
    expect(theme.choice.value).toBe('dark')
    expect(provider.props('theme')).toBe(darkTheme)
    expect(provider.props('themeOverrides')).toBe(themeOverrides.dark)
    expect(button.attributes('aria-label')).toBe('Тема: тёмная')
  })

  it('варианты: как в системе, светлая, тёмная', async () => {
    const wrapper = await mountAt('/')
    const options = wrapper.findComponent(NPopselect).props('options') as { label: string; value: string }[]
    expect(options.map((o) => [o.value, o.label])).toEqual([
      ['system', 'Как в системе'],
      ['light', 'Светлая'],
      ['dark', 'Тёмная'],
    ])
  })

  it('тема без входа тоже действует', async () => {
    const theme = isolatedTheme()
    theme.set('dark')
    const wrapper = await mountAt('/login', false, user, theme)
    expect(wrapper.findComponent(NConfigProvider).props('theme')).toBe(darkTheme)
  })
})

describe('меню на телефоне', () => {
  it('кнопка «Меню» раскрывает разделы и аккаунт и прячет их обратно', async () => {
    const wrapper = await mountAt('/')
    const burger = wrapper.find('button.burger')
    expect(burger.attributes('aria-label')).toBe('Меню')
    expect(burger.attributes('aria-expanded')).toBe('false')
    const menu = wrapper.find(`#${burger.attributes('aria-controls')}`)
    expect(menu.classes()).not.toContain('open')
    expect(menu.find('nav').exists()).toBe(true)
    expect(menu.text()).toContain('reader@example.com')
    expect(menu.text()).toContain('Выйти')

    await burger.trigger('click')
    expect(burger.attributes('aria-expanded')).toBe('true')
    expect(menu.classes()).toContain('open')

    await burger.trigger('click')
    expect(burger.attributes('aria-expanded')).toBe('false')
  })

  it('переход в раздел закрывает меню', async () => {
    const wrapper = await mountAt('/')
    await wrapper.find('button.burger').trigger('click')
    await wrapper.findAll('nav a').find((a) => a.text() === 'Переводы')!.trigger('click')
    await flushPromises()
    expect(wrapper.find('button.burger').attributes('aria-expanded')).toBe('false')
  })

  it('Escape закрывает меню и возвращает фокус на кнопку', async () => {
    const wrapper = await mountAt('/')
    const burger = wrapper.find('button.burger')
    const focus = vi.spyOn(burger.element as HTMLButtonElement, 'focus')
    await burger.trigger('click')
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await nextTick()
    expect(burger.attributes('aria-expanded')).toBe('false')
    expect(focus).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('нажатие мимо меню закрывает его', async () => {
    const wrapper = await mountAt('/')
    expect(wrapper.find('.scrim').exists()).toBe(false)
    await wrapper.find('button.burger').trigger('click')
    await wrapper.find('.scrim').trigger('click')
    expect(wrapper.find('button.burger').attributes('aria-expanded')).toBe('false')
  })
})

describe('условия и конфиденциальность', () => {
  it('открываются без входа — их читают до регистрации', async () => {
    const wrapper = await mountAt('/terms', false)
    expect(wrapper.find('h1').text()).toBe('Условия и конфиденциальность')
    expect(wrapper.find('nav').exists()).toBe(false)
    // Шапки нет — путь обратно даёт название сервиса.
    expect(wrapper.find('main a.back').attributes('href')).toBe('/')
  })

  it('и после входа — с шапкой', async () => {
    const wrapper = await mountAt('/terms')
    expect(wrapper.find('h1').text()).toBe('Условия и конфиденциальность')
    expect(wrapper.find('nav').exists()).toBe(true)
    expect(wrapper.find('main a.back').exists()).toBe(false)
  })
})
