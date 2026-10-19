import { request } from './client'
import type { ExportInput, ExportJob } from './types'

const job = (id: string) => `/api/exports/${encodeURIComponent(id)}`

export const createExport = (bookId: string, input: ExportInput) =>
  request<ExportJob>('POST', `/api/books/${encodeURIComponent(bookId)}/exports`, input)

export const listBookExports = (bookId: string) =>
  request<ExportJob[]>('GET', `/api/books/${encodeURIComponent(bookId)}/exports`)

export const getExport = (id: string) => request<ExportJob>('GET', job(id))

export const deleteExport = (id: string) => request<void>('DELETE', job(id))

/** Адрес файла: обычная ссылка — браузер скачает его с cookie сессии. */
export const exportFileUrl = (id: string) => `${job(id)}/file`
