<script setup lang="ts">
// Список выбора Naive UI с подписью. Подпись связана со списком через
// aria-labelledby, а не обёрткой <label>: щелчок по такой подписи
// переоткрывал бы список. Длинные списки (языки, модели) — с поиском.
import { NSelect, type SelectOption } from 'naive-ui'
import { useId } from 'vue'

withDefaults(
  defineProps<{
    label: string
    name: string
    options: SelectOption[]
    filterable?: boolean
    disabled?: boolean
    hideLabel?: boolean
    size?: 'small' | 'medium'
  }>(),
  { filterable: false, disabled: false, hideLabel: false, size: 'medium' },
)

const model = defineModel<string>({ required: true })
const labelId = useId()
</script>

<template>
  <div
    class="field select-field"
    :data-select="name"
  >
    <span
      :id="labelId"
      :class="{ 'visually-hidden': hideLabel }"
    >{{ label }}</span>
    <NSelect
      v-model:value="model"
      :options="options"
      :filterable="filterable"
      :disabled="disabled"
      :size="size"
      :aria-labelledby="labelId"
      :input-props="{ name, 'aria-labelledby': labelId }"
      :consistent-menu-width="false"
    />
  </div>
</template>

<style scoped>
.select-field > span {
  font-weight: 500;
}
</style>
