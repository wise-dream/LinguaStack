import { request } from './client'
import { query } from './query'
import type {
  Project,
  Revision,
  Segment,
  SegmentStatus,
  Severity,
  TranslationInput,
  TranslationOptions,
} from './types'

const project = (id: string) => `/api/translations/${encodeURIComponent(id)}`
const segment = (id: string) => `/api/segments/${encodeURIComponent(id)}`

export const getTranslationOptions = () => request<TranslationOptions>('GET', '/api/translation-options')

export const listTranslations = (bookId?: string) =>
  request<Project[]>('GET', `/api/translations${query({ book_id: bookId })}`)

export const getTranslation = (id: string) => request<Project>('GET', project(id))

export const createTranslation = (bookId: string, input: TranslationInput) =>
  request<Project>('POST', `/api/books/${encodeURIComponent(bookId)}/translations`, input)

export const pauseTranslation = (id: string) => request<Project>('POST', `${project(id)}/pause`)
export const resumeTranslation = (id: string) => request<Project>('POST', `${project(id)}/resume`)
export const cancelTranslation = (id: string) => request<Project>('POST', `${project(id)}/cancel`)
/** Заново — все фрагменты с ошибками и с предупреждениями. */
export const retryProblems = (id: string) =>
  request<{ queued: number; project: Project }>('POST', `${project(id)}/retry-problems`)

export interface SegmentFilter {
  chapter?: number
  status?: SegmentStatus
  severity?: Severity
  /** Текст в оригинале или переводе — правила как у поиска в читалке. */
  q?: string
  limit?: number
  offset?: number
}

export const listSegments = (id: string, f: SegmentFilter) =>
  request<Segment[]>(
    'GET',
    `${project(id)}/segments${query({ chapter: f.chapter, status: f.status, severity: f.severity, q: f.q, limit: f.limit, offset: f.offset })}`,
  )

export const patchSegment = (id: string, translatedText: string) =>
  request<Segment>('PATCH', segment(id), { translated_text: translatedText })

export const retrySegment = (id: string) => request<Segment>('POST', `${segment(id)}/retry`)

export const segmentRevisions = (id: string) => request<Revision[]>('GET', `${segment(id)}/revisions`)

export const eventsUrl = (id: string) => `${project(id)}/events`
