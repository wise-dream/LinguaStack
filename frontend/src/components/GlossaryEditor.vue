<script setup lang="ts">
// Глоссарий перевода: термин → как его переводить всегда.
import { NButton } from 'naive-ui'

import TextField from './TextField.vue'
import type { GlossaryRow } from './glossary'

const rows = defineModel<GlossaryRow[]>({ required: true })

function add() {
  rows.value = [...rows.value, { source: '', target: '' }]
}

function remove(i: number) {
  rows.value = rows.value.filter((_, j) => j !== i)
}
</script>

<template>
  <fieldset class="glossary">
    <legend>Глоссарий</legend>
    <p class="hint">
      Термины, которые модель должна переводить всегда одинаково: имена, названия, понятия предметной области.
    </p>
    <div
      v-for="(row, i) in rows"
      :key="i"
      class="row"
    >
      <TextField
        v-model="row.source"
        :label="`Термин ${i + 1}`"
        placeholder="Термин"
        hide-label
      />
      <span aria-hidden="true">→</span>
      <TextField
        v-model="row.target"
        :label="`Перевод термина ${i + 1}`"
        placeholder="Перевод"
        hide-label
      />
      <NButton
        class="remove"
        quaternary
        :aria-label="`Убрать термин ${i + 1}`"
        @click="remove(i)"
      >
        ×
      </NButton>
    </div>
    <NButton
      class="add"
      dashed
      @click="add"
    >
      Добавить термин
    </NButton>
  </fieldset>
</template>

<style scoped>
.glossary {
  border: none;
  padding: 0;
  margin: 0;
  display: grid;
  gap: var(--space-2);
}

legend {
  font-weight: 500;
  padding: 0;
  margin-bottom: var(--space-1);
}

.hint {
  margin: 0;
  color: var(--muted);
}

.row {
  display: grid;
  grid-template-columns: 1fr auto 1fr auto;
  align-items: center;
  gap: var(--space-2);
}

.glossary > .add {
  justify-self: start;
}
</style>
