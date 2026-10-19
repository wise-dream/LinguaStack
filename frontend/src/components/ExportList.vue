<script setup lang="ts">
// Экспорты: формат, состав, состояние, скачивание. Ошибка сборки — с
// повтором; непереведённые фрагменты — честной строкой под файлом.
import { exportFileUrl } from '../api/exports'
import type { ExportJob } from '../api/types'
import { exportStatusLabel, formatLabel, modeLabel, untranslatedNote } from '../lib/exports'
import { formatBytes, formatDate, languageName } from '../lib/format'
import ConfirmButton from './ConfirmButton.vue'

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
  <ul class="exports">
    <li
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
        <span
          v-else
          class="state"
        >{{ exportStatusLabel(j.status) }}</span>
      </p>
      <p
        v-if="j.status === 'completed' && j.untranslated > 0"
        class="note"
      >
        {{ untranslatedNote(j.untranslated) }}
      </p>
      <p
        v-if="j.status === 'failed' && j.error"
        class="form-error"
      >
        {{ j.error }}
      </p>
      <div class="actions">
        <a
          v-if="j.status === 'completed'"
          class="button download"
          :href="exportFileUrl(j.id)"
          :download="j.file_name"
        >Скачать</a>
        <button
          v-if="j.status === 'failed'"
          type="button"
          class="button retry"
          @click="emit('retry', j)"
        >
          Повторить экспорт
        </button>
        <ConfirmButton
          v-if="j.status !== 'running'"
          label="Удалить"
          @confirm="emit('remove', j)"
        />
      </div>
    </li>
  </ul>
</template>

<style scoped>
.exports {
  margin: 0;
  padding: 0;
  list-style: none;
}

.exports > li {
  display: grid;
  gap: var(--space-2);
  padding-block: var(--space-3);
  border-bottom: 1px solid var(--rule);
}

.what {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-1) var(--space-3);
  align-items: baseline;
}

.meta,
.note {
  margin: 0;
  color: var(--muted);
}

.meta {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-1) var(--space-4);
}

.note {
  border-left: 3px solid var(--highlight);
  padding-left: var(--space-3);
  color: var(--ink-original);
}

.actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-3);
}
</style>
