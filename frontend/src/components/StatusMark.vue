<script setup lang="ts">
// Состояние перевода: цвет повторяет смысл текста, а не заменяет его.
import { computed } from 'vue'

import type { ProjectStatus } from '../api/types'
import { statusLabel } from '../lib/format'

const props = defineProps<{ status: ProjectStatus }>()

const tone = computed(() => {
  switch (props.status) {
    case 'completed':
      return 'done'
    case 'completed_with_warnings':
      return 'warn'
    case 'failed':
      return 'error'
    case 'running':
      return 'active'
    default:
      return 'idle'
  }
})
</script>

<template>
  <span
    class="status"
    :class="tone"
  >{{ statusLabel(status) }}</span>
</template>

<style scoped>
.status {
  white-space: nowrap;
}

.status::before {
  content: '';
  display: inline-block;
  width: 0.55em;
  height: 0.55em;
  margin-right: 0.4em;
  border-radius: 50%;
  border: 1.5px solid currentColor;
  vertical-align: 0.05em;
}

.done {
  color: var(--ink-translation);
}

.done::before {
  background: currentColor;
}

.warn::before {
  background: var(--highlight);
  border-color: #8a6d00;
}

.error {
  color: var(--proof);
}

.error::before {
  background: currentColor;
}

.active {
  color: var(--ink-original);
}

.idle {
  color: var(--muted);
}
</style>
