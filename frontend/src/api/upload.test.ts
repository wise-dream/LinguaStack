import { afterEach, describe, expect, it, vi } from 'vitest'

import { uploadBook } from './books'

// Подмена XMLHttpRequest: событие прогресса и ответ сервера.
function fakeXHR(status: number, body: string, contentType = 'application/json', networkError = false) {
  const sent: { url?: string; method?: string; body?: unknown } = {}
  class FakeXHR {
    status = 0
    responseText = ''
    upload = { onprogress: null as ((e: { lengthComputable: boolean; loaded: number; total: number }) => void) | null }
    onload: (() => void) | null = null
    onerror: (() => void) | null = null
    open(method: string, url: string) {
      sent.method = method
      sent.url = url
    }
    getResponseHeader() {
      return contentType
    }
    send(form: FormData) {
      sent.body = form
      this.upload.onprogress?.({ lengthComputable: true, loaded: 50, total: 100 })
      if (networkError) {
        this.onerror?.()
        return
      }
      this.status = status
      this.responseText = body
      this.onload?.()
    }
  }
  vi.stubGlobal('XMLHttpRequest', FakeXHR)
  return sent
}

afterEach(() => vi.unstubAllGlobals())

describe('загрузка книги', () => {
  it('сообщает прогресс и отдаёт книгу', async () => {
    const sent = fakeXHR(201, '{"id":"b1","title":"Алиса"}')
    const progress: number[] = []
    const got = await uploadBook(new File(['x'], 'alice.epub'), (f) => progress.push(f))
    expect(got).toMatchObject({ id: 'b1' })
    expect(progress).toEqual([0.5])
    expect(sent.method).toBe('POST')
    expect(sent.url).toBe('/api/books')
    expect((sent.body as FormData).get('file')).toBeInstanceOf(File)
  })

  it('отказ сервера — ApiError с его текстом', async () => {
    fakeXHR(413, '{"error":"Файл больше 50 МБ"}')
    await expect(uploadBook(new File(['x'], 'big.epub'), () => undefined)).rejects.toMatchObject({
      status: 413,
      message: 'Файл больше 50 МБ',
    })
  })

  it('обрыв сети — понятная ошибка', async () => {
    fakeXHR(0, '', 'text/plain', true)
    await expect(uploadBook(new File(['x'], 'a.epub'), () => undefined)).rejects.toMatchObject({
      status: 0,
      message: 'Нет связи с сервером',
    })
  })
})
