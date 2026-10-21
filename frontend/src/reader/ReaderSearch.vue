<script setup lang="ts">
// Поиск по книге и выбранному переводу (ТЗ §8.3, дополнение §19): слова в
// любой форме, фраза в кавычках, подсказка при опечатке. После перехода к
// находке стрелки листают остальные, как Ctrl+F в браузере.
import { NButton, NInput } from 'naive-ui'
import { computed, ref } from 'vue'

import { errorMessage } from '../api/client'
import { searchBook } from '../api/reader'
import type { SearchResponse, SearchResult } from '../api/types'
import ErrorAlert from '../components/ErrorAlert.vue'
import { plural } from '../lib/format'
import { splitHits } from './highlight'

const props = defineProps<{ bookId: string; translationId: string; chapterTitle: (index: number) => string }>()
const emit = defineEmits<{ go: [result: SearchResult] }>()

const q = ref('')
const found = ref<SearchResponse | null>(null)
const error = ref('')
const open = ref(false)
/** Номер находки, к которой перешли; -1 — ещё ни к какой. */
const current = ref(-1)

// Результаты уже в порядке чтения: подряд идущие из одной главы — группа.
const groups = computed(() => {
  const out: { chapter: number; title: string; items: { r: SearchResult; i: number }[] }[] = []
  found.value?.results.forEach((r, i) => {
    const last = out.at(-1)
    if (last?.chapter === r.chapter_index) last.items.push({ r, i })
    else out.push({ chapter: r.chapter_index, title: props.chapterTitle(r.chapter_index), items: [{ r, i }] })
  })
  return out
})

const summary = computed(() => {
  const n = found.value?.total ?? 0
  return `Найдено ${n} ${plural(n, ['совпадение', 'совпадения', 'совпадений'])}`
})

const shown = computed(() => found.value?.results.length ?? 0)

async function search(text = q.value) {
  q.value = text
  error.value = ''
  current.value = -1
  if (text.trim().length < 2) {
    found.value = null
    open.value = false
    return
  }
  try {
    found.value = await searchBook(props.bookId, text.trim(), props.translationId || undefined)
  } catch (e) {
    found.value = null
    error.value = errorMessage(e)
  }
  open.value = true
}

function go(i: number) {
  const r = found.value?.results[i]
  if (!r) return
  current.value = i
  open.value = false
  emit('go', r)
}
</script>

<template>
  <div class="reader-search">
    <form
      class="search"
      role="search"
      @submit.prevent="search()"
    >
      <NInput
        v-model:value="q"
        placeholder="Поиск по книге"
        clearable
        :input-props="{ type: 'search', 'aria-label': 'Поиск по книге' }"
        @keydown.esc="open = false"
      />
    </form>
    <div
      v-if="found && current >= 0"
      class="stepper"
    >
      <NButton
        quaternary
        size="small"
        aria-label="Предыдущее совпадение"
        :disabled="current === 0"
        @click="go(current - 1)"
      >
        <svg
          viewBox="0 0 24 24"
          width="18"
          height="18"
          aria-hidden="true"
          fill="none"
          stroke="currentColor"
          stroke-width="1.8"
          stroke-linecap="round"
          stroke-linejoin="round"
        ><path d="M6 15l6-6 6 6" /></svg>
      </NButton>
      <output aria-live="polite">{{ current + 1 }} из {{ shown }}{{ found.truncated ? '+' : '' }}</output>
      <NButton
        quaternary
        size="small"
        aria-label="Следующее совпадение"
        :disabled="current === shown - 1"
        @click="go(current + 1)"
      >
        <svg
          viewBox="0 0 24 24"
          width="18"
          height="18"
          aria-hidden="true"
          fill="none"
          stroke="currentColor"
          stroke-width="1.8"
          stroke-linecap="round"
          stroke-linejoin="round"
        ><path d="M6 9l6 6 6-6" /></svg>
      </NButton>
    </div>
    <div
      v-if="open && (found || error)"
      class="search-results"
    >
      <ErrorAlert
        v-if="error"
        :message="error"
      />
      <template v-else-if="found && !found.results.length">
        <p class="muted">
          Ничего не нашлось.
        </p>
        <p
          v-if="found.suggestion"
          class="muted"
        >
          Возможно, вы искали:
          <button
            type="button"
            class="suggestion"
            @click="search(found.suggestion)"
          >
            {{ found.suggestion }}
          </button>
        </p>
      </template>
      <template v-else-if="found">
        <p class="summary">
          {{ summary }}
        </p>
        <section
          v-for="g in groups"
          :key="`${g.chapter}-${g.items[0]!.i}`"
        >
          <h3>{{ g.title }} <span class="count">{{ g.items.length }}</span></h3>
          <ol>
            <li
              v-for="{ r, i } in g.items"
              :key="`${r.in}-${r.node_id}`"
            >
              <button
                type="button"
                class="hit"
                @click="go(i)"
              >
                <span class="where">{{ r.in === 'original' ? 'оригинал' : 'перевод' }}</span>
                <span
                  class="snippet"
                  :class="r.in"
                ><template
                  v-for="(part, k) in splitHits(r.snippet, r.hits)"
                  :key="k"
                ><mark v-if="part.hit">{{ part.text }}</mark><template v-else>{{ part.text }}</template></template></span>
              </button>
            </li>
          </ol>
        </section>
        <p
          v-if="found.truncated"
          class="muted"
        >
          Показаны первые {{ shown }} из {{ found.total }} — уточните запрос.
        </p>
      </template>
    </div>
  </div>
</template>

<style scoped>
.reader-search {
  position: relative;
  display: flex;
  align-items: center;
  gap: var(--space-1);
}

.search {
  width: 16rem;
  max-width: 100%;
}

.stepper {
  display: flex;
  align-items: center;
  flex: none;
}

.stepper output {
  min-width: 4.5rem;
  text-align: center;
  font-size: var(--step--1);
  color: var(--muted);
  font-variant-numeric: tabular-nums;
}

.search-results {
  position: absolute;
  top: 100%;
  right: 0;
  z-index: 10;
  width: min(30rem, 90vw);
  max-height: 60vh;
  overflow-y: auto;
  margin-top: var(--space-2);
  padding: var(--space-2);
  background: var(--paper-raised);
  border: 1px solid var(--rule);
  border-radius: var(--radius);
  box-shadow: var(--shadow-pop);
}

.summary {
  margin: var(--space-1) var(--space-2) var(--space-2);
  font-weight: 500;
}

h3 {
  display: flex;
  justify-content: space-between;
  gap: var(--space-2);
  margin: var(--space-3) var(--space-2) var(--space-1);
  font-size: var(--step--1);
  font-weight: 500;
  color: var(--muted);
}

.count {
  font-variant-numeric: tabular-nums;
}

ol {
  margin: 0;
  padding: 0;
  list-style: none;
}

.hit {
  display: grid;
  gap: 2px;
  width: 100%;
  padding: var(--space-2);
  border: none;
  border-radius: var(--radius);
  background: none;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
}

.hit:hover,
.hit:focus-visible {
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

mark {
  padding: 0 1px;
  border-radius: 2px;
  background: color-mix(in srgb, var(--highlight) 45%, transparent);
  color: inherit;
}

.suggestion {
  padding: 0;
  border: none;
  background: none;
  color: var(--ink-original);
  font: inherit;
  font-weight: 500;
  text-decoration: underline;
  text-underline-offset: 0.2em;
  cursor: pointer;
}

.muted {
  margin: var(--space-2);
  color: var(--muted);
}
</style>
