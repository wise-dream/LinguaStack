import { afterEach, describe, expect, it, vi } from 'vitest'

import { clearHits, hitRanges, paintHits } from './highlight'

function el(html: string): HTMLElement {
  const div = document.createElement('div')
  // Разметка теста — своя, не из книги.
  div.innerHTML = html
  return div
}

afterEach(() => vi.unstubAllGlobals())

describe('подсветка найденного в тексте', () => {
  it('находит слова без учёта регистра во всех текстовых узлах', () => {
    const root = el('<p>Кошки спали. <em>кошки</em> и коты</p>')
    expect(hitRanges(root, ['кошки']).map((r) => r.toString())).toEqual(['Кошки', 'кошки'])
  })

  it('спецсимволы в найденном — буквально', () => {
    const root = el('<code>fmt.Println(x)</code> Println')
    expect(hitRanges(root, ['rintln(']).map((r) => r.toString())).toEqual(['rintln('])
  })

  it('пустые слова не подсвечивают всё подряд', () => {
    expect(hitRanges(el('<p>текст</p>'), ['', '  '])).toEqual([])
  })

  it('кладёт отметки в CSS.highlights, если браузер умеет', () => {
    const highlights = new Map<string, unknown>()
    vi.stubGlobal('CSS', { ...globalThis.CSS, highlights })
    vi.stubGlobal('Highlight', class {
      ranges: Range[]
      constructor(...ranges: Range[]) {
        this.ranges = ranges
      }
    })
    const ranges = hitRanges(el('<p>Кошки</p>'), ['кошки'])
    paintHits(ranges)
    expect((highlights.get('search-hit') as { ranges: Range[] }).ranges).toEqual(ranges)
    clearHits()
    expect(highlights.has('search-hit')).toBe(false)
  })

  it('без поддержки браузера — молча ничего', () => {
    vi.stubGlobal('CSS', { escape: (s: string) => s })
    expect(() => paintHits(hitRanges(el('<p>Кошки</p>'), ['кошки']))).not.toThrow()
    expect(() => clearHits()).not.toThrow()
  })
})
