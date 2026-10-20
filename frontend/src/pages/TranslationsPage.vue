<script setup lang="ts">
// Монитор переводов (ТЗ §8.4): идущие, ждущие и завершённые задачи с
// прогрессом, ошибками, временем работы и статистикой провайдера.
// Список опрашивается раз в 5 с, пока есть активные переводы: по SSE на
// строку браузер исчерпал бы соединения с сервером.
import { NEmpty, NList, NListItem } from 'naive-ui'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

import { errorMessage } from '../api/client'
import { listTranslations } from '../api/translations'
import type { Project, ProjectStatus } from '../api/types'
import ErrorAlert from '../components/ErrorAlert.vue'
import LangPair from '../components/LangPair.vue'
import LinkButton from '../components/LinkButton.vue'
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
    <ErrorAlert :message="error" />

    <section
      v-if="projects && !projects.length"
      class="empty card"
    >
      <NEmpty description="Переводов пока нет. Откройте книгу в библиотеке и начните перевод.">
        <template #extra>
          <LinkButton to="/">
            В библиотеку
          </LinkButton>
        </template>
      </NEmpty>
    </section>

    <section
      v-for="g in grouped"
      :key="g.id"
      :data-group="g.id"
      class="group card"
    >
      <h2>{{ g.title }} <span class="count">{{ g.items.length }}</span></h2>
      <NList class="rows">
        <NListItem
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
        </NListItem>
      </NList>
    </section>
  </main>
</template>

<style scoped>
.group + .group {
  margin-top: var(--space-5);
}

h2 {
  display: flex;
  align-items: baseline;
  gap: var(--space-2);
  font-size: var(--step-1);
}

.count {
  color: var(--muted);
  font-weight: 400;
  font-size: var(--step-0);
  font-variant-numeric: tabular-nums;
}

.rows {
  background: transparent;
}

.rows :deep(.n-list-item__main) {
  display: grid;
  gap: var(--space-2);
}

.row-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2) var(--space-4);
}

.title {
  font-family: var(--font-book);
  font-size: var(--step-1);
  font-weight: 500;
  text-decoration: none;
}

.title:hover {
  text-decoration: underline;
}

.sub {
  margin: 0;
  color: var(--muted);
  font-size: var(--step--1);
}

.last-error {
  margin: 0;
  padding: var(--space-2) var(--space-3);
  color: var(--proof);
  background: color-mix(in srgb, var(--proof) 7%, var(--surface));
  border-radius: 8px;
}

.empty {
  padding-block: var(--space-6);
}
</style>
