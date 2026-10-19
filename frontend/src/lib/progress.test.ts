import { flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { effectScope, ref } from 'vue'

import { stubApi } from '../test/fakeApi'
import { stubEventSource } from '../test/fakeEventSource'
import { project } from '../test/fixtures'
import { useProgress } from './progress'

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

function start(id = 'p1') {
  const scope = effectScope()
  const progress = scope.run(() => useProgress(ref(id)))!
  return { progress, scope }
}

const running = { ...project({ status: 'running', segments_done: 10 }) }

describe('прогресс перевода', () => {
  it('событие потока обновляет прогресс', () => {
    const streams = stubEventSource()
    const { progress } = start()
    expect(streams[0]?.url).toBe('/api/translations/p1/events')
    streams[0]?.emit({ ...running, segments_done: 11 })
    expect(progress.event.value?.segments_done).toBe(11)
    expect(progress.mode.value).toBe('stream')
  })

  it('терминальный статус закрывает поток', () => {
    const streams = stubEventSource()
    const { progress } = start()
    streams[0]?.emit({ ...running, status: 'completed', segments_done: 873 })
    expect(streams[0]?.closed).toBe(true)
    expect(progress.mode.value).toBe('done')
  })

  it('три ошибки потока подряд — опрос раз в 10 с (Review Focus №4)', async () => {
    vi.useFakeTimers()
    const streams = stubEventSource()
    const calls = stubApi({ 'GET /api/translations/p1': { body: { ...running, segments_done: 20 } } })
    const { progress } = start()
    streams[0]?.fail()
    streams[0]?.fail()
    expect(progress.mode.value).toBe('stream')
    streams[0]?.fail()
    expect(progress.mode.value).toBe('polling')
    expect(streams[0]?.closed).toBe(true)
    await flushPromises()
    expect(progress.event.value?.segments_done).toBe(20)

    const before = calls.length
    await vi.advanceTimersByTimeAsync(10_000)
    expect(calls.length).toBe(before + 1)
  })

  it('событие между ошибками сбрасывает счёт', () => {
    const streams = stubEventSource()
    const { progress } = start()
    streams[0]?.fail()
    streams[0]?.fail()
    streams[0]?.emit(running)
    streams[0]?.fail()
    streams[0]?.fail()
    expect(progress.mode.value).toBe('stream')
  })

  it('уход со страницы закрывает поток', () => {
    const streams = stubEventSource()
    const { scope } = start()
    scope.stop()
    expect(streams[0]?.closed).toBe(true)
  })
})
