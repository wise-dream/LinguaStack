<script setup lang="ts">
// Импорт книги (ТЗ §4): загрузка с прогрессом, замечания импорта и
// предварительный просмотр оглавления с перестановкой глав.
import { NButton, NProgress } from 'naive-ui'
import { computed, ref } from 'vue'

import { getTOC, putTOC, uploadBook } from '../api/books'
import { errorMessage } from '../api/client'
import type { Book, ImportIssue, TocEntry } from '../api/types'
import LinkButton from '../components/LinkButton.vue'
import TocTree from '../components/TocTree.vue'
import { formatNumber, plural } from '../lib/format'

const book = ref<Book | null>(null)
const toc = ref<TocEntry[]>([])
const progress = ref<number | null>(null)
const error = ref('')
const pendingOrder = ref<number[] | null>(null)
const orderSaved = ref(false)
const dragging = ref(false)

const busy = computed(() => progress.value !== null)

const severityLabel: Record<ImportIssue['severity'], string> = {
  info: 'К сведению',
  warning: 'Замечание',
  error: 'Ошибка',
}

async function start(file: File | undefined) {
  if (!file || busy.value) return
  error.value = ''
  book.value = null
  pendingOrder.value = null
  orderSaved.value = false
  progress.value = 0
  try {
    const uploaded = await uploadBook(file, (f) => (progress.value = f))
    toc.value = await getTOC(uploaded.id)
    book.value = uploaded
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    progress.value = null
  }
}

function onChange(e: Event) {
  const input = e.target as HTMLInputElement
  void start(input.files?.[0])
  input.value = ''
}

function onDrop(e: DragEvent) {
  dragging.value = false
  void start(e.dataTransfer?.files?.[0])
}

function onReorder(order: number[]) {
  pendingOrder.value = order
  orderSaved.value = false
}

async function saveOrder() {
  if (!book.value || !pendingOrder.value) return
  error.value = ''
  try {
    toc.value = await putTOC(book.value.id, pendingOrder.value)
    pendingOrder.value = null
    orderSaved.value = true
  } catch (e) {
    error.value = errorMessage(e)
  }
}
</script>

<template>
  <main class="page import">
    <h1>Загрузка книги</h1>

    <label
      class="drop"
      :class="{ dragging, busy }"
      @dragover.prevent="dragging = true"
      @dragleave="dragging = false"
      @drop.prevent="onDrop"
    >
      <span class="drop-title">Перетащите файл сюда или выберите его</span>
      <span class="drop-hint">EPUB, PDF, DOCX, HTML, Markdown, TXT или ZIP-архив документации, до 50 МБ</span>
      <span
        class="button pick"
        aria-hidden="true"
      >Выбрать файл</span>
      <input
        class="visually-hidden"
        type="file"
        accept=".epub,.pdf,.docx,.html,.htm,.md,.markdown,.txt,.zip"
        :disabled="busy"
        @change="onChange"
      >
    </label>

    <div
      v-if="progress !== null"
      class="progress"
    >
      <NProgress
        type="line"
        :percentage="Math.round(progress * 100)"
        :show-indicator="false"
        :processing="progress >= 1"
        aria-label="Загрузка файла"
      />
      <span>{{ progress < 1 ? `Отправлено ${Math.round(progress * 100)}%` : 'Разбираю книгу…' }}</span>
    </div>

    <p
      v-if="error"
      role="alert"
      class="form-error"
    >
      {{ error }}
    </p>

    <section
      v-if="book"
      class="result card"
    >
      <h2 class="book-title">
        {{ book.title }}
      </h2>
      <p
        v-if="book.author"
        class="author"
      >
        {{ book.author }}
      </p>
      <p class="stats">
        {{ formatNumber(book.chapters) }} {{ plural(book.chapters, ['глава', 'главы', 'глав']) }},
        {{ formatNumber(book.translatable) }} {{ plural(book.translatable, ['фрагмент', 'фрагмента', 'фрагментов']) }} для перевода
      </p>

      <ul
        v-if="book.issues.length"
        class="issues"
      >
        <li
          v-for="(issue, i) in book.issues"
          :key="i"
          :class="issue.severity"
        >
          <strong>{{ severityLabel[issue.severity] }}.</strong> {{ issue.message }}
        </li>
      </ul>

      <h3>Оглавление</h3>
      <p class="hint">
        Если главы встали не по порядку, переставьте их — перевод и читалка пойдут в этом порядке.
      </p>
      <TocTree
        :entries="toc"
        reorderable
        @reorder="onReorder"
      />
      <p class="order-actions">
        <NButton
          v-if="pendingOrder"
          class="save-order"
          type="primary"
          secondary
          @click="saveOrder"
        >
          Сохранить порядок глав
        </NButton>
        <span
          v-if="orderSaved"
          role="status"
          class="notice"
        >Порядок глав сохранён</span>
      </p>

      <div class="next">
        <LinkButton
          :to="`/books/${book.id}/translate`"
          type="primary"
        >
          Настроить перевод
        </LinkButton>
        <LinkButton :to="`/books/${book.id}`">
          Открыть книгу
        </LinkButton>
      </div>
    </section>
  </main>
