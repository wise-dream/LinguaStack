<script setup lang="ts">
// Счётчики и статистика провайдера (ТЗ §8.4): сделано, ошибки, замечания,
// время работы, запросы, токены и стоимость.
import { computed } from 'vue'

import type { Project } from '../api/types'
import { formatCost, formatDuration, formatNumber, plural } from '../lib/format'

const props = defineProps<{ project: Project; now: number }>()

const elapsed = computed(() => {
  const p = props.project
  if (!p.started_at) return ''
  const end = p.finished_at ? Date.parse(p.finished_at) : props.now
  return formatDuration(end - Date.parse(p.started_at))
})
</script>

<template>
  <p class="facts">
    <span>{{ formatNumber(project.segments_done) }} из {{ formatNumber(project.segments_total) }}</span>
    <span
      v-if="project.segments_failed"
      class="failed"
    >{{ formatNumber(project.segments_failed) }} с ошибкой</span>
    <span
      v-if="project.warnings_count"
      class="warn"
    >{{ formatNumber(project.warnings_count) }} с замечаниями</span>
    <span v-if="elapsed">{{ elapsed }}</span>
    <span v-if="project.provider_calls">
      {{ formatNumber(project.provider_calls) }} {{ plural(project.provider_calls, ['запрос', 'запроса', 'запросов']) }}
    </span>
    <span v-if="project.input_tokens || project.output_tokens">
      {{ formatNumber(project.input_tokens) }} / {{ formatNumber(project.output_tokens) }} токенов
    </span>
    <span v-if="project.cost_usd > 0">{{ formatCost(project.cost_usd) }}</span>
  </p>
</template>

<style scoped>
.facts {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-1) var(--space-4);
  margin: 0;
  color: var(--muted);
  font-variant-numeric: tabular-nums;
}

.facts > :first-child {
  color: var(--ink-original);
}

.failed {
  color: var(--proof);
}

.warn {
  color: #6b5600;
}
</style>
