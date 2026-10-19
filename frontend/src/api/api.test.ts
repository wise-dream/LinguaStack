import { afterEach, describe, expect, it, vi } from 'vitest'

import { assetUrl, getChapter } from './books'
import { searchBook } from './reader'
import { listSegments, listTranslations } from './translations'

function capture() {
  const fetchMock = vi.fn(async () => new Response('[]', { status: 200, headers: { 'Content-Type': 'application/json' } }))
  vi.stubGlobal('fetch', fetchMock)
  return () => (fetchMock.mock.calls.at(-1) as unknown as [string])[0]
}

afterEach(() => vi.unstubAllGlobals())

describe('адреса API', () => {
  it('фильтры сегментов — в query, пустые не передаются', async () => {
    const lastURL = capture()
    await listSegments('p1', { chapter: 0, status: 'failed', limit: 1000, offset: 2000 })
    expect(lastURL()).toBe('/api/translations/p1/segments?chapter=0&status=failed&limit=1000&offset=2000')
    await listSegments('p1', {})
    expect(lastURL()).toBe('/api/translations/p1/segments')
  })

  it('переводы книги', async () => {
    const lastURL = capture()
    await listTranslations('b 1')
    expect(lastURL()).toBe('/api/translations?book_id=b+1')
    await listTranslations()
    expect(lastURL()).toBe('/api/translations')
  })

  it('поиск кодирует запрос', async () => {
    const lastURL = capture()
    await searchBook('b1', 'Алиса & кот', 't1')
    expect(lastURL()).toBe('/api/books/b1/search?q=%D0%90%D0%BB%D0%B8%D1%81%D0%B0+%26+%D0%BA%D0%BE%D1%82&translation_id=t1')
  })

  it('глава и ресурс', async () => {
    const lastURL = capture()
    await getChapter('b1', 3)
    expect(lastURL()).toBe('/api/books/b1/chapters/3')
    // Сегменты пути кодируются по одному: косая черта остаётся разделителем.
    expect(assetUrl('b1', 'OEBPS/images/обложка 1.jpg')).toBe('/api/books/b1/assets/OEBPS/images/%D0%BE%D0%B1%D0%BB%D0%BE%D0%B6%D0%BA%D0%B0%201.jpg')
  })
})
