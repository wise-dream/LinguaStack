<script setup lang="ts">
// Перевод книги: живой прогресс, управление и сегменты с фильтрами по
// ошибкам и замечаниям — чтобы найти и повторить проблемные (ТЗ §5.3).
import { NButton, NEmpty } from 'naive-ui'
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
import ErrorAlert from '../components/ErrorAlert.vue'
import ExportPanel from '../components/ExportPanel.vue'
import LangPair from '../components/LangPair.vue'
import LinkButton from '../components/LinkButton.vue'
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
    <ErrorAlert :message="error" />
    <template v-if="project">
      <section class="summary card">
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
          <LinkButton
            v-if="project.segments_done > 0"
            :to="{ path: `/read/${project.book_id}`, query: { t: project.id } }"
            type="primary"
          >
            Читать
          </LinkButton>
          <NButton
            v-if="canPause"
            class="pause"
            @click="act(pauseTranslation)"
          >
            Приостановить
          </NButton>
          <NButton
            v-if="canResume"
            class="resume"
            type="primary"
            secondary
            @click="act(resumeTranslation, true)"
          >
            Продолжить
          </NButton>
          <ConfirmButton
            v-if="canCancel"
            label="Отменить перевод"
            confirm-label="Точно отменить?"
            @confirm="act(cancelTranslation)"
          />
        </div>
        <ErrorAlert :message="actionError" />
      </section>

      <ExportPanel
        :book-id="project.book_id"
        :translation-id="project.id"
      />

      <section class="segments card">
        <h2>Фрагменты</h2>
        <div
          class="filters"
          role="group"
          aria-label="Показать фрагменты"
        >
          <NButton
            v-for="f in filters"
            :key="f.id"
            size="small"
            round
            :type="filter === f.id ? 'primary' : 'default'"
            :data-filter="f.id"
            :aria-pressed="filter === f.id"
            @click="setFilter(f.id)"
          >
            {{ f.label }}
          </NButton>
        </div>
        <NEmpty
          v-if="!segments.length"
          size="small"
          :description="filter === 'all' ? 'Фрагменты появятся, когда перевод начнётся.' : 'Таких фрагментов нет.'"
        />
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
              <LinkButton
                :to="readerLink(s)"
                size="small"
                text
              >
                Открыть в читалке
              </LinkButton>
              <NButton
                class="retry"
                size="small"
                text
                @click="retry(s)"
              >
                Перевести заново
              </NButton>
            </div>
            <ErrorAlert
              :message="segmentError[s.id]"
              size="small"
            />
          </li>
        </ol>
        <NButton
          v-if="hasMore"
          class="more"
          @click="loadSegments(false)"
        >
          Показать ещё
        </NButton>
      </section>
    </template>
  </main>
</template>

<style scoped>
.detail {
  display: grid;
  gap: var(--space-5);
  align-content: start;
}

.summary {
  display: grid;
  gap: var(--space-3);
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
  font-weight: 600;
}

.sub,
.muted {
  margin: 0;
  color: var(--muted);
}

.last-error {
  margin: 0;
  padding: var(--space-2) var(--space-3);
  color: var(--proof);
  background: color-mix(in srgb, var(--proof) 7%, var(--surface));
  border-radius: 8px;
}

.controls {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
  margin-top: var(--space-2);
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
  border-bottom: 1px solid var(--rule-soft);
  border-left: 3px solid transparent;
}

.segment-list > li:last-child {
  border-bottom: none;
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
.segment-list .error-alert {
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



.more {
  margin-top: var(--space-4);
}

@media (max-width: 40rem) {
  .segment-list > li {
    grid-template-columns: 1fr;
  }
}
</style>
