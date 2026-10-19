<script setup lang="ts">
// Экспорт перевода (ТЗ §3.2): формат, только перевод или вместе с
// оригиналом. Файл собирает worker; панель ждёт его и даёт скачать.
import { onMounted, reactive, ref } from 'vue'

import { errorMessage } from '../api/client'
import type { ExportFormat, ExportJob, ExportMode } from '../api/types'
import { exportFormats, exportModes, useExports } from '../lib/exports'
import ExportList from './ExportList.vue'

const props = defineProps<{ bookId: string; translationId: string }>()

const form = reactive<{ format: ExportFormat; mode: ExportMode }>({ format: 'epub', mode: 'translation' })
const busy = ref(false)
const error = ref('')
const exports = useExports(
  () => props.bookId,
  () => props.translationId,
)

onMounted(() => void exports.load())

async function start(format: ExportFormat, mode: ExportMode) {
  busy.value = true
  error.value = ''
  try {
    await exports.create({ translation_id: props.translationId, format, mode })
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    busy.value = false
  }
}

async function remove(j: ExportJob) {
  error.value = ''
  try {
    await exports.remove(j.id)
  } catch (e) {
    error.value = errorMessage(e)
  }
}
</script>

<template>
  <section
    class="export"
    aria-labelledby="export-title"
  >
    <h2 id="export-title">
      Экспорт
    </h2>
    <form
      class="export-form"
      @submit.prevent="start(form.format, form.mode)"
    >
      <div class="field">
        <label for="export-format">Формат файла</label>
        <select
          id="export-format"
          v-model="form.format"
          name="format"
        >
          <option
            v-for="f in exportFormats"
            :key="f.id"
            :value="f.id"
          >
            {{ f.label }}
          </option>
        </select>
      </div>
      <fieldset class="modes">
        <legend>Что включить</legend>
        <label
          v-for="m in exportModes"
          :key="m.id"
        >
          <input
            v-model="form.mode"
            type="radio"
            name="mode"
            :value="m.id"
          >
          {{ m.label }}
        </label>
      </fieldset>
      <button
        type="submit"
        class="button primary"
        :disabled="busy"
      >
        Экспортировать
      </button>
    </form>
    <p
      v-if="error || exports.error.value"
      role="alert"
      class="form-error"
    >
      {{ error || exports.error.value }}
    </p>
    <ExportList
      :items="exports.items.value"
      @retry="(j) => start(j.format, j.mode)"
      @remove="remove"
    />
  </section>
</template>

<style scoped>
.export {
  margin-top: var(--space-6);
}

.export-form {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: var(--space-3) var(--space-5);
  margin-bottom: var(--space-3);
}

.export-form .field {
  min-width: 12rem;
}

.modes {
  border: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2) var(--space-4);
}

.modes legend {
  font-weight: 500;
  padding: 0;
  margin-bottom: var(--space-2);
}

.modes label {
  display: flex;
  gap: var(--space-2);
  align-items: center;
  cursor: pointer;
}

h2 {
  font-size: var(--step-1);
  margin-bottom: var(--space-3);
}
</style>
