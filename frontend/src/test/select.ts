// Списки выбора Naive UI в тестах: значение и варианты читаются из
// SelectField, выбор — его событием (меню NSelect рисуется вне компонента).
import type { VueWrapper } from '@vue/test-utils'

import SelectField from '../components/SelectField.vue'

function field(w: VueWrapper, name: string) {
  const f = w.findAllComponents(SelectField).find((c) => c.props('name') === name)
  if (!f) throw new Error(`нет списка ${name}`)
  return f
}

export const selected = (w: VueWrapper, name: string) => field(w, name).props('modelValue') as string

export const optionValues = (w: VueWrapper, name: string) =>
  (field(w, name).props('options') as { value: string }[]).map((o) => o.value)

export const optionLabels = (w: VueWrapper, name: string) =>
  (field(w, name).props('options') as { label: string }[]).map((o) => o.label)

export const hasSelect = (w: VueWrapper, name: string) =>
  w.findAllComponents(SelectField).some((c) => c.props('name') === name)

export async function choose(w: VueWrapper, name: string, value: string) {
  field(w, name).vm.$emit('update:modelValue', value)
  await w.vm.$nextTick()
}
