import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import TocTree from './TocTree.vue'

const toc = [
  { index: 0, title: 'Предисловие' },
  { index: 1, title: 'Глава 1', children: [{ index: 2, title: 'Раздел 1.1' }] },
  { index: 3, title: 'Глава 2' },
]

describe('оглавление', () => {
  it('показывает вложенные главы', () => {
    const w = mount(TocTree, { props: { entries: toc } })
    expect(w.text()).toContain('Раздел 1.1')
    expect(w.findAll('button')).toHaveLength(0)
  })

  it('перестановка отдаёт новый порядок прежних позиций', async () => {
    const w = mount(TocTree, { props: { entries: toc, reorderable: true } })
    await w.find('button[aria-label="Переместить «Глава 1» выше"]').trigger('click')
    expect(w.emitted('reorder')?.at(-1)).toEqual([[1, 0, 2]])
    expect(w.findAll('li.top > .row .title').map((t) => t.text())).toEqual(['Глава 1', 'Предисловие', 'Глава 2'])
    // Первую главу выше не поднять, последнюю ниже не опустить.
    expect(w.find('button[aria-label="Переместить «Глава 1» выше"]').attributes('disabled')).toBeDefined()
    expect(w.find('button[aria-label="Переместить «Глава 2» ниже"]').attributes('disabled')).toBeDefined()
  })
})
