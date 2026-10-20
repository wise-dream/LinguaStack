<script setup lang="ts">
// Новый пароль по ссылке из письма, Telegram или от администратора.
// Ссылка одноразовая; после сохранения все сессии аккаунта закрыты.
import { NButton } from 'naive-ui'
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { confirmPasswordReset } from '../api/auth'
import { ApiError, errorMessage } from '../api/client'
import AuthCard from '../components/AuthCard.vue'
import ErrorAlert from '../components/ErrorAlert.vue'
import TextField from '../components/TextField.vue'

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
  <AuthCard title="Новый пароль">
    <template v-if="!token">
      <p>В ссылке нет кода сброса — откройте её из письма или сообщения целиком.</p>
      <RouterLink to="/forgot-password">
        Запросить новую ссылку
      </RouterLink>
    </template>
    <form
      v-else
      novalidate
      @submit.prevent="submit"
    >
      <TextField
        v-model="password"
        label="Новый пароль"
        type="password"
        autocomplete="new-password"
        hint="Не короче 12 символов. После сохранения на всех устройствах нужно войти заново."
        required
      />
      <ErrorAlert :message="error" />
      <RouterLink
        v-if="expired"
        to="/forgot-password"
      >
        Запросить новую ссылку
      </RouterLink>
      <NButton
        attr-type="submit"
        type="primary"
        block
        :loading="busy"
        :disabled="busy"
      >
        Сохранить пароль
      </NButton>
    </form>
  </AuthCard>
</template>

<style scoped>
form {
  display: grid;
  gap: var(--space-4);
}

form > a {
  justify-self: start;
}

p {
  margin: 0;
}
</style>
