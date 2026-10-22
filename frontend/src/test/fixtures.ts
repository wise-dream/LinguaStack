import type { Book, Project, ProviderConfig, ProviderKind } from '../api/types'

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
    owned: true,
    ...overrides,
  }
}

export function project(overrides: Partial<Project> = {}): Project {
  return {
    owned: true,
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


export const kinds: ProviderKind[] = [
  { provider_id: 'ollama', name: 'Ollama (локальные модели)', sends_data_externally: false, requires_api_key: false, requires_base_url: false, user_base_url: false },
  { provider_id: 'anthropic', name: 'Anthropic Claude', sends_data_externally: true, requires_api_key: true, requires_base_url: false, user_base_url: false },
  { provider_id: 'openai_compatible', name: 'OpenAI-совместимый API', sends_data_externally: true, requires_api_key: false, requires_base_url: true, user_base_url: true },
]

export function providerConfig(overrides: Partial<ProviderConfig> = {}): ProviderConfig {
  return {
    id: 'c1',
    provider_id: 'ollama',
    name: 'Локальный Ollama',
    has_api_key: false,
    key_fingerprint: null,
    key_last4: null,
    allow_external: false,
    sends_data_externally: false,
    created_at: '2026-10-16T19:00:00Z',
    ...overrides,
  }
}

export const options = {
  languages: [
    { code: 'de', name: 'German' },
    { code: 'en', name: 'English' },
    { code: 'fr', name: 'French' },
    { code: 'ru', name: 'Russian' },
  ],
  styles: ['literary', 'neutral', 'simplified', 'technical'],
  determinism: ['strict', 'balanced', 'creative'],
}

export function exportJob(overrides: Partial<import('../api/types').ExportJob> = {}): import('../api/types').ExportJob {
  return {
    id: 'e1',
    book_id: 'b1',
    translation_id: 'p1',
    format: 'epub',
    mode: 'translation',
    status: 'completed',
    file_name: 'Приключения Алисы в Стране чудес (ru).epub',
    size_bytes: 1258291,
    untranslated: 0,
    created_at: '2026-10-19T10:00:00Z',
    finished_at: '2026-10-19T10:00:02Z',
    ...overrides,
  }
}
