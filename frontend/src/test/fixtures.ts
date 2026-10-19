import type { Book, Project } from '../api/types'

export function book(overrides: Partial<Book> = {}): Book {
  return {
    id: 'b1',
    title: 'Приключения Алисы в Стране чудес',
    author: 'Льюис Кэрролл',
    language: 'en',
    source_format: 'epub',
    original_filename: 'alice.epub',
    original_size: 189249,
    status: 'imported',
    chapters: 15,
    blocks: 878,
    translatable: 873,
    issues: [],
    created_at: '2026-10-16T19:31:28Z',
    ...overrides,
  }
}

export function project(overrides: Partial<Project> = {}): Project {
  return {
    id: 'p1',
    book_id: 'b1',
    book_title: 'Приключения Алисы в Стране чудес',
    provider_config_id: 'c1',
    source_lang: 'en',
    target_lang: 'ru',
    model_id: 'qwen3:8b',
    style: 'literary',
    determinism: 'strict',
    glossary: {},
    status: 'completed',
    segments_total: 873,
    segments_done: 873,
    segments_failed: 0,
    warnings_count: 0,
    progress_percent: 100,
    sends_data_externally: false,
    created_at: '2026-10-16T20:00:00Z',
    started_at: '2026-10-16T20:00:01Z',
    finished_at: '2026-10-16T23:00:01Z',
    cost_usd: 0,
    input_tokens: 0,
    output_tokens: 0,
    provider_calls: 0,
    ...overrides,
  }
}
