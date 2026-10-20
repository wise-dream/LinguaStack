// @vitest-environment node
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

import { palette } from './theme'

// Naive UI считает оттенки наведения и нажатия из настоящих цветов, CSS-
// переменные ему не годятся — палитра темы повторяет tokens.css. Тест не
// даёт им разойтись.
describe('тема Naive UI', () => {
  it('цвета совпадают с токенами', () => {
    const css = readFileSync(fileURLToPath(new URL('./tokens.css', import.meta.url)), 'utf8')
    const tokens = Object.fromEntries([...css.matchAll(/--([a-z-]+):\s*(#[0-9a-f]{6})\s*;/gi)].map((m) => [m[1], m[2]!.toLowerCase()]))
    for (const [name, value] of Object.entries(palette)) {
      const token = name.replace(/[A-Z]/g, (c) => `-${c.toLowerCase()}`)
      expect(tokens[token], `--${token}`).toBe(value)
    }
  })
})
