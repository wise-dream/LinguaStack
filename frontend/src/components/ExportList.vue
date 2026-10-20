<script setup lang="ts">
// Экспорты: формат, состав, состояние, скачивание. Ошибка сборки — с
// повтором; непереведённые фрагменты — честной строкой под файлом.
import { NAlert, NButton, NList, NListItem, NTag } from 'naive-ui'

import { exportFileUrl } from '../api/exports'
import type { ExportJob } from '../api/types'
import { exportStatusLabel, formatLabel, modeLabel, untranslatedNote } from '../lib/exports'
import { formatBytes, formatDate, languageName } from '../lib/format'
import ConfirmButton from './ConfirmButton.vue'
import ErrorAlert from './ErrorAlert.vue'

const props = defineProps<{
  items: ExportJob[]
  /** Язык перевода по его id — для истории всех экспортов книги. */
  languages?: Record<string, string>
}>()
const emit = defineEmits<{ retry: [job: ExportJob]; remove: [job: ExportJob] }>()

const lang = (j: ExportJob) => {
  const code = props.languages?.[j.translation_id]
  return code ? `, ${languageName(code)}` : ''
}
</script>

<template>
  <NList class="exports">
    <NListItem
      v-for="j in props.items"
      :key="j.id"
      :data-export="j.id"
      :class="j.status"
    >
      <div class="what">
        <strong>{{ formatLabel(j.format) }}</strong>
        <span>{{ modeLabel(j.mode).toLowerCase() }}{{ lang(j) }}</span>
      </div>
      <p class="meta">
        <span>{{ formatDate(j.created_at) }}</span>
        <span
          v-if="j.status === 'completed'"
          class="size"
        >{{ formatBytes(j.size_bytes) }}</span>
        <NTag
          v-else
          class="state"
          size="small"
          round
          :bordered="false"
          :type="j.status === 'failed' ? 'error' : 'info'"
        >
          {{ exportStatusLabel(j.status) }}
        </NTag>
      </p>
      <NAlert
        v-if="j.status === 'completed' && j.untranslated > 0"
        class="note"
        type="warning"
        :show-icon="false"
      >
        {{ untranslatedNote(j.untranslated) }}
      </NAlert>
      <ErrorAlert
        v-if="j.status === 'failed'"
        :message="j.error"
        size="small"
      />
      <div class="actions">
        <NButton
          v-if="j.status === 'completed'"
          tag="a"
          class="download"
          size="small"
          type="primary"
          :href="exportFileUrl(j.id)"
          :download="j.file_name"
        >
          Скачать
        </NButton>
        <NButton
          v-if="j.status === 'failed'"
          class="retry"
          size="small"
          @click="emit('retry', j)"
        >
          Повторить экспорт
        </NButton>
        <ConfirmButton
          v-if="j.status !== 'running'"
          label="Удалить"
          size="small"
          @confirm="emit('remove', j)"
        />
      </div>
    </NListItem>
  </NList>
</template>

<style scoped>
.exports {
  background: transparent;
}

.exports :deep(.n-list-item__main) {
  display: grid;
  gap: var(--space-2);
}

.what {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-1) var(--space-3);
  align-items: baseline;
}

.meta {
  margin: 0;
  color: var(--muted);
  font-size: var(--step--1);
}

.meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-1) var(--space-4);
}

.note :deep(.n-alert-body) {
  padding: var(--space-1) var(--space-3);
}

.actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
}
</style>
