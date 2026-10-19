import { describe, expect, it } from 'vitest'

import { renderInline } from './inline'

describe('renderInline — текст книги в DOM (Review Focus №1)', () => {
  it('HTML из книги — только текст', () => {
    expect(renderInline('<script>alert(1)</script>')).toBe('&lt;script&gt;alert(1)&lt;/script&gt;')
    const img = renderInline('<img src=x onerror=alert(1)>')
    expect(img).not.toContain('<img')
    expect(img).toContain('&lt;img')
  })

  it('ссылка с опасной схемой — только её текст', () => {
    for (const url of ['javascript:alert(1)', 'JavaScript:alert(1)', 'data:text/html,<b>x</b>', 'vbscript:x']) {
      const html = renderInline(`[жми](${url})`)
      expect(html).not.toContain('<a')
      expect(html).toContain('жми')
    }
  })

  it('кавычка в адресе не выходит из атрибута', () => {
    const html = renderInline('[a](https://e.com/?q="x"onclick=alert(1))')
    expect(html).not.toMatch(/href="[^"]*"onclick/)
    expect(html).toContain('&quot;')
  })

  it('внешняя ссылка открывается в новой вкладке без доступа к окну', () => {
    expect(renderInline('Читайте [руководство](https://example.com/guide).')).toBe(
      'Читайте <a href="https://example.com/guide" rel="noopener noreferrer" target="_blank">руководство</a>.',
    )
  })

  it('относительная ссылка книги — текстом', () => {
    expect(renderInline('[дальше](chapter2.xhtml#start)')).toBe('дальше')
  })

  it('курсив, жирный и код; внутри кода разметка не работает', () => {
    expect(renderInline('*курсив* и **жирный** и `код *не курсив*`')).toBe(
      '<em>курсив</em> и <strong>жирный</strong> и <code>код *не курсив*</code>',
    )
    expect(renderInline('_тоже курсив_')).toBe('<em>тоже курсив</em>')
  })

  it('snake_case и одиночные звёздочки — как есть', () => {
    expect(renderInline('snake_case_name и 2 * 3 = 6')).toBe('snake_case_name и 2 * 3 = 6')
  })

  it('экранированные символы Markdown — буквально', () => {
    expect(renderInline('\\*      \\*      \\*')).toBe('*      *      *')
    expect(renderInline('\\[не ссылка\\](x)')).toBe('[не ссылка](x)')
  })
})
