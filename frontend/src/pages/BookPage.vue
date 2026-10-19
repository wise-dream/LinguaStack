<script setup lang="ts">
// Книга: метаданные, оглавление и её переводы (ТЗ §8.2).
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'

import { getBook, getTOC } from '../api/books'
import { errorMessage } from '../api/client'
import { listTranslations, resumeTranslation } from '../api/translations'
import type { Book, Project, TocEntry } from '../api/types'
import LangPair from '../components/LangPair.vue'
import StatusMark from '../components/StatusMark.vue'
import TocTree from '../components/TocTree.vue'
import { formatDate, formatName, languageName } from '../lib/format'

const route = useRoute()
const id = String(route.params.id)

const book = ref<Book | null>(null)
const toc = ref<TocEntry[]>([])
const projects = ref<Project[]>([])
const error = ref('')
const rowError = ref<Record<string, string>>({})

onMounted(async () => {
  try {
    const [b, t, p] = await Promise.all([getBook(id), getTOC(id), listTranslations(id)])
    book.value = b
    toc.value = t
    projects.value = p
  } catch (e) {
    error.value = errorMessage(e)
  }
})

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
      <header class="book-head">
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
          <RouterLink
            :to="`/read/${book.id}`"
            class="button"
          >
            Читать оригинал
          </RouterLink>
          <RouterLink
            :to="`/books/${book.id}/translate`"
            class="button primary"
          >
            Новый перевод
          </RouterLink>
        </div>
      </header>

      <div class="columns">
        <section>
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
                <RouterLink
                  v-if="p.segments_done > 0"
                  :to="`/read/${book.id}?t=${p.id}`"
                  class="button"
                >
                  Читать
                </RouterLink>
                <button
                  v-if="canResume(p)"
                  type="button"
                  class="button resume"
                  @click="resume(p)"
                >
                  Продолжить
                </button>
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
        <section>
          <h2>Оглавление</h2>
          <TocTree :entries="toc" />
        </section>
      </div>
    </template>
  </main>
</template>

<style scoped>
.book-head {
  padding-bottom: var(--space-5);
  border-bottom: 1px solid var(--rule);
}

h1 {
  font-family: var(--font-book);
  font-size: var(--step-3);
  font-weight: 500;
  max-width: var(--measure);
}

.author {
  margin: var(--space-2) 0 0;
  font-family: var(--font-book);
  font-size: var(--step-1);
  color: var(--muted);
}

.facts {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2) var(--space-6);
  margin: var(--space-4) 0;
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
  gap: var(--space-3);
}

.columns {
  display: grid;
  grid-template-columns: minmax(0, 3fr) minmax(0, 2fr);
  gap: var(--space-6);
  margin-top: var(--space-5);
}

h2 {
  font-size: var(--step-1);
  margin-bottom: var(--space-3);
}

.muted {
  color: var(--muted);
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
  border-bottom: 1px solid var(--rule);
}

.project-head {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-1) var(--space-4);
}

.project-meta {
  margin: 0;
  color: var(--muted);
}

@media (max-width: 48rem) {
  .columns {
    grid-template-columns: 1fr;
  }
}
</style>
