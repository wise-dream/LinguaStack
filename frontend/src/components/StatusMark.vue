<script setup lang="ts">
// Состояние перевода: цвет повторяет смысл текста, а не заменяет его.
import { NTag } from 'naive-ui'
import { computed } from 'vue'

import type { ProjectStatus } from '../api/types'
import { statusLabel } from '../lib/format'

const props = defineProps<{ status: ProjectStatus }>()

const tone = computed(() => {
  switch (props.status) {
    case 'completed':
      return 'success'
    case 'completed_with_warnings':
      return 'warning'
    case 'failed':
      return 'error'
    case 'running':
      return 'info'
    default:
      return 'default'
  }
})
</script>

<template>
  <NTag
    class="status"
    :type="tone"
    size="small"
    round
    :bordered="false"
  >
    {{ statusLabel(status) }}
  </NTag>
</template>
