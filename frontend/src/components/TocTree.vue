<script setup lang="ts">
// Оглавление книги. В режиме перестановки главы верхнего уровня
// двигаются кнопками; наружу уходит новый порядок прежних позиций —
// ровно то, что принимает PUT /api/books/{id}/toc.
import { computed, ref, watch } from 'vue'

import type { TocEntry } from '../api/types'

const props = withDefaults(defineProps<{ entries: TocEntry[]; reorderable?: boolean }>(), { reorderable: false })
const emit = defineEmits<{ reorder: [order: number[]]; select: [index: number] }>()

const order = ref<number[]>([])
watch(
  () => props.entries,
  (entries) => {
    order.value = entries.map((_, i) => i)
  },
  { immediate: true },
)

const top = computed(() => order.value.map((i) => props.entries[i]).filter((e): e is TocEntry => e !== undefined))

function move(position: number, delta: number) {
  const target = position + delta
  if (target < 0 || target >= order.value.length) return
  const next = [...order.value]
  ;[next[position], next[target]] = [next[target] as number, next[position] as number]
  order.value = next
  emit('reorder', next)
}
</script>

<template>
  <ol class="toc">
    <li
      v-for="(entry, position) in top"
      :key="entry.index"
      class="top"
    >
      <div class="row">
        <span class="title">{{ entry.title || 'Без названия' }}</span>
        <span
          v-if="reorderable"
          class="moves"
        >
          <button
            type="button"
            class="move"
            :aria-label="`Переместить «${entry.title}» выше`"
            :disabled="position === 0"
            @click="move(position, -1)"
          >↑</button>
          <button
            type="button"
            class="move"
            :aria-label="`Переместить «${entry.title}» ниже`"
            :disabled="position === top.length - 1"
            @click="move(position, 1)"
          >↓</button>
        </span>
      </div>
      <ol
        v-if="entry.children?.length"
        class="nested"
      >
        <li
          v-for="child in entry.children"
          :key="child.index"
        >
          {{ child.title || 'Без названия' }}
        </li>
      </ol>
    </li>
  </ol>
</template>

<style scoped>
.toc {
  margin: 0;
  padding-left: 1.6em;
  font-family: var(--font-book);
}

.toc > li {
  padding-block: var(--space-1);
}

.row {
  display: flex;
  align-items: baseline;
  gap: var(--space-3);
}

.moves {
  display: inline-flex;
  gap: var(--space-1);
  margin-left: auto;
}

.move {
  width: 2rem;
  height: 2rem;
  border: 1px solid var(--rule);
  border-radius: var(--radius);
  background: var(--paper-raised);
  cursor: pointer;
}

.move:disabled {
  opacity: 0.35;
  cursor: default;
}

.nested {
  margin: var(--space-1) 0 0;
  padding-left: 1.4em;
  color: var(--muted);
  list-style: lower-alpha;
}
</style>
