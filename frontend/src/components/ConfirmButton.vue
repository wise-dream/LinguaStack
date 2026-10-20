<script setup lang="ts">
// Разрушительное действие в два нажатия: первое спрашивает, второе в
// течение 4 с подтверждает. Без модальных окон — вопрос там же, где кнопка.
import { NButton } from 'naive-ui'
import { onBeforeUnmount, ref } from 'vue'

const props = withDefaults(
  defineProps<{ label: string; confirmLabel?: string; disabled?: boolean; size?: 'tiny' | 'small' | 'medium' }>(),
  { confirmLabel: 'Точно удалить?', disabled: false, size: 'medium' },
)
const emit = defineEmits<{ confirm: [] }>()

const armed = ref(false)
let timer: ReturnType<typeof setTimeout> | undefined

function disarm() {
  armed.value = false
  clearTimeout(timer)
}

function onClick() {
  if (armed.value) {
    disarm()
    emit('confirm')
    return
  }
  armed.value = true
  timer = setTimeout(disarm, 4000)
}

onBeforeUnmount(() => clearTimeout(timer))
</script>

<template>
  <NButton
    class="danger"
    :class="{ armed }"
    :type="'error'"
    :secondary="!armed"
    :disabled="props.disabled"
    :size="props.size"
    @click="onClick"
    @keydown.esc="disarm"
    @blur="disarm"
  >
    {{ armed ? props.confirmLabel : props.label }}
  </NButton>
</template>
