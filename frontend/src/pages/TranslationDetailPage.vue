<script setup lang="ts">
// Перевод книги: живой прогресс, управление и сегменты с фильтрами по
// ошибкам и замечаниям — чтобы найти и повторить проблемные (ТЗ §5.3).
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'

import { errorMessage } from '../api/client'
import {
  cancelTranslation,
  getTranslation,
  listSegments,
  pauseTranslation,
  resumeTranslation,
  retrySegment,
  type SegmentFilter,
} from '../api/translations'
import type { Project, Segment } from '../api/types'
import ConfirmButton from '../components/ConfirmButton.vue'
import ExportPanel from '../components/ExportPanel.vue'
import LangPair from '../components/LangPair.vue'
import ProgressBar from '../components/ProgressBar.vue'
import ProjectFacts from '../components/ProjectFacts.vue'
import StatusMark from '../components/StatusMark.vue'
import { renderInline } from '../lib/inline'
import { useNow } from '../lib/now'
import { isTerminal, useProgress } from '../lib/progress'

const PAGE = 50

type Filter = 'all' | 'failed' | 'warning'

const route = useRoute()
const id = computed(() => String(route.params.id))
const project = ref<Project | null>(null)
const error = ref('')
const actionError = ref('')
const now = useNow()
const progress = useProgress(id)

const filter = ref<Filter>('all')
const segments = ref<Segment[]>([])
const hasMore = ref(false)
const segmentError = ref<Record<string, string>>({})

const filters: { id: Filter; label: string }[] = [
  { id: 'all', label: 'Все' },
  { id: 'failed', label: 'С ошибками' },
  { id: 'warning', label: 'С предупреждениями' },
]

watch(progress.event, (e) => {
  if (!e || !project.value) return
  const statusChanged = e.status !== project.value.status
  project.value = { ...project.value, ...e }
  // Фрагменты появляются при старте и меняются к концу — список
  // перечитывается при смене состояния, а не на каждом шаге прогресса.
  if (statusChanged) void loadSegments(true)
})

const canPause = computed(() => project.value?.status === 'running' || project.value?.status === 'queued')
const canResume = computed(() => project.value?.status === 'paused' || project.value?.status === 'failed')
// Упавший перевод тоже можно отменить: он ждёт «Продолжить».
const canCancel = computed(() => !!project.value && (!isTerminal(project.value.status) || project.value.status === 'failed'))

function filterQuery(f: Filter): SegmentFilter {
  if (f === 'failed') return { status: 'failed' }
  if (f === 'warning') return { severity: 'warning' }
  return {}
}

async function loadSegments(reset: boolean) {
  const offset = reset ? 0 : segments.value.length
  try {
    const page = await listSegments(id.value, { ...filterQuery(filter.value), limit: PAGE, offset })
    segments.value = reset ? page : [...segments.value, ...page]
    hasMore.value = page.length === PAGE
  } catch (e) {
    error.value = errorMessage(e)
  }
}

function setFilter(f: Filter) {
  filter.value = f
  void loadSegments(true)
}

onMounted(async () => {
  try {
    project.value = await getTranslation(id.value)
  } catch (e) {
    error.value = errorMessage(e)
    return
  }
  await loadSegments(true)
})

async function act(action: (id: string) => Promise<Project>, restart = false) {
  actionError.value = ''
  try {
    project.value = await action(id.value)
    if (restart) progress.restart()
  } catch (e) {
    actionError.value = errorMessage(e)
  }
}

async function retry(s: Segment) {
  segmentError.value = { ...segmentError.value, [s.id]: '' }
  try {
    const updated = await retrySegment(s.id)
    segments.value = segments.value.map((x) => (x.id === s.id ? updated : x))
  } catch (e) {
    segmentError.value = { ...segmentError.value, [s.id]: errorMessage(e) }
  }
}

const readerLink = (s: Segment) => ({
  path: `/read/${project.value?.book_id}`,
  query: { t: id.value, ch: String(s.chapter_index), n: s.node_id },
})
</script>

