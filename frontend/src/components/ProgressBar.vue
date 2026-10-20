<script setup lang="ts">
// Полоса прогресса книги: переведённое — краской перевода, исчерпавшее
// попытки — корректорским красным.
import { computed } from 'vue'

const props = defineProps<{ done: number; failed: number; total: number }>()

const pct = (n: number) => (props.total > 0 ? (n / props.total) * 100 : 0)
const doneWidth = computed(() => `${pct(props.done)}%`)
const failedWidth = computed(() => `${pct(props.failed)}%`)
</script>

<template>
  <div
    class="bar"
    role="progressbar"
    :aria-valuenow="done + failed"
    aria-valuemin="0"
    :aria-valuemax="total"
    :aria-valuetext="`${done} из ${total}`"
  >
    <span
      class="done"
      :style="{ width: doneWidth }"
    />
    <span
      class="failed"
      :style="{ width: failedWidth }"
    />
  </div>
</template>

<style scoped>
.bar {
  display: flex;
  height: 0.5rem;
  background: var(--rule-soft);
  border-radius: 999px;
  overflow: hidden;
}

.done {
  background: var(--ink-translation);
  transition: width 0.4s ease;
}

.failed {
  background: var(--proof);
}
</style>
