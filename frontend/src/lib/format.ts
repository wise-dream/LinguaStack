import type { ProjectStatus } from '../api/types'

// Неразрывный пробел: число и единица не разрываются переносом.
const NBSP = String.fromCharCode(0xa0)

const languages = new Intl.DisplayNames(['ru'], { type: 'language' })
const dates = new Intl.DateTimeFormat('ru', { day: 'numeric', month: 'short', year: 'numeric' })
const numbers = new Intl.NumberFormat('ru')
const decimals = new Intl.NumberFormat('ru', { maximumFractionDigits: 1 })

/** Название языка по коду ISO 639-1, как пишут по-русски: «английский». */
export function languageName(code: string): string {
  if (code === 'auto' || code === '') return 'определить автоматически'
  try {
    return languages.of(code) ?? code
  } catch {
    return code
  }
}

/** Язык оригинала в тексте: при автоопределении — «исходный язык». */
export function sourceLanguageName(code: string): string {
  return code === 'auto' || code === '' ? 'исходный язык' : languageName(code)
}

/** Первая буква — заглавная: для пунктов списков («Английский»). */
export function capitalize(s: string): string {
  return s ? s[0]!.toUpperCase() + s.slice(1) : s
}

export function formatDate(iso: string): string {
  return dates.format(new Date(iso))
}

export function formatNumber(n: number): string {
  return numbers.format(n)
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${n}${NBSP}Б`
  if (n < 1024 * 1024) return `${decimals.format(n / 1024)}${NBSP}КБ`
  if (n < 1024 ** 3) return `${decimals.format(n / 1024 / 1024)}${NBSP}МБ`
  return `${decimals.format(n / 1024 ** 3)}${NBSP}ГБ`
}

/** «12 с», «4 мин 12 с», «3 ч 05 мин». */
export function formatDuration(ms: number): string {
  const total = Math.max(0, Math.round(ms / 1000))
  const h = Math.floor(total / 3600)
  const m = Math.floor((total % 3600) / 60)
  const s = total % 60
  if (h > 0) return `${h} ч ${String(m).padStart(2, '0')} мин`
  if (m > 0) return `${m} мин ${s} с`
  return `${s} с`
}

const statusLabels: Record<ProjectStatus, string> = {
  draft: 'Черновик',
  queued: 'В очереди',
  running: 'Переводится',
  paused: 'Приостановлен',
  completed: 'Готово',
  completed_with_warnings: 'Готово, есть замечания',
  failed: 'Ошибка',
  cancelled: 'Отменён',
}

export function statusLabel(status: ProjectStatus): string {
  return statusLabels[status] ?? status
}

const formats: Record<string, string> = {
  epub: 'EPUB',
  markdown: 'Markdown',
  txt: 'Текст',
  zip_repo: 'Документация',
  pdf: 'PDF',
  docx: 'Word (DOCX)',
  html: 'HTML',
}

export function formatName(sourceFormat: string): string {
  return formats[sourceFormat] ?? sourceFormat
}

/** Форма слова по числу: plural(5, ['глава', 'главы', 'глав']) — «глав». */
export function plural(n: number, forms: readonly [string, string, string]): string {
  const mod10 = n % 10
  const mod100 = n % 100
  if (mod10 === 1 && mod100 !== 11) return forms[0]
  if (mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14)) return forms[1]
  return forms[2]
}

/** Стоимость в долларах: меньше доллара — с четырьмя знаками (запросы к моделям стоят центы). */
export function formatCost(usd: number): string {
  return `$${usd < 1 ? usd.toFixed(4) : usd.toFixed(2)}`
}
