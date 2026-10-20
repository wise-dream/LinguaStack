<script setup lang="ts">
// Ошибка у формы или строки — предупреждение Naive UI (role="alert").
// Пустое сообщение — ничего не показывается.
import { NAlert } from 'naive-ui'

withDefaults(defineProps<{ message?: string | null; size?: 'small' | 'medium' }>(), { message: '', size: 'medium' })
</script>

<template>
  <NAlert
    v-if="message"
    type="error"
    class="error-alert"
    :class="size"
    :show-icon="size !== 'small'"
  >
    {{ message }}
  </NAlert>
</template>

<style scoped>
.error-alert.small :deep(.n-alert-body) {
  padding: var(--space-1) var(--space-3);
  font-size: var(--step--1);
}
</style>
