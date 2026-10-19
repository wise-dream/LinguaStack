<script setup lang="ts">
// Направление перевода: «английский → русский». Стрелка — часть смысла.
import { languageName } from '../lib/format'

defineProps<{ source: string; target: string }>()
</script>

<template>
  <span class="lang-pair">{{ languageName(source) }} → {{ languageName(target) }}</span>
</template>
