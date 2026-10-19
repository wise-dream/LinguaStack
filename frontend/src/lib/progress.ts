// Прогресс перевода без перезагрузки страницы (ТЗ §8.4, ADD §13): поток
// SSE, а если он раз за разом рвётся (прокси, перезапуск api) — опрос.
import { onScopeDispose, ref, watch, type Ref } from 'vue'

import { eventsUrl, getTranslation } from '../api/translations'
import type { Project, ProgressEvent, ProjectStatus } from '../api/types'

const POLL_MS = 10_000
const MAX_STREAM_ERRORS = 3

const terminal: ProjectStatus[] = ['completed', 'completed_with_warnings', 'failed', 'cancelled']

export function isTerminal(status: ProjectStatus) {
  return terminal.includes(status)
}

export function toEvent(p: Project): ProgressEvent {
  return {
    status: p.status,
    segments_total: p.segments_total,
    segments_done: p.segments_done,
    segments_failed: p.segments_failed,
    warnings_count: p.warnings_count,
    progress_percent: p.progress_percent,
    last_error: p.last_error,
    cost_usd: p.cost_usd,
    input_tokens: p.input_tokens,
    output_tokens: p.output_tokens,
    provider_calls: p.provider_calls,
  }
}

export function useProgress(projectId: Ref<string>) {
  const event = ref<ProgressEvent | null>(null)
  const mode = ref<'stream' | 'polling' | 'done'>('stream')
  let source: EventSource | null = null
  let timer: ReturnType<typeof setInterval> | undefined
  let errors = 0

  function stop() {
    source?.close()
    source = null
    clearInterval(timer)
    timer = undefined
  }

  function apply(e: ProgressEvent) {
    event.value = e
    if (isTerminal(e.status)) {
      stop()
      mode.value = 'done'
    }
  }

  async function poll(id: string) {
    try {
      apply(toEvent(await getTranslation(id)))
    } catch {
      // Следующий опрос попробует снова.
    }
  }

  function startPolling(id: string) {
    source?.close()
    source = null
    mode.value = 'polling'
    void poll(id)
    timer = setInterval(() => void poll(id), POLL_MS)
  }

  function open(id: string) {
    stop()
    errors = 0
    mode.value = 'stream'
    source = new EventSource(eventsUrl(id))
    source.onmessage = (m) => {
      errors = 0
      apply(JSON.parse(m.data) as ProgressEvent)
    }
    // EventSource переподключается сам; после нескольких неудач подряд
    // поток, видимо, режет прокси — дальше надёжнее опрашивать.
    source.onerror = () => {
      errors++
      if (errors >= MAX_STREAM_ERRORS && mode.value === 'stream') startPolling(id)
    }
  }

  /** Снова следить за проектом — после «Продолжить». */
  function restart() {
    open(projectId.value)
  }

  watch(projectId, open, { immediate: true })
  onScopeDispose(stop)

  return { event, mode, restart }
}
