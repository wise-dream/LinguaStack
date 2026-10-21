<script setup lang="ts">
import { NButton, NPopselect } from 'naive-ui'
import { computed } from 'vue'

import { useTheme, type ThemeChoice } from '../theme'

const theme = useTheme()

const options: { value: ThemeChoice; label: string }[] = [
  { value: 'system', label: 'Как в системе' },
  { value: 'light', label: 'Светлая' },
  { value: 'dark', label: 'Тёмная' },
]

const current = computed(() => options.find((o) => o.value === theme.choice.value)!)
const label = computed(() => `Тема: ${current.value.label.toLowerCase()}`)

function choose(value: ThemeChoice) {
  theme.set(value)
}
</script>

<template>
  <NPopselect
    :value="theme.choice.value"
    :options="options"
    trigger="click"
    placement="bottom-end"
    @update:value="choose"
  >
    <NButton
      quaternary
      size="small"
      class="theme-switch"
      :aria-label="label"
      :title="label"
    >
      <!-- Значок — выбор человека, а не вычисленная тема: «как в системе»
           отдельным знаком, иначе его не отличить от явного выбора. -->
      <svg
        viewBox="0 0 24 24"
        width="18"
        height="18"
        aria-hidden="true"
        fill="none"
        stroke="currentColor"
        stroke-width="1.8"
        stroke-linecap="round"
        stroke-linejoin="round"
      >
        <template v-if="current.value === 'light'">
          <circle
            cx="12"
            cy="12"
            r="4"
          />
          <path d="M12 2.5v2M12 19.5v2M2.5 12h2M19.5 12h2M5.3 5.3l1.4 1.4M17.3 17.3l1.4 1.4M5.3 18.7l1.4-1.4M17.3 6.7l1.4-1.4" />
        </template>
        <path
          v-else-if="current.value === 'dark'"
          d="M20 14.5A8 8 0 0 1 9.5 4a8 8 0 1 0 10.5 10.5z"
        />
        <template v-else>
          <circle
            cx="12"
            cy="12"
            r="8.5"
          />
          <path
            d="M12 3.5a8.5 8.5 0 0 1 0 17z"
            fill="currentColor"
          />
        </template>
      </svg>
    </NButton>
  </NPopselect>
</template>
