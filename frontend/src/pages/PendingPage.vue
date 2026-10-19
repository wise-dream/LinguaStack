<script setup lang="ts">
// Заглушка раздела, который ещё не реализован (план этапа 4).
import { useRoute } from 'vue-router'

const route = useRoute()
</script>

<template>
  <main class="page">
    <h1>{{ route.meta.title }}</h1>
  </main>
</template>
