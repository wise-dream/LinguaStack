// Экспорт перевода: подписи форматов и режимов, список экспортов книги с
// опросом, пока какой-нибудь собирается.
import { onBeforeUnmount, ref } from 'vue'

import { createExport, deleteExport, listBookExports } from '../api/exports'
import { errorMessage } from '../api/client'
import type { ExportFormat, ExportInput, ExportJob, ExportMode, ExportStatus } from '../api/types'
import { plural } from './format'

export const exportFormats: { id: ExportFormat; label: string }[] = [
  { id: 'epub', label: 'EPUB' },
  { id: 'markdown', label: 'Markdown' },
  { id: 'docx', label: 'Word (DOCX)' },
  { id: 'html', label: 'HTML' },
  { id: 'txt', label: 'Текст (TXT)' },
]

export const exportModes: { id: ExportMode; label: string }[] = [
  { id: 'translation', label: 'Только перевод' },
  { id: 'bilingual', label: 'Оригинал и перевод' },
]

export const formatLabel = (f: ExportFormat) => exportFormats.find((x) => x.id === f)?.label ?? f
export const modeLabel = (m: ExportMode) => exportModes.find((x) => x.id === m)?.label ?? m

const statusLabels: Record<ExportStatus, string> = {
  pending: 'В очереди',
  running: 'Собирается',
  completed: 'Готово',
  failed: 'Ошибка',
}

export const exportStatusLabel = (s: ExportStatus) => statusLabels[s] ?? s

export const isActiveExport = (j: ExportJob) => j.status === 'pending' || j.status === 'running'

/** «12 фрагментов не переведены и оставлены в оригинале». */
export function untranslatedNote(n: number): string {
  const one = n % 10 === 1 && n % 100 !== 11
  return `${n} ${plural(n, ['фрагмент', 'фрагмента', 'фрагментов'])} ` +
    (one ? 'не переведён и оставлен в оригинале' : 'не переведены и оставлены в оригинале')
}

export const POLL_MS = 1000

/**
 * Экспорты книги (или одного её перевода) с опросом, пока какой-нибудь
 * ждёт или собирается: файл готовит worker, и страница узнаёт об этом сама.
 */
export function useExports(bookId: () => string, translationId?: () => string) {
  const items = ref<ExportJob[]>([])
  const error = ref('')
  let timer: ReturnType<typeof setTimeout> | undefined
  let stopped = false

  function schedule() {
    clearTimeout(timer)
    if (!stopped && items.value.some(isActiveExport)) timer = setTimeout(() => void load(), POLL_MS)
  }

  async function load() {
    try {
      const all = await listBookExports(bookId())
      items.value = translationId ? all.filter((j) => j.translation_id === translationId()) : all
      error.value = ''
    } catch (e) {
      error.value = errorMessage(e)
    }
    schedule()
  }

  async function create(input: ExportInput): Promise<ExportJob> {
    const job = await createExport(bookId(), input)
    items.value = [job, ...items.value]
    schedule()
    return job
  }

  async function remove(id: string) {
    await deleteExport(id)
    items.value = items.value.filter((j) => j.id !== id)
  }

  onBeforeUnmount(() => {
    stopped = true
    clearTimeout(timer)
  })

  return { items, error, load, create, remove }
}
