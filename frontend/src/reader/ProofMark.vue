<script setup lang="ts">
// Корректорская пометка на полях: замечания проверки к фрагменту.
import { computed } from 'vue'

import type { Segment } from '../api/types'

const props = defineProps<{ segment: Segment }>()

const notes = computed(() => {
  const list = props.segment.findings.map((f) => f.message)
  if (!list.length && props.segment.last_error) list.push(props.segment.last_error.split('\n')[0] ?? '')
  return list
})
</script>

<template>
  <aside
    v-if="notes.length && segment.severity !== 'ok'"
    class="proof"
    :class="segment.severity"
  >
    <p
      v-for="(note, i) in notes"
      :key="i"
    >
      {{ note }}
    </p>
  </aside>
</template>

<style scoped>
.proof {
  font-size: var(--step--1);
  line-height: 1.4;
  padding-left: var(--space-2);
  border-left: 2px solid var(--highlight);
  color: var(--warn);
}

.proof.error {
  border-left-color: var(--proof);
  color: var(--proof);
}

.proof p {
  margin: 0;
}

.proof p + p {
  margin-top: var(--space-1);
}
</style>