</template>

<style scoped>
.import {
  max-width: 48rem;
}

.drop {
  position: relative;
  display: grid;
  gap: var(--space-1);
  justify-items: center;
  text-align: center;
  padding: var(--space-7) var(--space-5);
  border: 1.5px dashed var(--rule);
  border-radius: var(--radius-card);
  background: var(--surface);
  cursor: pointer;
  transition: border-color 0.2s, background-color 0.2s;
}

.drop:hover {
  border-color: var(--ink-original);
}

.drop.dragging {
  border-color: var(--ink-original);
  background: color-mix(in srgb, var(--highlight) 20%, var(--surface));
}

.drop.busy {
  opacity: 0.6;
  cursor: progress;
}

.drop-title {
  font-weight: 500;
  font-size: var(--step-1);
}

.drop-hint {
  color: var(--muted);
}

.pick {
  margin-top: var(--space-3);
}

.drop:focus-within {
  outline: 2px solid var(--ink-original);
  outline-offset: 2px;
}

.progress {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  margin-top: var(--space-4);
}

.progress > :first-child {
  flex: 1;
}

.progress span {
  color: var(--muted);
  white-space: nowrap;
}

.form-error {
  margin-top: var(--space-4);
}

.result {
  margin-top: var(--space-5);
}

.book-title {
  font-family: var(--font-book);
  font-size: var(--step-2);
  font-weight: 600;
}

.author {
  margin: var(--space-1) 0 0;
  color: var(--muted);
}

.stats {
  margin: var(--space-2) 0 var(--space-4);
}

.issues {
  margin: 0 0 var(--space-5);
  padding: 0;
  list-style: none;
  display: grid;
  gap: var(--space-2);
}

.issues li {
  padding: var(--space-2) var(--space-3);
  border-radius: 8px;
  background: var(--paper-raised);
  border: 1px solid var(--rule-soft);
}

.issues li.warning {
  background: color-mix(in srgb, var(--highlight) 18%, var(--surface));
  border-color: color-mix(in srgb, var(--highlight) 55%, var(--surface));
}

.issues li.error {
  color: var(--proof);
  background: color-mix(in srgb, var(--proof) 7%, var(--surface));
  border-color: color-mix(in srgb, var(--proof) 25%, var(--surface));
}

h3 {
  font-size: var(--step-1);
  margin-top: var(--space-5);
}

.hint {
  color: var(--muted);
  margin: var(--space-1) 0 var(--space-3);
}

.order-actions {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-height: 2.5rem;
}

.next {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-3);
  margin-top: var(--space-5);
  padding-top: var(--space-4);
  border-top: 1px solid var(--rule-soft);
}
</style>