<template>
  <main class="page detail">
    <p
      v-if="error"
      role="alert"
      class="form-error"
    >
      {{ error }}
    </p>
    <template v-if="project">
      <p class="crumb">
        <RouterLink :to="`/books/${project.book_id}`">
          {{ project.book_title }}
        </RouterLink>
      </p>
      <header class="head">
        <h1>
          <LangPair
            :source="project.source_lang"
            :target="project.target_lang"
          />
        </h1>
        <StatusMark :status="project.status" />
      </header>
      <p class="sub">
        {{ project.model_id }}
      </p>
      <ProgressBar
        :done="project.segments_done"
        :failed="project.segments_failed"
        :total="project.segments_total"
      />
      <ProjectFacts
        :project="project"
        :now="now"
      />
      <p
        v-if="project.last_error"
        class="last-error"
      >
        {{ project.last_error }}
      </p>

      <div class="controls">
        <RouterLink
          v-if="project.segments_done > 0"
          :to="{ path: `/read/${project.book_id}`, query: { t: project.id } }"
          class="button primary"
        >
          Читать
        </RouterLink>
        <button
          v-if="canPause"
          type="button"
          class="button pause"
          @click="act(pauseTranslation)"
        >
          Приостановить
        </button>
        <button
          v-if="canResume"
          type="button"
          class="button resume"
          @click="act(resumeTranslation, true)"
        >
          Продолжить
        </button>
        <ConfirmButton
          v-if="canCancel"
          label="Отменить перевод"
          confirm-label="Точно отменить?"
          @confirm="act(cancelTranslation)"
        />
      </div>
      <p
        v-if="actionError"
        role="alert"
        class="form-error"
      >
        {{ actionError }}
      </p>

      <ExportPanel
        :book-id="project.book_id"
        :translation-id="project.id"
      />

      <section class="segments">
        <h2>Фрагменты</h2>
        <div
          class="filters"
          role="group"
          aria-label="Показать фрагменты"
        >
          <button
            v-for="f in filters"
            :key="f.id"
            type="button"
            :data-filter="f.id"
            :aria-pressed="filter === f.id"
            @click="setFilter(f.id)"
          >
            {{ f.label }}
          </button>
        </div>
        <p
          v-if="!segments.length"
          class="muted"
        >
          {{ filter === 'all' ? 'Фрагменты появятся, когда перевод начнётся.' : 'Таких фрагментов нет.' }}
        </p>
        <ol class="segment-list">
          <li
            v-for="s in segments"
            :key="s.id"
            :data-segment="s.id"
            :class="s.severity"
          >
            <!-- eslint-disable vue/no-v-html -- renderInline сначала экранирует текст книги -->
            <p
              class="original"
              v-html="renderInline(s.source_text)"
            />
            <p
              v-if="s.translated_text !== null"
              class="translation"
              v-html="renderInline(s.translated_text)"
            />
            <!-- eslint-enable vue/no-v-html -->
            <p
              v-else
              class="pending"
            >
              {{ s.status === 'failed' ? 'Не переведено' : 'Ещё не переведено' }}
            </p>
            <ul
              v-if="s.findings.length || s.last_error"
              class="findings"
            >
              <li
                v-for="(f, i) in s.findings"
                :key="i"
              >
                {{ f.message }}
              </li>
              <li v-if="s.last_error && !s.findings.length">
                {{ s.last_error.split('\n')[0] }}
              </li>
            </ul>
            <div class="segment-actions">
              <RouterLink :to="readerLink(s)">
                Открыть в читалке
              </RouterLink>
              <button
                type="button"
                class="link retry"
                @click="retry(s)"
              >
                Перевести заново
              </button>
            </div>
            <p
              v-if="segmentError[s.id]"
              role="alert"
              class="form-error"
            >
              {{ segmentError[s.id] }}
            </p>
          </li>
        </ol>
        <button
          v-if="hasMore"
          type="button"
          class="button more"
          @click="loadSegments(false)"
        >
          Показать ещё
        </button>
      </section>
    </template>
  </main>
</template>

<style scoped>
.detail {
  display: grid;
  gap: var(--space-3);
  align-content: start;
}

.crumb {
  margin: 0;
  font-family: var(--font-book);
  color: var(--muted);
}

.head {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: var(--space-2) var(--space-4);
}

h1 {
  font-family: var(--font-book);
  font-size: var(--step-3);
  font-weight: 500;
}

.sub,
.muted {
  margin: 0;
  color: var(--muted);
}

.last-error {
  margin: 0;
  color: var(--proof);
}

.controls {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-3);
  margin-top: var(--space-2);
}

.segments {
  margin-top: var(--space-5);
}

h2 {
  font-size: var(--step-1);
  margin-bottom: var(--space-3);
}

.filters {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
  margin-bottom: var(--space-4);
}

.filters button {
  padding: var(--space-1) var(--space-3);
  border: 1px solid var(--rule);
  border-radius: 999px;
  background: var(--paper-raised);
  cursor: pointer;
}

.filters button[aria-pressed='true'] {
  border-color: var(--ink-original);
  background: var(--ink-original);
  color: var(--paper-raised);
}

.segment-list {
  margin: 0;
  padding: 0;
  list-style: none;
}

.segment-list > li {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: var(--space-2) var(--space-5);
  padding: var(--space-4) 0 var(--space-4) var(--space-3);
  border-bottom: 1px solid var(--rule);
  border-left: 3px solid transparent;
}

.segment-list > li.warning {
  border-left-color: var(--highlight);
}

.segment-list > li.error {
  border-left-color: var(--proof);
}

.original,
.translation,
.pending {
  margin: 0;
  font-family: var(--font-book);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.original {
  color: var(--ink-original);
}

.translation {
  color: var(--ink-translation);
}

.pending {
  color: var(--muted);
  font-style: italic;
}

.findings,
.segment-actions,
.segment-list .form-error {
  grid-column: 1 / -1;
}

.findings {
  margin: 0;
  padding-left: 1.2em;
  color: var(--proof);
}

.segment-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-4);
}

.link {
  padding: 0;
  border: none;
  background: none;
  color: var(--muted);
  text-decoration: underline;
  text-underline-offset: 0.2em;
  cursor: pointer;
}

.link:hover,
.segment-actions a:hover {
  color: var(--ink-original);
}

.segment-actions a {
  color: var(--muted);
}

.more {
  margin-top: var(--space-4);
}

@media (max-width: 40rem) {
  .segment-list > li {
    grid-template-columns: 1fr;
  }
}
</style>
