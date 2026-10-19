// Типы ответов API — по структурам Go (backend/internal/httpapi).

export interface User {
  id: string
  email: string
  role: 'user' | 'admin'
  created_at: string
}

export type Severity = 'ok' | 'info' | 'warning' | 'error'

export interface ImportIssue {
  severity: 'info' | 'warning' | 'error'
  code: string
  message: string
  node_id?: string
}

export interface Book {
  id: string
  title: string
  author?: string
  language?: string
  source_format: string
  original_filename: string
  original_size: number
  status: string
  chapters: number
  blocks: number
  translatable: number
  issues: ImportIssue[]
  created_at: string
}

export interface TocEntry {
  index: number
  title: string
  children?: TocEntry[]
}

export type BlockType =
  | 'heading'
  | 'paragraph'
  | 'list_item'
  | 'table_cell'
  | 'caption'
  | 'blockquote'
  | 'code_block'
  | 'math_block'
  | 'image'
  | 'thematic_break'
  | 'raw_html'

export interface Block {
  node_id: string
  type: BlockType
  text: string
  level?: number
  source_hash: string
  meta?: Record<string, string>
}

export interface Chapter {
  index: number
  title: string
  blocks: Block[]
}

export type ProjectStatus =
  | 'draft'
  | 'queued'
  | 'running'
  | 'paused'
  | 'completed'
  | 'completed_with_warnings'
  | 'failed'
  | 'cancelled'

export interface Usage {
  cost_usd: number
  input_tokens: number
  output_tokens: number
  provider_calls: number
}

export interface Project extends Usage {
  id: string
  book_id: string
  book_title: string
  provider_config_id: string
  source_lang: string
  target_lang: string
  model_id: string
  style: string
  determinism: Determinism
  glossary: Record<string, string>
  status: ProjectStatus
  segments_total: number
  segments_done: number
  segments_failed: number
  warnings_count: number
  progress_percent: number
  last_error?: string
  sends_data_externally: boolean
  created_at: string
  started_at?: string
  finished_at?: string
}

/** Событие потока прогресса /api/translations/{id}/events. */
export interface ProgressEvent extends Usage {
  status: ProjectStatus
  segments_total: number
  segments_done: number
  segments_failed: number
  warnings_count: number
  progress_percent: number
  last_error?: string
}

export interface Finding {
  code: string
  severity: Severity
  message: string
}

export type SegmentStatus = 'pending' | 'running' | 'completed' | 'failed'

export interface Segment {
  id: string
  node_id: string
  chapter_index: number
  ordinal: number
  block_type: BlockType
  source_text: string
  translated_text: string | null
  status: SegmentStatus
  severity: Severity
  findings: Finding[]
  attempts: number
  last_error?: string
  updated_at: string
}

export interface Revision {
  id: string
  translated_text: string
  origin: string
  created_at: string
}

export type Determinism = 'strict' | 'balanced' | 'creative'

export interface TranslationOptions {
  languages: { code: string; name: string }[]
  styles: string[]
  determinism: Determinism[]
}

export interface TranslationInput {
  source_lang: string
  target_lang: string
  provider_config_id: string
  model_id: string
  style: string
  determinism: Determinism
  glossary: Record<string, string>
}

export interface ProviderKind {
  provider_id: string
  name: string
  sends_data_externally: boolean
  requires_api_key: boolean
  requires_base_url: boolean
  user_base_url: boolean
}

export interface ProviderConfig {
  id: string
  provider_id: string
  name: string
  base_url?: string
  has_api_key: boolean
  key_fingerprint: string | null
  key_last4: string | null
  allow_external: boolean
  sends_data_externally: boolean
  created_at: string
}

export interface ProviderConfigInput {
  provider_id: string
  name: string
  base_url?: string
  api_key?: string
  allow_external: boolean
}

export interface ModelCaps {
  model_id: string
  display_name?: string
  context_tokens?: number
  max_output_tokens?: number
  supports_temperature: boolean
  supports_system_prompt: boolean
  supported_languages?: string[]
  input_cost_per_mtok?: number
  output_cost_per_mtok?: number
}

export interface Health {
  available: boolean
  detail?: string
}

export type ReadMode = 'parallel' | 'original' | 'translation'

export interface Position {
  chapter_index: number
  node_id: string
  translation_id: string | null
  mode: ReadMode
}

export interface SearchResult {
  chapter_index: number
  node_id: string
  in: 'original' | 'translation'
  snippet: string
}

export interface SearchResponse {
  results: SearchResult[]
  truncated: boolean
}

export type ExportFormat = 'epub' | 'markdown' | 'docx' | 'html' | 'txt'
export type ExportMode = 'translation' | 'bilingual'
export type ExportStatus = 'pending' | 'running' | 'completed' | 'failed'

export interface ExportJob {
  id: string
  book_id: string
  translation_id: string
  format: ExportFormat
  mode: ExportMode
  status: ExportStatus
  file_name: string
  size_bytes: number
  untranslated: number
  error?: string
  created_at: string
  finished_at?: string
}

export interface ExportInput {
  translation_id: string
  format: ExportFormat
  mode: ExportMode
}
