import { describe, expect, it } from 'vitest'

import { capitalize, formatBytes, formatDate, formatDuration, formatNumber, languageName, plural, sourceLanguageName, statusLabel } from './format'

describe('форматирование', () => {
  it('названия языков по-русски', () => {
    expect(languageName('en')).toBe('английский')
    expect(languageName('ru')).toBe('русский')
    expect(languageName('auto')).toBe('определить автоматически')
  })

  it('длительность', () => {
    expect(formatDuration(12_000)).toBe('12 с')
    expect(formatDuration(252_000)).toBe('4 мин 12 с')
    expect(formatDuration(3 * 3_600_000 + 5 * 60_000)).toBe('3 ч 05 мин')
    expect(formatDuration(-5)).toBe('0 с')
  })

  it('дата, размер и числа', () => {
    expect(formatDate('2026-10-16T19:31:28Z')).toBe('16 окт. 2026 г.')
    // Разряды и единицы отделены неразрывным пробелом (так требует
    // типографика) — сравниваем с обычным.
    const plain = (s: string) => s.replace(/\s/g, ' ')
    expect(plain(formatBytes(189_249))).toBe('184,8 КБ')
    expect(plain(formatBytes(512))).toBe('512 Б')
    expect(plain(formatNumber(1234567))).toBe('1 234 567')
  })

  it('состояния перевода', () => {
    expect(statusLabel('completed_with_warnings')).toBe('Готово, есть замечания')
    expect(statusLabel('running')).toBe('Переводится')
  })

  it('склонение по числу', () => {
    const forms = ['глава', 'главы', 'глав'] as const
    expect([1, 2, 5, 11, 12, 21, 22, 25, 111].map((n) => plural(n, forms))).toEqual([
      'глава', 'главы', 'глав', 'глав', 'глав', 'глава', 'главы', 'глав', 'глав',
    ])
  })

  it('язык оригинала при автоопределении — «исходный язык», в списках — с заглавной', () => {
    expect(sourceLanguageName('auto')).toBe('исходный язык')
    expect(sourceLanguageName('en')).toBe('английский')
    expect(capitalize('английский')).toBe('Английский')
    expect(capitalize('')).toBe('')
  })
})
