<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'

import type { Section } from '../router'
import { useSession } from '../session'

const route = useRoute()
const session = useSession()

const common: { section: Section; to: string; label: string }[] = [
  { section: 'library', to: '/', label: 'Библиотека' },
  { section: 'translations', to: '/translations', label: 'Переводы' },
  { section: 'providers', to: '/settings/providers', label: 'Модели' },
  { section: 'settings', to: '/settings', label: 'Настройки' },
]

// Администрирование — только администратору: остальным сервер всё равно
// ответит «не найдено».
const sections = computed(() =>
  session.me.value?.role === 'admin'
    ? [...common, { section: 'admin' as const, to: '/admin', label: 'Администрирование' }]
    : common,
)
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
  align-items: center;
  gap: var(--space-5);
  flex-wrap: wrap;
  padding: var(--space-2) var(--space-5);
  background: var(--surface);
  border-bottom: 1px solid var(--rule-soft);
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
  gap: var(--space-1);
}

nav a {
  text-decoration: none;
  color: var(--muted);
  padding: var(--space-1) var(--space-3);
  border-radius: var(--radius);
  transition: background-color 0.2s, color 0.2s;
}

nav a:hover {
  color: var(--ink-original);
  background: var(--paper);
}

nav a[aria-current='page'] {
  color: var(--ink-original);
  background: color-mix(in srgb, var(--ink-original) 8%, var(--surface));
  font-weight: 500;
}

.slot {
  margin-left: auto;
  display: flex;
  align-items: center;
}

/* Пять разделов на телефоне — без прокрутки вбок. */
@media (max-width: 40rem) {
  .app-header {
    gap: var(--space-2);
    padding: var(--space-2) var(--space-3);
  }

  nav {
    flex-wrap: wrap;
    gap: var(--space-1);
  }

  nav a {
    padding: var(--space-1) var(--space-2);
  }
}
</style>
