import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { afterEach, describe, expect, it, vi } from 'vitest'

import { THEME_STORAGE_KEY } from './theme'

// public/theme-init.js выполняется до приложения и решает так же, как
// createTheme: иначе при загрузке тема мигнёт и сменится.
const script = readFileSync(resolve(import.meta.dirname, '../public/theme-init.js'), 'utf8')

function run(stored: string | null, systemDark: boolean) {
  localStorage.clear()
  if (stored !== null) localStorage.setItem(THEME_STORAGE_KEY, stored)
  vi.stubGlobal('matchMedia', (q: string) => ({ matches: q === '(prefers-color-scheme: dark)' && systemDark }))
  delete document.documentElement.dataset.theme
  new Function(script)()
  return document.documentElement.dataset.theme
}

afterEach(() => {
  vi.unstubAllGlobals()
  localStorage.clear()
})

describe('тема до загрузки приложения', () => {
  it('без выбора — как в системе', () => {
    expect(run(null, true)).toBe('dark')
    expect(run(null, false)).toBe('light')
  })

  it('сохранённый выбор важнее системы', () => {
    expect(run('light', true)).toBe('light')
    expect(run('dark', false)).toBe('dark')
  })

  it('непонятное значение — как в системе', () => {
    expect(run('sepia', true)).toBe('dark')
  })

  it('без matchMedia — светлая', () => {
    localStorage.clear()
    vi.stubGlobal('matchMedia', undefined)
    delete document.documentElement.dataset.theme
    new Function(script)()
    expect(document.documentElement.dataset.theme).toBe('light')
  })
})
