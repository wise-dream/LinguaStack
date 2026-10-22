<script setup lang="ts">
// Конец порции списка: сколько показано и кнопка следующей порции.
import { NButton } from 'naive-ui'

import { formatNumber } from '../lib/format'

defineProps<{ count: number; total: number; loading: boolean }>()
const emit = defineEmits<{ more: [] }>()
</script>

<template>
  <div class="load-more">
    <NButton
      :loading="loading"
      :disabled="loading"
      @click="emit('more')"
    >
      Показать ещё
    </NButton>
    <span class="count">Показано {{ formatNumber(count) }} из {{ formatNumber(total) }}</span>
  </div>
</template>

<style scoped>
.load-more {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: center;
  gap: var(--space-2) var(--space-4);
  margin-top: var(--space-5);
}

.count {
  color: var(--muted);
  font-size: var(--step--1);
}
</style>
