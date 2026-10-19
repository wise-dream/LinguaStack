<script setup lang="ts">
// Поиск по книге и выбранному переводу (ТЗ §8.3).
import { ref } from 'vue'

import { errorMessage } from '../api/client'
import { searchBook } from '../api/reader'
import type { SearchResult } from '../api/types'

const props = defineProps<{ bookId: string; translationId: string; chapterTitle: (index: number) => string }>()
const emit = defineEmits<{ go: [result: SearchResult] }>()

const q = ref('')
const results = ref<SearchResult[] | null>(null)
const truncated = ref(false)
const error = ref('')

async function search() {
  error.value = ''
  if (q.value.trim().length < 2) {
    results.value = null
    return
  }
  try {
    const res = await searchBook(props.bookId, q.value.trim(), props.translationId || undefined)
    results.value = res.results
    truncated.value = res.truncated
  } catch (e) {
    error.value = errorMessage(e)
  }
}

function go(r: SearchResult) {
  emit('go', r)
  results.value = null
}
</script>

<template>
  <div class="reader-search">
    <form
      class="search"
      role="search"
      @submit.prevent="search"
    >
      <label class="visually-hidden">Поиск по книге</label>
      <input
        v-model="q"
        type="search"
        placeholder="Поиск по книге"
        @keydown.esc="results = null"
      >
    </form>
    <div
      v-if="results || error"
      class="search-results"
    >
      <p
        v-if="error"
        role="alert"
        class="form-error"
      >
        {{ error }}
      </p>
      <p
        v-else-if="!results?.length"
        class="muted"
      >
        Ничего не нашлось.
      </p>
      <ol v-else>
        <li
          v-for="r in results"
          :key="`${r.in}-${r.node_id}`"
        >
          <button
            type="button"
            @click="go(r)"
          >
            <span class="where">{{ chapterTitle(r.chapter_index) }}, {{ r.in === 'original' ? 'оригинал' : 'перевод' }}</span>
            <span
              class="snippet"
              :class="r.in"
            >{{ r.snippet }}</span>
          </button>
        </li>
      </ol>
      <p
        v-if="truncated"
        class="muted"
      >
        Показаны первые 50 находок — уточните запрос.
      </p>
    </div>
  </div>
</template>

<style scoped>
.reader-search {
  position: relative;
}

.search input {
  width: 16rem;
  max-width: 100%;
}

.search-results {
  position: absolute;
  right: 0;
  z-index: 10;
  width: min(28rem, 90vw);
  max-height: 60vh;
  overflow-y: auto;
  margin-top: var(--space-2);
  padding: var(--space-2);
  background: var(--paper-raised);
  border: 1px solid var(--rule);
  border-radius: var(--radius);
  box-shadow: 0 8px 24px rgb(27 36 64 / 12%);
}

ol {
  margin: 0;
  padding: 0;
  list-style: none;
}

button {
  display: grid;
  gap: var(--space-1);
  width: 100%;
  padding: var(--space-2);
  border: none;
  border-radius: var(--radius);
  background: none;
  text-align: left;
  cursor: pointer;
}

button:hover {
  background: var(--paper);
}

.where {
  font-size: var(--step--1);
  color: var(--muted);
}

.snippet {
  font-family: var(--font-book);
}

.snippet.translation {
  color: var(--ink-translation);
}

.muted {
  margin: var(--space-2);
  color: var(--muted);
}
</style>
