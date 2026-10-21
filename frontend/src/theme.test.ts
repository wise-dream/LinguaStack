import { describe, expect, it } from 'vitest'

import { THEME_STORAGE_KEY, createTheme } from './theme'

function memoryStorage(initial: Record<string, string> = {}) {
  const data = new Map(Object.entries(initial))
  return {
    data,
    getItem: (k: string) => data.get(k) ?? null,
    setItem: (k: string, v: string) => void data.set(k, v),
    removeItem: (k: string) => void data.delete(k),
  }
}

function fakeQuery(matches: boolean) {
  const listeners: ((e: { matches: boolean }) => void)[] = []
  const query = {
    matches,
    addEventListener: (_type: 'change', fn: (e: { matches: boolean }) => void) => void listeners.push(fn),
    flip(next: boolean) {
      this.matches = next
      for (const fn of listeners) fn({ matches: next })
    },
  }
  return query
}

function setup(stored: Record<string, string> = {}, systemDark = false) {
  const storage = memoryStorage(stored)
  const media = fakeQuery(systemDark)
  const root = document.createElement('html')
  const theme = createTheme({ storage, media, root })
  return { theme, storage, media, root }
}

describe('тема оформления', () => {
  it('по умолчанию — как в системе', () => {
    const { theme, root } = setup({}, true)
    expect(theme.choice.value).toBe('system')
    expect(theme.dark.value).toBe(true)
    expect(root.dataset.theme).toBe('dark')
  })

  it('следит за сменой темы системы', () => {
    const { theme, media, root } = setup()
    expect(root.dataset.theme).toBe('light')
    media.flip(true)
    expect(theme.dark.value).toBe(true)
    expect(root.dataset.theme).toBe('dark')
  })

  it('выбранная тема важнее системной и запоминается', () => {
    const { theme, storage, media, root } = setup({}, true)
    theme.set('light')
    expect(root.dataset.theme).toBe('light')
    expect(storage.data.get(THEME_STORAGE_KEY)).toBe('light')
    media.flip(false)
    media.flip(true)
    expect(theme.dark.value).toBe(false)
  })

  it('читает сохранённый выбор', () => {
    const { theme, root } = setup({ [THEME_STORAGE_KEY]: 'dark' })
    expect(theme.choice.value).toBe('dark')
    expect(root.dataset.theme).toBe('dark')
  })

  it('возврат к системной теме стирает сохранённый выбор', () => {
    const { theme, storage } = setup({ [THEME_STORAGE_KEY]: 'dark' })
    theme.set('system')
    expect(storage.data.has(THEME_STORAGE_KEY)).toBe(false)
    expect(theme.dark.value).toBe(false)
  })

  it('непонятное значение в хранилище — как в системе', () => {
    const { theme } = setup({ [THEME_STORAGE_KEY]: 'sepia' }, true)
    expect(theme.choice.value).toBe('system')
    expect(theme.dark.value).toBe(true)
  })

  it('без хранилища (запрещено браузером) тема всё равно переключается', () => {
    const root = document.createElement('html')
    const broken = {
      getItem: () => { throw new Error('SecurityError') },
      setItem: () => { throw new Error('SecurityError') },
      removeItem: () => { throw new Error('SecurityError') },
    }
    const theme = createTheme({ storage: broken, media: null, root })
    expect(theme.choice.value).toBe('system')
    theme.set('dark')
    expect(root.dataset.theme).toBe('dark')
  })

  it('без поддержки prefers-color-scheme системная тема — светлая', () => {
    const root = document.createElement('html')
    const theme = createTheme({ storage: null, media: null, root })
    expect(theme.dark.value).toBe(false)
    expect(root.dataset.theme).toBe('light')
  })
})
