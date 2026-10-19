import { afterEach, describe, expect, it, vi } from 'vitest'

import type { Position } from '../api/types'
import { stubApi } from '../test/fakeApi'
import { fetchChapterSegments, usePositionSaver } from './position'

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

const at = (node: string): Position => ({ chapter_index: 1, node_id: node, translation_id: 'p1', mode: 'parallel' })

describe('позиция чтения', () => {
  it('сохраняет последнее положение не чаще раза в 1,5 с', async () => {
    vi.useFakeTimers()
    const save = vi.fn(async () => undefined)
    const saver = usePositionSaver(save)
    saver.schedule(at('a'))
    saver.schedule(at('b'))
    await vi.advanceTimersByTimeAsync(1000)
    saver.schedule(at('c'))
    expect(save).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1500)
    expect(save).toHaveBeenCalledTimes(1)
    expect(save).toHaveBeenCalledWith(at('c'))
  })

  it('при уходе со страницы досохраняет немедленно', () => {
    vi.useFakeTimers()
    const save = vi.fn(async () => undefined)
    const saver = usePositionSaver(save)
    saver.flush()
    expect(save).not.toHaveBeenCalled()
    saver.schedule(at('x'))
    saver.flush()
    expect(save).toHaveBeenCalledWith(at('x'))
    vi.advanceTimersByTime(5000)
    expect(save).toHaveBeenCalledTimes(1)
  })
})

describe('сегменты главы (Review Focus №3)', () => {
  it('дочитывает все страницы по 1000', async () => {
    const page = (n: number, from: number) => Array.from({ length: n }, (_, i) => ({ id: `s${from + i}` }))
    const calls = stubApi({
      'GET /api/translations/p1/segments': (c) => {
        const offset = Number(new URL(c.url, 'http://x').searchParams.get('offset'))
        return { body: offset === 0 ? page(1000, 0) : offset === 1000 ? page(1000, 1000) : page(500, 2000) }
      },
    })
    const segs = await fetchChapterSegments('p1', 3)
    expect(segs).toHaveLength(2500)
    expect(calls.map((c) => c.url)).toEqual([
      '/api/translations/p1/segments?chapter=3&limit=1000&offset=0',
      '/api/translations/p1/segments?chapter=3&limit=1000&offset=1000',
      '/api/translations/p1/segments?chapter=3&limit=1000&offset=2000',
    ])
  })
})
