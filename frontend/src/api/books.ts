import { ApiError, parseResponse, request } from './client'
import type { Book, Chapter, TocEntry } from './types'

const book = (id: string) => `/api/books/${encodeURIComponent(id)}`

export const listBooks = () => request<Book[]>('GET', '/api/books')

export const getBook = (id: string) => request<Book>('GET', book(id))

export const deleteBook = (id: string) => request<void>('DELETE', book(id))

export const getTOC = async (id: string) =>
  (await request<{ chapters: TocEntry[] }>('GET', `${book(id)}/toc`)).chapters

/** order — новая последовательность прежних позиций глав верхнего уровня. */
export const putTOC = async (id: string, order: number[]) =>
  (await request<{ chapters: TocEntry[] }>('PUT', `${book(id)}/toc`, { order })).chapters

export const getChapter = (id: string, index: number) => request<Chapter>('GET', `${book(id)}/chapters/${index}`)

/** Адрес картинки книги по meta.src блока image. */
export const assetUrl = (bookId: string, src: string) =>
  `${book(bookId)}/assets/${src.split('/').map(encodeURIComponent).join('/')}`

/**
 * Загрузка книги с прогрессом: fetch не сообщает, сколько отправлено,
 * поэтому XMLHttpRequest. Ошибки разбираются так же, как у request().
 */
export function uploadBook(file: File, onProgress: (fraction: number) => void): Promise<Book> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open('POST', '/api/books')
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) onProgress(e.loaded / e.total)
    }
    xhr.onload = () => {
      try {
        resolve(parseResponse<Book>(xhr.status, xhr.getResponseHeader('Content-Type'), xhr.responseText, '/api/books'))
      } catch (e) {
        reject(e)
      }
    }
    xhr.onerror = () => reject(new ApiError(0, 'Нет связи с сервером'))
    const form = new FormData()
    form.append('file', file)
    xhr.send(form)
  })
}
