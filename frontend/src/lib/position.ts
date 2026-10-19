import { listSegments } from '../api/translations'
import type { Position, Segment } from '../api/types'

const SEGMENT_PAGE = 1000

/** Все сегменты главы: страница API — 1000, глава бывает больше. */
export async function fetchChapterSegments(projectId: string, chapter: number): Promise<Segment[]> {
  const out: Segment[] = []
  for (let offset = 0; ; offset += SEGMENT_PAGE) {
    const page = await listSegments(projectId, { chapter, limit: SEGMENT_PAGE, offset })
    out.push(...page)
    if (page.length < SEGMENT_PAGE) return out
  }
}

/**
 * Сохранение позиции чтения: не чаще раза в delayMs (прокрутка даёт
 * десятки событий в секунду) и немедленно при уходе со страницы.
 */
export function usePositionSaver(save: (p: Position) => Promise<void>, delayMs = 1500) {
  let pending: Position | null = null
  let timer: ReturnType<typeof setTimeout> | undefined

  function flush() {
    clearTimeout(timer)
    timer = undefined
    if (!pending) return
    const p = pending
    pending = null
    void save(p).catch(() => undefined) // позиция — удобство: ошибка не мешает читать
  }

  function schedule(p: Position) {
    pending = p
    clearTimeout(timer)
    timer = setTimeout(flush, delayMs)
  }

  return { schedule, flush }
}
