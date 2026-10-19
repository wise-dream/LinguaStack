<script setup lang="ts">
// Разрушительное действие в два нажатия: первое спрашивает, второе в
// течение 4 с подтверждает. Без модальных окон — вопрос там же, где кнопка.
import { onBeforeUnmount, ref } from 'vue'

const props = withDefaults(defineProps<{ label: string; confirmLabel?: string; disabled?: boolean }>(), {
  confirmLabel: 'Точно удалить?',
  disabled: false,
})
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
  <button
    type="button"
    class="button danger"
    :class="{ armed }"
    :disabled="props.disabled"
    @click="onClick"
    @keydown.esc="disarm"
    @blur="disarm"
  >
    {{ armed ? props.confirmLabel : props.label }}
  </button>
</template>

<style scoped>
.armed {
  background: var(--proof);
  color: var(--paper-raised);
}
</style>
