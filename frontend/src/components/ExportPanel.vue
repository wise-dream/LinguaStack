<script setup lang="ts">
// Экспорт перевода (ТЗ §3.2): формат, только перевод или вместе с
// оригиналом. Файл собирает worker; панель ждёт его и даёт скачать.
import { NButton, NRadio, NRadioGroup } from 'naive-ui'
import { onMounted, reactive, ref } from 'vue'

import { errorMessage } from '../api/client'
import type { ExportFormat, ExportJob, ExportMode } from '../api/types'
import { exportFormats, exportModes, useExports } from '../lib/exports'
import ErrorAlert from './ErrorAlert.vue'
import ExportList from './ExportList.vue'
import SelectField from './SelectField.vue'

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

const formatOptions = exportFormats.map((f) => ({ label: f.label, value: f.id }))
</script>

<template>
  <section
    class="export card"
    aria-labelledby="export-title"
  >
    <h2 id="export-title">
      Экспорт
    </h2>
    <form
      class="export-form"
      @submit.prevent="start(form.format, form.mode)"
    >
      <SelectField
        v-model="form.format"
        class="format"
        label="Формат файла"
        name="format"
        :options="formatOptions"
      />
      <fieldset class="modes">
        <legend>Что включить</legend>
        <NRadioGroup
          v-model:value="form.mode"
          name="mode"
        >
          <NRadio
            v-for="m in exportModes"
            :key="m.id"
            :value="m.id"
          >
            {{ m.label }}
          </NRadio>
        </NRadioGroup>
      </fieldset>
      <NButton
        attr-type="submit"
        type="primary"
        :loading="busy"
        :disabled="busy"
      >
        Экспортировать
      </NButton>
    </form>
    <ErrorAlert :message="error || exports.error.value" />
    <ExportList
      :items="exports.items.value"
      @retry="(j) => start(j.format, j.mode)"
      @remove="remove"
    />
  </section>
</template>

<style scoped>
.export-form {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: var(--space-3) var(--space-5);
}

/* Черта под формой — только когда под ней есть экспорты. */
.export :deep(.exports:has(li)) {
  margin-top: var(--space-4);
  padding-top: var(--space-4);
  border-top: 1px solid var(--rule-soft);
}

.export-form .format {
  min-width: 12rem;
}

.modes {
  border: none;
  margin: 0;
  padding: 0;
}

.modes :deep(.n-radio-group) {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2) var(--space-4);
  min-height: 38px;
  align-items: center;
}

.modes legend {
  font-weight: 500;
  padding: 0;
  margin-bottom: var(--space-2);
}

h2 {
  font-size: var(--step-1);
  margin-bottom: var(--space-3);
}
</style>
