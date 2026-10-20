<script setup lang="ts">
// Поле формы: подпись, поле Naive UI и подсказка. Подпись оборачивает поле —
// так оно находится по подписи и читалкой экрана, и тестами.
import { NInput } from 'naive-ui'
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    label: string
    name?: string
    type?: 'text' | 'email' | 'password' | 'search' | 'url' | 'textarea'
    autocomplete?: string
    inputmode?: 'text' | 'numeric' | 'decimal' | 'email' | 'url' | 'search'
    hint?: string
    placeholder?: string
    required?: boolean
    disabled?: boolean
    rows?: number
    hideLabel?: boolean
    spellcheck?: boolean
  }>(),
  { type: 'text', placeholder: '', rows: 3, name: undefined, autocomplete: undefined, inputmode: undefined, hint: undefined },
)

const model = defineModel<string>({ required: true })

const kind = computed(() => (props.type === 'password' ? 'password' : props.type === 'textarea' ? 'textarea' : 'text'))
const inputProps = computed(() => ({
  name: props.name,
  autocomplete: props.autocomplete,
  required: props.required,
  inputmode: props.inputmode,
  spellcheck: props.spellcheck,
  ...(props.type === 'email' || props.type === 'url' || props.type === 'search' ? { type: props.type } : {}),
}))
</script>

<template>
  <label class="field">
    <span :class="{ 'visually-hidden': hideLabel }">{{ label }}</span>
    <NInput
      v-model:value="model"
      :type="kind"
      :input-props="inputProps"
      :placeholder="placeholder"
      :disabled="disabled"
      :rows="rows"
      :autosize="type === 'textarea' ? { minRows: rows } : undefined"
      :show-password-on="type === 'password' ? 'click' : undefined"
    />
    <small v-if="hint">{{ hint }}</small>
    <slot />
  </label>
</template>
