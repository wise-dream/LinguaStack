<script setup lang="ts">
import { useRoute, useRouter } from 'vue-router'

import AuthForm from '../components/AuthForm.vue'
import { safeNext, useSession } from '../session'

const session = useSession()
const route = useRoute()
const router = useRouter()

async function submit(email: string, password: string) {
  await session.login(email, password)
  await router.replace(safeNext(route.query.next))
}
</script>

<template>
  <AuthForm
    title="Вход"
    submit-label="Войти"
    password-autocomplete="current-password"
    :submit="submit"
  >
    <template #notice>
      <p
        v-if="route.query.reset === 'done'"
        role="status"
        class="notice"
      >
        Пароль изменён. Войдите с новым паролем.
      </p>
      <p
        v-else-if="route.query.deleted"
        role="status"
        class="notice"
      >
        Аккаунт и все его данные удалены.
      </p>
    </template>
    <template #after-submit>
      <RouterLink
        to="/forgot-password"
        class="forgot"
      >
        Забыли пароль?
      </RouterLink>
    </template>
    Нет аккаунта? <RouterLink :to="{ path: '/register', query: route.query }">
      Зарегистрироваться
    </RouterLink>
  </AuthForm>
</template>

<style scoped>
.forgot {
  justify-self: center;
  color: var(--muted);
  font-size: var(--step--1);
}
</style>
