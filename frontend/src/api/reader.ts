import { request } from './client'
import { query } from './query'
import type { Position, SearchResponse } from './types'

const book = (id: string) => `/api/books/${encodeURIComponent(id)}`

export const searchBook = (bookId: string, q: string, translationId?: string) =>
  request<SearchResponse>('GET', `${book(bookId)}/search${query({ q, translation_id: translationId })}`)

export const getPosition = (bookId: string) => request<Position | null>('GET', `${book(bookId)}/position`)

export const putPosition = (bookId: string, position: Position) =>
  request<void>('PUT', `${book(bookId)}/position`, position, { keepalive: true })
