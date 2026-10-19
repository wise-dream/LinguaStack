import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import LangPair from './LangPair.vue'

describe('пара языков', () => {
  it('при автоопределении — «исходный язык»', () => {
    expect(mount(LangPair, { props: { source: 'auto', target: 'ru' } }).text()).toBe('исходный язык → русский')
    expect(mount(LangPair, { props: { source: 'en', target: 'de' } }).text()).toBe('английский → немецкий')
  })
})
