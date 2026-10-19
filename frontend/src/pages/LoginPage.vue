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
    Нет аккаунта? <RouterLink :to="{ path: '/register', query: route.query }">
      Зарегистрироваться
    </RouterLink>
  </AuthForm>
</template>
