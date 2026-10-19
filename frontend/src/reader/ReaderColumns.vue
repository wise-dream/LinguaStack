<script setup lang="ts">
// Шапка колонок билингвального разворота: какой язык где.
import type { ReadMode } from '../api/types'
import { languageName } from '../lib/format'

defineProps<{ mode: ReadMode; source: string; target?: string }>()
</script>

<template>
  <div
    class="columns-head"
    :class="`mode-${mode}`"
    aria-hidden="true"
  >
    <span
      v-if="mode !== 'translation'"
      class="original"
    >Оригинал, {{ languageName(source) }}</span>
    <span
      v-if="mode !== 'original' && target"
      class="translation"
    >Перевод, {{ languageName(target) }}</span>
  </div>
  <slot />
</template>

<style scoped>
.columns-head {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) 11rem;
  column-gap: var(--space-6);
  padding-bottom: var(--space-2);
  margin-bottom: var(--space-3);
  border-bottom: 1px solid var(--rule);
  font-size: var(--step--1);
  color: var(--muted);
}

.columns-head.mode-original,
.columns-head.mode-translation {
  grid-template-columns: minmax(0, var(--measure)) 11rem;
}

.original {
  color: var(--ink-original);
}

.translation {
  color: var(--ink-translation);
}

@media (max-width: 56rem) {
  .columns-head {
    display: none;
  }
}
</style>
