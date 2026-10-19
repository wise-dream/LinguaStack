import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'

import ConfirmButton from './ConfirmButton.vue'

afterEach(() => vi.useRealTimers())

describe('кнопка с подтверждением', () => {
  it('первое нажатие спрашивает, второе подтверждает', async () => {
    const w = mount(ConfirmButton, { props: { label: 'Удалить' } })
    await w.find('button').trigger('click')
    expect(w.find('button').text()).toBe('Точно удалить?')
    expect(w.emitted('confirm')).toBeUndefined()
    await w.find('button').trigger('click')
    expect(w.emitted('confirm')).toHaveLength(1)
    expect(w.find('button').text()).toBe('Удалить')
  })

  it('через 4 секунды забывает о первом нажатии', async () => {
    vi.useFakeTimers()
    const w = mount(ConfirmButton, { props: { label: 'Удалить' } })
    await w.find('button').trigger('click')
    vi.advanceTimersByTime(4000)
    await w.vm.$nextTick()
    expect(w.find('button').text()).toBe('Удалить')
    await w.find('button').trigger('click')
    expect(w.emitted('confirm')).toBeUndefined()
  })

  it('Esc отменяет', async () => {
    const w = mount(ConfirmButton, { props: { label: 'Отменить перевод', confirmLabel: 'Точно отменить?' } })
    await w.find('button').trigger('click')
    expect(w.find('button').text()).toBe('Точно отменить?')
    await w.find('button').trigger('keydown', { key: 'Escape' })
    expect(w.find('button').text()).toBe('Отменить перевод')
  })
})
