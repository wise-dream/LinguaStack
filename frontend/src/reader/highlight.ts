// Подсветка найденных слов в тексте книги через CSS Custom Highlight API:
// отметки — диапазоны поверх готового текста, DOM не меняется, и Vue нечего
// перерисовывать. Браузер без API просто не подсвечивает слова — сам блок
// всё равно отмечен как найденный.

const NAME = 'search-hit'

interface HighlightRegistry {
  set(name: string, value: unknown): void
  delete(name: string): void
}

function registry(): HighlightRegistry | null {
  const css = (globalThis as { CSS?: { highlights?: HighlightRegistry } }).CSS
  const ctor = (globalThis as { Highlight?: unknown }).Highlight
  return css?.highlights && typeof ctor === 'function' ? css.highlights : null
}

/** Выражение для поиска слов без учёта регистра; null — искать нечего. */
function wordsPattern(words: string[]): RegExp | null {
  const clean = words.map((w) => w.trim()).filter(Boolean)
  if (!clean.length) return null
  // Длинные — первыми: «кошками» не превратится в «кошк» + «ами».
  clean.sort((a, b) => b.length - a.length)
  return new RegExp(clean.map((w) => w.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('|'), 'giu')
}

/** Отрывок, разрезанный на найденное и остальное, — для <mark> без v-html. */
export function splitHits(text: string, words: string[]): { text: string; hit: boolean }[] {
  const re = wordsPattern(words)
  if (!re) return [{ text, hit: false }]
  const parts: { text: string; hit: boolean }[] = []
  let at = 0
  for (const m of text.matchAll(re)) {
    if (m.index > at) parts.push({ text: text.slice(at, m.index), hit: false })
    parts.push({ text: m[0], hit: true })
    at = m.index + m[0].length
  }
  if (at < text.length) parts.push({ text: text.slice(at), hit: false })
  return parts
}

/** Все вхождения слов в тексте узла без учёта регистра. */
export function hitRanges(root: Node, words: string[]): Range[] {
  const re = wordsPattern(words)
  if (!re) return []
  const ranges: Range[] = []
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT)
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    const text = node.nodeValue ?? ''
    for (const m of text.matchAll(re)) {
      const range = document.createRange()
      range.setStart(node, m.index)
      range.setEnd(node, m.index + m[0].length)
      ranges.push(range)
    }
  }
  return ranges
}

export function paintHits(ranges: Range[]): void {
  const reg = registry()
  if (!reg) return
  const Ctor = (globalThis as unknown as { Highlight: new (...r: Range[]) => unknown }).Highlight
  reg.set(NAME, new Ctor(...ranges))
}

export function clearHits(): void {
  registry()?.delete(NAME)
}
