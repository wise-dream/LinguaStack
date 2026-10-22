import { mount } from '@vue/test-utils'
import { NSelect } from 'naive-ui'
import { afterEach, describe, expect, it, vi } from 'vitest'

import SelectField from './SelectField.vue'

afterEach(() => vi.unstubAllGlobals())

function mountAt(narrow: boolean) {
  vi.stubGlobal('matchMedia', (q: string) => ({
    matches: narrow && q === '(max-width: 40rem)',
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  }))
  return mount(SelectField, {
    props: { label: 'Провайдер', name: 'provider_id', modelValue: 'a', options: [{ label: 'A', value: 'a' }] },
  })
}

describe('список выбора', () => {
  it('на узком экране меню — шириной поля: длинный вариант не выходит за экран', () => {
    expect(mountAt(true).findComponent(NSelect).props('consistentMenuWidth')).toBe(true)
  })

  it('на широком — по содержимому: короткому полю длинный вариант не обрезается', () => {
    expect(mountAt(false).findComponent(NSelect).props('consistentMenuWidth')).toBe(false)
  })
})
