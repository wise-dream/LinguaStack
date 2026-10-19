<script setup lang="ts">
import { useRoute } from 'vue-router'

import type { Section } from '../router'

const route = useRoute()

const sections: { section: Section; to: string; label: string }[] = [
  { section: 'library', to: '/', label: 'Библиотека' },
  { section: 'translations', to: '/translations', label: 'Переводы' },
  { section: 'providers', to: '/settings/providers', label: 'Модели' },
]
</script>

<template>
  <header class="app-header">
    <RouterLink
      to="/"
      class="brand"
    >
      LinguaStack
    </RouterLink>
    <nav aria-label="Разделы">
      <RouterLink
        v-for="s in sections"
        :key="s.section"
        :to="s.to"
        :aria-current="route.meta.section === s.section ? 'page' : undefined"
      >
        {{ s.label }}
      </RouterLink>
    </nav>
    <div class="slot">
      <slot />
    </div>
  </header>
</template>

<style scoped>
.app-header {
  display: flex;
  align-items: baseline;
  gap: var(--space-5);
  flex-wrap: wrap;
  padding: var(--space-3) var(--space-4);
  border-bottom: 1px solid var(--rule);
}

.brand {
  font-family: var(--font-book);
  font-size: var(--step-1);
  font-weight: 600;
  text-decoration: none;
  letter-spacing: -0.01em;
}

nav {
  display: flex;
  gap: var(--space-4);
}

nav a {
  text-decoration: none;
  color: var(--muted);
  padding-block: var(--space-1);
  border-bottom: 2px solid transparent;
}

nav a:hover {
  color: var(--ink-original);
}

nav a[aria-current='page'] {
  color: var(--ink-original);
  border-bottom-color: var(--ink-original);
}

.slot {
  margin-left: auto;
}
</style>
