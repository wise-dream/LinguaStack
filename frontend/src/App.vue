<script setup lang="ts">
import { watchEffect } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import AppHeader from './components/AppHeader.vue'
import { useSession } from './session'

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
  <AppHeader v-if="session.me.value">
    <span class="who">{{ session.me.value.email }}</span>
    <button
      type="button"
      class="link-button"
      @click="logout"
    >
      Выйти
    </button>
  </AppHeader>
  <RouterView />
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

.link-button {
  background: none;
  border: none;
  padding: 0;
  text-decoration: underline;
  text-underline-offset: 0.2em;
  cursor: pointer;
}
</style>
