<script setup lang="ts">
// Книга: метаданные, оглавление и её переводы (ТЗ §8.2).
import { NButton } from 'naive-ui'
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'

import { getBook, getTOC } from '../api/books'
import { errorMessage } from '../api/client'
import { listTranslations, resumeTranslation } from '../api/translations'
import type { Book, ExportJob, Project, TocEntry } from '../api/types'
import ExportList from '../components/ExportList.vue'
import LangPair from '../components/LangPair.vue'
import LinkButton from '../components/LinkButton.vue'
import StatusMark from '../components/StatusMark.vue'
import TocTree from '../components/TocTree.vue'
import { useExports } from '../lib/exports'
import { formatDate, formatName, languageName } from '../lib/format'

const route = useRoute()
const id = String(route.params.id)

const book = ref<Book | null>(null)
const toc = ref<TocEntry[]>([])
const projects = ref<Project[]>([])
const error = ref('')
const rowError = ref<Record<string, string>>({})
const exports = useExports(() => id)
const exportError = ref('')
// Язык перевода у каждого экспорта: в истории книги их бывает несколько.
const exportLanguages = computed(() => Object.fromEntries(projects.value.map((p) => [p.id, p.target_lang])))

onMounted(async () => {
  try {
    const [b, t, p] = await Promise.all([getBook(id), getTOC(id), listTranslations(id)])
    book.value = b
    toc.value = t
    projects.value = p
  } catch (e) {
    error.value = errorMessage(e)
  }
  await exports.load()
})

async function retryExport(j: ExportJob) {
  exportError.value = ''
  try {
    await exports.create({ translation_id: j.translation_id, format: j.format, mode: j.mode })
  } catch (e) {
    exportError.value = errorMessage(e)
  }
}

async function removeExport(j: ExportJob) {
  exportError.value = ''
  try {
    await exports.remove(j.id)
  } catch (e) {
    exportError.value = errorMessage(e)
  }
}

const canResume = (p: Project) => p.status === 'paused' || p.status === 'failed'

async function resume(p: Project) {
  rowError.value = { ...rowError.value, [p.id]: '' }
  try {
    const updated = await resumeTranslation(p.id)
    projects.value = projects.value.map((x) => (x.id === p.id ? updated : x))
  } catch (e) {
    rowError.value = { ...rowError.value, [p.id]: errorMessage(e) }
  }
}
</script>

<template>
  <main class="page">
    <p
      v-if="error"
      role="alert"
      class="form-error"
    >
      {{ error }}
    </p>
    <template v-if="book">
      <header class="book-head card">
        <h1>{{ book.title }}</h1>
        <p
          v-if="book.author"
          class="author"
        >
          {{ book.author }}
        </p>
        <dl class="facts">
          <div>
            <dt>Формат</dt>
            <dd>{{ formatName(book.source_format) }}</dd>
          </div>
          <div v-if="book.language">
            <dt>Язык</dt>
            <dd>{{ languageName(book.language) }}</dd>
          </div>
          <div>
            <dt>Добавлена</dt>
            <dd>{{ formatDate(book.created_at) }}</dd>
          </div>
        </dl>
        <div class="actions">
          <LinkButton
            :to="`/books/${book.id}/translate`"
            type="primary"
          >
            Новый перевод
          </LinkButton>
          <LinkButton :to="`/read/${book.id}`">
            Читать оригинал
          </LinkButton>
        </div>
      </header>

      <div class="columns">
        <section class="card">
          <h2>Переводы</h2>
          <p
            v-if="!projects.length"
            class="muted"
          >
            Переводов ещё нет.
          </p>
          <ul
            v-else
            class="projects"
          >
            <li
              v-for="p in projects"
              :key="p.id"
              :data-project="p.id"
            >
              <div class="project-head">
                <RouterLink :to="`/translations/${p.id}`">
                  <LangPair
                    :source="p.source_lang"
                    :target="p.target_lang"
                  />
                </RouterLink>
                <StatusMark :status="p.status" />
              </div>
              <p class="project-meta">
                {{ p.model_id }}, переведено {{ Math.round(p.progress_percent) }}%
              </p>
              <div class="project-actions">
                <LinkButton
                  v-if="p.segments_done > 0"
                  :to="`/read/${book.id}?t=${p.id}`"
                  size="small"
                >
                  Читать
                </LinkButton>
                <NButton
                  v-if="canResume(p)"
                  class="resume"
                  size="small"
                  type="primary"
                  secondary
                  @click="resume(p)"
                >
                  Продолжить
                </NButton>
              </div>
              <p
                v-if="rowError[p.id]"
                role="alert"
                class="form-error"
              >
                {{ rowError[p.id] }}
              </p>
            </li>
          </ul>
        </section>
        <section class="card">
          <h2>Оглавление</h2>
          <TocTree :entries="toc" />
        </section>
      </div>

      <section class="exports-history card">
        <h2>Экспорты</h2>
        <p
          v-if="exportError || exports.error.value"
          role="alert"
          class="form-error"
        >
          {{ exportError || exports.error.value }}
        </p>
        <p
          v-if="!exports.items.value.length"
          class="muted"
        >
          Экспортов ещё нет. Экспортировать перевод в EPUB, Word, Markdown, HTML или текст можно на странице перевода.
        </p>
        <ExportList
          v-else
          :items="exports.items.value"
          :languages="exportLanguages"
          @retry="retryExport"
          @remove="removeExport"
        />
      </section>
    </template>
  </main>
</template>

<style scoped>
.book-head {
  display: grid;
  gap: var(--space-2);
  justify-items: start;
}

h1 {
  font-family: var(--font-book);
  font-size: var(--step-3);
  font-weight: 600;
  max-width: var(--measure);
}

.author {
  margin: 0;
  font-family: var(--font-book);
  font-size: var(--step-1);
  color: var(--muted);
}

.facts {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2) var(--space-6);
  margin: var(--space-2) 0;
}

.facts dt {
  color: var(--muted);
  font-size: var(--step--1);
}

.facts dd {
  margin: 0;
}

.actions,
.project-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
}

.columns {
  display: grid;
  grid-template-columns: minmax(0, 3fr) minmax(0, 2fr);
  gap: var(--space-5);
  align-items: start;
  margin-top: var(--space-5);
}

h2 {
  font-size: var(--step-1);
  margin-bottom: var(--space-3);
}

.muted {
  margin: 0;
}

.projects {
  margin: 0;
  padding: 0;
  list-style: none;
}

.projects > li {
  display: grid;
  gap: var(--space-2);
  padding-block: var(--space-3);
  border-bottom: 1px solid var(--rule-soft);
}

.projects > li:first-child {
  padding-top: 0;
}

.projects > li:last-child {
  border-bottom: none;
  padding-bottom: 0;
}

.project-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-1) var(--space-3);
}

.project-meta {
  margin: 0;
  color: var(--muted);
  font-size: var(--step--1);
}

.exports-history {
  margin-top: var(--space-5);
}

@media (max-width: 48rem) {
  .columns {
    grid-template-columns: 1fr;
  }
}
</style>
