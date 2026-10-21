<script setup lang="ts">
import { NButton, NConfigProvider, darkTheme, dateRuRU, ruRU } from 'naive-ui'
import { watchEffect } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import AppHeader from './components/AppHeader.vue'
import ThemeSwitch from './components/ThemeSwitch.vue'
import { useSession } from './session'
import { themeOverrides } from './styles/theme'
import { useTheme } from './theme'

const session = useSession()
const theme = useTheme()
const route = useRoute()
const router = useRouter()

watchEffect(() => {
  document.title = route.meta.title ? `${route.meta.title} — LinguaStack` : 'LinguaStack'
})

async function logout() {
  await session.logout()
  await router.push('/login')
}
</script>

<template>
  <NConfigProvider
    abstract
    :theme="theme.dark.value ? darkTheme : null"
    :theme-overrides="theme.dark.value ? themeOverrides.dark : themeOverrides.light"
    :locale="ruRU"
    :date-locale="dateRuRU"
  >
    <AppHeader v-if="session.me.value">
      <ThemeSwitch />
      <span class="who">{{ session.me.value.email }}</span>
      <NButton
        quaternary
        size="small"
        @click="logout"
      >
        Выйти
      </NButton>
    </AppHeader>
    <RouterView />
  </NConfigProvider>
</template>

<style scoped>
.who {
  color: var(--muted);
  margin: 0 var(--space-3) 0 var(--space-2);
}
</style>
