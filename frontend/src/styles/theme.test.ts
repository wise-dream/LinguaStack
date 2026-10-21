// @vitest-environment node
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

import { palettes } from './theme'

const css = readFileSync(fileURLToPath(new URL('./tokens.css', import.meta.url)), 'utf8')

/** Объявления токенов в блоке с данным селектором. */
function block(selector: string): Record<string, string> {
  const start = css.indexOf(`${selector} {`)
  expect(start, selector).toBeGreaterThanOrEqual(0)
  const body = css.slice(start, css.indexOf('\n}', start))
  return Object.fromEntries([...body.matchAll(/--([a-z0-9-]+):\s*([^;]+);/gi)].map((m) => [m[1], m[2]!.trim().toLowerCase()]))
}

const isColor = (v: string) => /#[0-9a-f]{3,8}\b|rgba?\(/i.test(v)

// Naive UI считает оттенки наведения и нажатия из настоящих цветов, CSS-
// переменные ему не годятся — палитры темы повторяют tokens.css. Тест не
// даёт им разойтись.
describe('тема Naive UI', () => {
  it.each([
    ['light', ':root'],
    ['dark', ":root[data-theme='dark']"],
  ] as const)('%s: цвета совпадают с токенами', (name, selector) => {
    const tokens = block(selector)
    for (const [key, value] of Object.entries(palettes[name])) {
      const token = key.replace(/[A-Z]/g, (c) => `-${c.toLowerCase()}`)
      expect(tokens[token], `--${token}`).toBe(value)
    }
  })

  it('у каждого цветового токена есть тёмное значение', () => {
    const light = Object.entries(block(':root')).filter(([, v]) => isColor(v)).map(([k]) => k)
    const dark = block(":root[data-theme='dark']")
    expect(light.filter((k) => !(k in dark))).toEqual([])
  })
})
