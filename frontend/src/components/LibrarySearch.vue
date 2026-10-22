<script setup lang="ts">
// Поиск по библиотеке: по названию и автору. Набор ищет сам после паузы,
// Enter — сразу. Под полем — сколько найдено.
import { computed } from 'vue'

import { formatNumber, plural } from '../lib/format'
import TextField from './TextField.vue'

const props = defineProps<{
  /** Запрос, по которому показан список. */
  shown: string
  total: number
}>()
const emit = defineEmits<{ search: [] }>()
const q = defineModel<string>({ required: true })

const found = computed(
  () =>
    `${plural(props.total, ['Найдена', 'Найдено', 'Найдено'])} ${formatNumber(props.total)} ` +
    plural(props.total, ['книга', 'книги', 'книг']),
)
</script>

<template>
  <form
    role="search"
    class="library-search"
    @submit.prevent="emit('search')"
  >
    <TextField
      v-model="q"
      label="Поиск книг"
      hide-label
      type="search"
      placeholder="Название или автор"
      :spellcheck="false"
    />
    <p
      v-if="shown && total > 0"
      class="found"
    >
      {{ found }}
    </p>
  </form>
</template>

<style scoped>
.library-search {
  display: grid;
  gap: var(--space-2);
  max-width: 32rem;
  margin-bottom: var(--space-5);
}

.found {
  margin: 0;
  color: var(--muted);
  font-size: var(--step--1);
}
</style>
