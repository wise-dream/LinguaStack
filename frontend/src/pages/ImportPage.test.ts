import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { ApiError } from '../api/client'
import { book } from '../test/fixtures'

const upload = vi.fn()
const putTOC = vi.fn()
const getTOC = vi.fn()
vi.mock('../api/books', () => ({
  uploadBook: (...a: unknown[]) => upload(...a),
  putTOC: (...a: unknown[]) => putTOC(...a),
  getTOC: (...a: unknown[]) => getTOC(...a),
}))

const { default: ImportPage } = await import('./ImportPage.vue')

afterEach(() => vi.clearAllMocks())

async function mountImport() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/:p(.*)*', component: { template: '<div />' }, meta: { title: '' } }],
  })
  await router.push('/import')
  return mount(ImportPage, { global: { plugins: [router] } })
}

function chooseFile(w: Awaited<ReturnType<typeof mountImport>>, name = 'alice.epub') {
  const input = w.find('input[type=file]')
  Object.defineProperty(input.element, 'files', { value: [new File(['x'], name)], configurable: true })
  return input.trigger('change')
}

describe('импорт', () => {
  it('принимает все форматы этапа 5 и называет их', async () => {
    const w = await mountImport()
    const accept = w.find('input[type=file]').attributes('accept')?.split(',')
    expect(accept).toEqual(expect.arrayContaining(['.epub', '.pdf', '.docx', '.html', '.htm', '.md', '.txt', '.zip']))
    expect(w.find('.drop-hint').text()).toContain('PDF, DOCX, HTML')
  })

  it('отказ сервера показан, форма снова доступна (Review Focus №5)', async () => {
    upload.mockRejectedValueOnce(new ApiError(413, 'Файл больше 50 МБ'))
    const w = await mountImport()
    await chooseFile(w)
    await flushPromises()
    expect(w.find('[role=alert]').text()).toBe('Файл больше 50 МБ')
    expect(w.find('input[type=file]').attributes('disabled')).toBeUndefined()
  })

  it('после загрузки — замечания, оглавление и перестановка глав', async () => {
    upload.mockImplementation(async (_file: File, onProgress: (f: number) => void) => {
      onProgress(0.5)
      return book({ issues: [{ severity: 'warning', code: 'toc_missing', message: 'Оглавление построено по заголовкам' }] })
    })
    const toc = [{ index: 0, title: 'Глава 1' }, { index: 1, title: 'Глава 2' }, { index: 2, title: 'Глава 3' }]
    getTOC.mockResolvedValue(toc)
    putTOC.mockResolvedValue([toc[1], toc[0], toc[2]])
    const w = await mountImport()
    await chooseFile(w)
    await flushPromises()

    expect(w.text()).toContain('Приключения Алисы в Стране чудес')
    expect(w.text()).toContain('Оглавление построено по заголовкам')
    expect(w.find('a[href="/books/b1/translate"]').exists()).toBe(true)

    await w.find('button[aria-label="Переместить «Глава 2» выше"]').trigger('click')
    await w.find('button.save-order').trigger('click')
    await flushPromises()
    expect(putTOC).toHaveBeenCalledWith('b1', [1, 0, 2])
    expect(w.text()).toContain('Порядок глав сохранён')
  })
})

describe('общая библиотека', () => {
  it('предупреждает до загрузки: книгу увидят все', async () => {
    const w = await mountImport()
    expect(w.text()).toContain('Книга и её переводы будут видны всем пользователям сервиса')
  })
})
