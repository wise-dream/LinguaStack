<script setup lang="ts">
// Новый пароль по ссылке из письма, Telegram или от администратора.
// Ссылка одноразовая; после сохранения все сессии аккаунта закрыты.
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { confirmPasswordReset } from '../api/auth'
import { ApiError, errorMessage } from '../api/client'

const route = useRoute()
const router = useRouter()
const token = computed(() => (typeof route.query.token === 'string' ? route.query.token : ''))
const password = ref('')
const error = ref('')
const expired = ref(false)
const busy = ref(false)

async function submit() {
  error.value = ''
  expired.value = false
  busy.value = true
  try {
    await confirmPasswordReset(token.value, password.value)
    await router.replace({ path: '/login', query: { reset: 'done' } })
  } catch (e) {
    error.value = errorMessage(e)
    // 422 бывает и от короткого пароля — новая ссылка нужна, только если
    // сервер отверг саму ссылку.
    expired.value = e instanceof ApiError && e.status === 422 && error.value.includes('Ссылка')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="page auth">
    <h1>Новый пароль</h1>
    <template v-if="!token">
      <p>В ссылке нет кода сброса — откройте её из письма или сообщения целиком.</p>
      <p class="switch">
        <RouterLink to="/forgot-password">
          Запросить новую ссылку
        </RouterLink>
      </p>
    </template>
    <form
      v-else
      novalidate
      @submit.prevent="submit"
    >
      <label class="field">
        <span>Новый пароль</span>
        <input
          v-model="password"
          type="password"
          autocomplete="new-password"
          required
        >
        <small>Не короче 12 символов. После сохранения на всех устройствах нужно войти заново.</small>
      </label>
      <p
        v-if="error"
        role="alert"
        class="form-error"
      >
        {{ error }}
      </p>
      <RouterLink
        v-if="expired"
        to="/forgot-password"
      >
        Запросить новую ссылку
      </RouterLink>
      <button
        type="submit"
        class="button primary"
        :disabled="busy"
      >
        Сохранить пароль
      </button>
    </form>
  </main>
</template>

<style scoped>
.auth {
  max-width: 26rem;
}

h1 {
  font-family: var(--font-book);
  font-size: var(--step-3);
  font-weight: 500;
  margin-bottom: var(--space-5);
}

form {
  display: grid;
  gap: var(--space-4);
}

form > a {
  justify-self: start;
}

.switch {
  margin-top: var(--space-5);
  color: var(--muted);
}
</style>
