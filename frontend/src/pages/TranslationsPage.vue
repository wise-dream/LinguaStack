<script setup lang="ts">
// Монитор переводов (ТЗ §8.4): идущие, ждущие и завершённые задачи с
// прогрессом, ошибками, временем работы и статистикой провайдера.
// Список опрашивается раз в 5 с, пока есть активные переводы: по SSE на
// строку браузер исчерпал бы соединения с сервером.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

import { errorMessage } from '../api/client'
import { listTranslations } from '../api/translations'
import type { Project, ProjectStatus } from '../api/types'
import LangPair from '../components/LangPair.vue'
import ProgressBar from '../components/ProgressBar.vue'
import ProjectFacts from '../components/ProjectFacts.vue'
import StatusMark from '../components/StatusMark.vue'
import { useNow } from '../lib/now'

const REFRESH_MS = 5_000

const projects = ref<Project[] | null>(null)
const error = ref('')
const now = useNow()
let timer: ReturnType<typeof setTimeout> | undefined

const groups: { id: string; title: string; statuses: ProjectStatus[] }[] = [
  { id: 'active', title: 'Идут', statuses: ['running'] },
  { id: 'waiting', title: 'Ждут', statuses: ['queued', 'paused', 'draft'] },
  { id: 'finished', title: 'Завершены', statuses: ['completed', 'completed_with_warnings', 'failed', 'cancelled'] },
]

const grouped = computed(() =>
  groups
    .map((g) => ({ ...g, items: (projects.value ?? []).filter((p) => g.statuses.includes(p.status)) }))
    .filter((g) => g.items.length),
)

const hasActive = computed(() => (projects.value ?? []).some((p) => p.status === 'running' || p.status === 'queued'))

async function load() {
  try {
    projects.value = await listTranslations()
    error.value = ''
  } catch (e) {
    error.value = errorMessage(e)
  }
  clearTimeout(timer)
  if (hasActive.value) timer = setTimeout(load, REFRESH_MS)
}

onMounted(load)
onBeforeUnmount(() => clearTimeout(timer))
</script>

<template>
  <main class="page">
    <h1>Переводы</h1>
    <p
      v-if="error"
      role="alert"
      class="form-error"
    >
      {{ error }}
    </p>

    <section
      v-if="projects && !projects.length"
      class="empty"
    >
      <p>Переводов пока нет. Откройте книгу в библиотеке и начните перевод.</p>
      <RouterLink
        to="/"
        class="button"
      >
        В библиотеку
      </RouterLink>
    </section>

    <section
      v-for="g in grouped"
      :key="g.id"
      :data-group="g.id"
      class="group"
    >
      <h2>{{ g.title }}</h2>
      <ul class="rows">
        <li
          v-for="p in g.items"
          :key="p.id"
          :data-project="p.id"
        >
          <div class="row-head">
            <RouterLink
              :to="`/translations/${p.id}`"
              class="title"
            >
              {{ p.book_title }}
            </RouterLink>
            <StatusMark :status="p.status" />
          </div>
          <p class="sub">
            <LangPair
              :source="p.source_lang"
              :target="p.target_lang"
            />, {{ p.model_id }}
          </p>
          <ProgressBar
            :done="p.segments_done"
            :failed="p.segments_failed"
            :total="p.segments_total"
          />
          <ProjectFacts
            :project="p"
            :now="now"
          />
          <p
            v-if="p.last_error"
            class="last-error"
          >
            {{ p.last_error }}
          </p>
        </li>
      </ul>
    </section>
  </main>
</template>

<style scoped>
h1 {
  font-family: var(--font-book);
  font-size: var(--step-3);
  font-weight: 500;
  margin-bottom: var(--space-5);
}

.group + .group {
  margin-top: var(--space-6);
}

h2 {
  font-size: var(--step-1);
  padding-bottom: var(--space-2);
  border-bottom: 1px solid var(--ink-original);
}

.rows {
  margin: 0;
  padding: 0;
  list-style: none;
}

.rows > li {
  display: grid;
  gap: var(--space-2);
  padding-block: var(--space-4);
  border-bottom: 1px solid var(--rule);
}

.row-head {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--space-2) var(--space-4);
}

.title {
  font-family: var(--font-book);
  font-size: var(--step-1);
  text-decoration: none;
}

.title:hover {
  text-decoration: underline;
}

.sub {
  margin: 0;
  color: var(--muted);
}

.last-error {
  margin: 0;
  color: var(--proof);
}

.empty p {
  color: var(--muted);
}
</style>
