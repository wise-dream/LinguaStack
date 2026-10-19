<script setup lang="ts">
// Направление перевода: «английский → русский». Стрелка — часть смысла.
import { languageName, sourceLanguageName } from '../lib/format'

defineProps<{ source: string; target: string }>()
</script>

<template>
  <span class="lang-pair">{{ sourceLanguageName(source) }} → {{ languageName(target) }}</span>
</template>
