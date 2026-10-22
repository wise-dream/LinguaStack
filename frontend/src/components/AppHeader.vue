<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'
import { useRoute } from 'vue-router'

import type { Section } from '../router'
import { useSession } from '../session'

const route = useRoute()
const session = useSession()

const common: { section: Section; to: string; label: string }[] = [
  { section: 'library', to: '/', label: 'Библиотека' },
  { section: 'shared', to: '/shared', label: 'Общая библиотека' },
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

// На узком экране разделы и аккаунт прячутся в меню под кнопкой. Разметка
// одна на обе ширины: на широком экране меню — просто часть шапки.
const menuId = useId()
const open = ref(false)
const burger = ref<HTMLButtonElement | null>(null)

watch(
  () => route.fullPath,
  () => {
    open.value = false
  },
)

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && open.value) {
    open.value = false
    burger.value?.focus()
  }
}

onMounted(() => document.addEventListener('keydown', onKeydown))
onBeforeUnmount(() => document.removeEventListener('keydown', onKeydown))
</script>

<template>
  <header class="app-header">
    <RouterLink
      to="/"
      class="brand"
    >
      LinguaStack
    </RouterLink>
    <button
      ref="burger"
      type="button"
      class="burger"
      aria-label="Меню"
      :aria-expanded="open"
      :aria-controls="menuId"
      @click="open = !open"
    >
      <svg
        viewBox="0 0 24 24"
        width="22"
        height="22"
        aria-hidden="true"
        fill="none"
        stroke="currentColor"
        stroke-width="1.8"
        stroke-linecap="round"
      >
        <path
          v-if="open"
          d="M6 6l12 12M18 6L6 18"
        />
        <path
          v-else
          d="M4 7h16M4 12h16M4 17h16"
        />
      </svg>
    </button>
    <div
      :id="menuId"
      class="menu"
      :class="{ open }"
    >
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
    </div>
  </header>
  <!-- Затемнение под открытым меню: нажатие мимо меню закрывает его. -->
  <div
    v-if="open"
    class="scrim"
    aria-hidden="true"
    @click="open = false"
  />
</template>

<style scoped>
.app-header {
  display: flex;
  align-items: center;
  gap: var(--space-5);
  padding: var(--space-2) var(--page-inset);
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

/* На широком экране обёртка меню не видна раскладке: разделы и аккаунт —
   прямо в строке шапки. */
.menu {
  display: contents;
}

/* Тесно — разделы переходят на вторую строку целиком, а не по словам. */
nav {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-1);
  min-width: 0;
}

nav a {
  white-space: nowrap;
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
  min-width: 0;
}

.slot > :slotted(.who) {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.burger,
.scrim {
  display: none;
}

/* Узкий экран: в строке шапки — название и кнопка меню; разделы и аккаунт
   выпадают панелью поверх страницы. */
@media (max-width: 56rem) {
  .app-header {
    position: relative;
    z-index: 30;
    justify-content: space-between;
  }

  .burger {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 2.75rem;
    height: 2.75rem;
    margin-right: calc(-1 * var(--space-2));
    padding: 0;
    border: none;
    border-radius: var(--radius);
    background: none;
    color: var(--ink-original);
    cursor: pointer;
  }

  .burger:hover,
  .burger[aria-expanded='true'] {
    background: var(--paper);
  }

  .menu {
    display: none;
  }

  .menu.open {
    display: grid;
    gap: var(--space-3);
    position: absolute;
    top: 100%;
    left: 0;
    right: 0;
    padding: var(--space-2) var(--page-inset) var(--space-4);
    background: var(--surface);
    border-bottom: 1px solid var(--rule-soft);
    box-shadow: var(--shadow-pop);
  }

  nav {
    flex-direction: column;
    gap: 0;
  }

  /* Пункты — во всю ширину и не ниже 44 px: в них попадает палец. */
  nav a {
    display: flex;
    align-items: center;
    min-height: 2.75rem;
    padding: 0 var(--space-3);
    font-size: var(--step-1);
  }

  /* Аккаунт под разделами: почта строкой, ниже тема и выход. */
  .slot {
    margin-left: 0;
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    justify-items: start;
    align-items: center;
    gap: var(--space-2);
    padding-top: var(--space-3);
    border-top: 1px solid var(--rule-soft);
  }

  .slot > :slotted(.who) {
    grid-column: 1 / -1;
    grid-row: 1;
    max-width: 100%;
    margin: 0;
    padding-inline: var(--space-3);
  }

  .scrim {
    display: block;
    position: fixed;
    inset: 0;
    z-index: 20;
    background: var(--scrim);
  }
}
</style>
