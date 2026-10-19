import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { ExportJob } from '../api/types'
import { stubApi } from '../test/fakeApi'
import { exportJob } from '../test/fixtures'
import ExportPanel from './ExportPanel.vue'

beforeEach(() => vi.useFakeTimers())
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

async function mountPanel(routes: Parameters<typeof stubApi>[0]) {
  const calls = stubApi(routes)
  const w = mount(ExportPanel, { props: { bookId: 'b1', translationId: 'p1' } })
  await flushPromises()
  return { w, calls }
}

describe('панель экспорта', () => {
  it('форматы и режимы; «Экспортировать» ставит задачу и ждёт готовности', async () => {
    let polls = 0
    const { w, calls } = await mountPanel({
      'GET /api/books/b1/exports': () => {
        polls++
        const job: ExportJob = polls < 3 ? exportJob({ status: 'running', file_name: '', size_bytes: 0 }) : exportJob()
        return { body: polls === 1 ? [] : [job] }
      },
      'POST /api/books/b1/exports': { status: 202, body: exportJob({ status: 'pending', file_name: '', size_bytes: 0 }) },
    })
    const options = w.findAll('select[name=format] option').map((o) => o.text())
    expect(options).toEqual(['EPUB', 'Markdown', 'Word (DOCX)', 'HTML', 'Текст (TXT)'])
    expect(w.findAll('input[name=mode]').map((r) => r.element.closest('label')?.textContent?.trim()))
      .toEqual(['Только перевод', 'Оригинал и перевод'])

    await w.find('select[name=format]').setValue('markdown')
    await w.find('input[value=bilingual]').setValue(true)
    await w.find('form').trigger('submit')
    await flushPromises()
    const post = calls.find((c) => c.method === 'POST')
    expect(post?.body).toEqual({ translation_id: 'p1', format: 'markdown', mode: 'bilingual' })
    expect(w.text()).toContain('В очереди')

    await vi.advanceTimersByTimeAsync(1000)
    expect(w.text()).toContain('Собирается')
    await vi.advanceTimersByTimeAsync(1000)
    const link = w.find('a.download')
    expect(link.text()).toBe('Скачать')
    expect(link.attributes('href')).toBe('/api/exports/e1/file')
    expect(w.text()).toContain('1,2' + String.fromCharCode(0xa0) + 'МБ')
    const before = polls
    await vi.advanceTimersByTimeAsync(5000)
    expect(polls).toBe(before) // готово — опрос остановлен
  })

  it('только экспорты своего перевода', async () => {
    const { w } = await mountPanel({
      'GET /api/books/b1/exports': { body: [exportJob(), exportJob({ id: 'e2', translation_id: 'p2' })] },
    })
    expect(w.findAll('[data-export]').map((r) => r.attributes('data-export'))).toEqual(['e1'])
  })

  it('непереведённые фрагменты и ошибка с повтором', async () => {
    const { w, calls } = await mountPanel({
      'GET /api/books/b1/exports': {
        body: [
          exportJob({ untranslated: 12 }),
          exportJob({ id: 'e2', status: 'failed', file_name: '', error: 'Не удалось собрать файл экспорта.', format: 'docx', mode: 'bilingual' }),
        ],
      },
      'POST /api/books/b1/exports': { status: 202, body: exportJob({ id: 'e3', status: 'pending' }) },
    })
    expect(w.find('[data-export=e1]').text()).toContain('12 фрагментов не переведены и оставлены в оригинале')
    const failed = w.find('[data-export=e2]')
    expect(failed.text()).toContain('Не удалось собрать файл экспорта.')
    await failed.find('button.retry').trigger('click')
    await flushPromises()
    expect(calls.find((c) => c.method === 'POST')?.body).toEqual({ translation_id: 'p1', format: 'docx', mode: 'bilingual' })
  })

  it('отказ сервера (429) показан', async () => {
    const { w } = await mountPanel({
      'GET /api/books/b1/exports': { body: [] },
      'POST /api/books/b1/exports': { status: 429, body: { error: 'Слишком много экспортов в работе: дождитесь, пока они завершатся' } },
    })
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(w.find('[role=alert]').text()).toBe('Слишком много экспортов в работе: дождитесь, пока они завершатся')
  })

  it('удаление — после подтверждения', async () => {
    const { w, calls } = await mountPanel({
      'GET /api/books/b1/exports': { body: [exportJob()] },
      'DELETE /api/exports/e1': { status: 204 },
    })
    const del = w.find('[data-export=e1] button.danger')
    await del.trigger('click')
    expect(calls.some((c) => c.method === 'DELETE')).toBe(false)
    await del.trigger('click')
    await flushPromises()
    expect(calls.some((c) => c.method === 'DELETE' && c.path === '/api/exports/e1')).toBe(true)
    expect(w.find('[data-export=e1]').exists()).toBe(false)
  })
})
