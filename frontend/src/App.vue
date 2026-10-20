<script setup lang="ts">
import { NButton, NConfigProvider, dateRuRU, ruRU } from 'naive-ui'
import { watchEffect } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import AppHeader from './components/AppHeader.vue'
import { useSession } from './session'
import { themeOverrides } from './styles/theme'

const session = useSession()
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
    :theme-overrides="themeOverrides"
    :locale="ruRU"
    :date-locale="dateRuRU"
  >
    <AppHeader v-if="session.me.value">
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
  margin-right: var(--space-3);
}

/* На телефоне адрес почты съедает строку шапки — остаётся «Выйти». */
@media (max-width: 40rem) {
  .who {
    display: none;
  }
}
</style>
