<script setup lang="ts">
// Книга: метаданные, оглавление и её переводы (ТЗ §8.2).
import { NButton, NDescriptions, NDescriptionsItem, NEmpty, NList, NListItem } from 'naive-ui'
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'

import { getBook, getTOC } from '../api/books'
import { errorMessage } from '../api/client'
import { listTranslations, resumeTranslation } from '../api/translations'
import type { Book, ExportJob, Project, TocEntry } from '../api/types'
import ErrorAlert from '../components/ErrorAlert.vue'
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

// Продолжить — только свой перевод: чужая книга из общей библиотеки — читать и скачивать.
const canResume = (p: Project) => p.owned && (p.status === 'paused' || p.status === 'failed')

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
    <ErrorAlert :message="error" />
    <template v-if="book">
      <header class="book-head card">
        <h1>{{ book.title }}</h1>
        <p
          v-if="book.author"
          class="author"
        >
          {{ book.author }}
        </p>
        <NDescriptions
          class="facts"
          label-placement="top"
          :column="3"
          size="small"
        >
          <NDescriptionsItem label="Формат">
            {{ formatName(book.source_format) }}
          </NDescriptionsItem>
          <NDescriptionsItem
            v-if="book.language"
            label="Язык"
          >
            {{ languageName(book.language) }}
          </NDescriptionsItem>
          <NDescriptionsItem label="Добавлена">
            {{ formatDate(book.created_at) }}
          </NDescriptionsItem>
        </NDescriptions>
        <div class="actions">
          <LinkButton
            v-if="book.owned"
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
        <div class="main-col">
          <section class="card">
            <h2>Переводы</h2>
            <NEmpty
              v-if="!projects.length"
              size="small"
              description="Переводов ещё нет."
            />
            <NList
              v-else
              class="projects"
            >
              <NListItem
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
                <ErrorAlert
                  :message="rowError[p.id]"
                  size="small"
                />
              </NListItem>
            </NList>
          </section>
          <section class="exports-history card">
            <h2>Экспорты</h2>
            <ErrorAlert :message="exportError || exports.error.value" />
            <NEmpty
              v-if="!exports.items.value.length"
              size="small"
              description="Экспортов ещё нет. Экспортировать перевод в EPUB, Word, Markdown, HTML или текст можно на странице перевода."
            />
            <ExportList
              v-else
              :items="exports.items.value"
              :languages="exportLanguages"
              @retry="retryExport"
              @remove="removeExport"
            />
          </section>
        </div>
        <aside class="side">
          <section class="card">
            <h2>Оглавление</h2>
            <TocTree :entries="toc" />
          </section>
        </aside>
      </div>
    </template>
  </main>
</template>

<style scoped>
.book-head {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  column-gap: var(--space-6);
  row-gap: var(--space-2);
  justify-items: start;
}

/* Действия — справа от названия; на телефоне — под ним. */
.book-head .actions {
  grid-column: 2;
  grid-row: 1 / span 3;
  align-self: end;
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
  width: min(100%, 32rem);
  margin: var(--space-2) 0;
}

.actions,
.project-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
}

/* Слева — переводы и экспорты, справа — оглавление: на широком экране
   оно рядом и не уезжает при прокрутке. */
.columns {
  display: grid;
  grid-template-columns: minmax(0, 2fr) minmax(0, 1fr);
  gap: var(--space-5);
  align-items: start;
  margin-top: var(--space-5);
}

.main-col {
  display: grid;
  gap: var(--space-5);
}

.side {
  position: sticky;
  top: var(--space-4);
  max-height: calc(100vh - var(--space-6));
  overflow-y: auto;
}

h2 {
  font-size: var(--step-1);
  margin-bottom: var(--space-3);
}

.muted {
  margin: 0;
}

.projects :deep(.n-list-item__main) {
  display: grid;
  gap: var(--space-2);
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



@media (max-width: 56rem) {
  .book-head {
    grid-template-columns: minmax(0, 1fr);
  }

  .book-head .actions {
    grid-column: auto;
    grid-row: auto;
  }

  .columns {
    grid-template-columns: 1fr;
  }

  .side {
    position: static;
    max-height: none;
  }
}
</style>
