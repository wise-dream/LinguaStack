import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import SegmentRow from './SegmentRow.vue'

const block = { node_id: 'n1', type: 'paragraph' as const, text: 'Alice.', source_hash: 'h' }
const segment = {
  id: 's1', node_id: 'n1', chapter_index: 0, ordinal: 0, block_type: 'paragraph' as const, source_text: 'Alice.',
  translated_text: 'Алиса.', status: 'completed' as const, severity: 'ok' as const, findings: [], attempts: 1,
  updated_at: '2026-10-17T10:00:00Z',
}

describe('строка читалки', () => {
  it('касание строки показывает и прячет действия', async () => {
    // На телефоне наведения нет: действия появляются по касанию, а не
    // висят под каждым абзацем.
    const w = mount(SegmentRow, { props: { block, segment, mode: 'parallel', bookId: 'b1' } })
    expect(w.classes()).not.toContain('revealed')
    await w.find('.original').trigger('click')
    expect(w.classes()).toContain('revealed')
    await w.find('.original').trigger('click')
    expect(w.classes()).not.toContain('revealed')
  })
})

describe('чужой перевод', () => {
  it('без правки и повтора: менять может только владелец', () => {
    const w = mount(SegmentRow, { props: { block, segment, mode: 'parallel', bookId: 'b1', editable: false } })
    expect(w.find('button.edit').exists()).toBe(false)
    expect(w.find('button.retry').exists()).toBe(false)
    expect(w.text()).toContain('Алиса.')
  })
})
