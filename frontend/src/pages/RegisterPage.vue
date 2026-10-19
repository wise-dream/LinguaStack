<script setup lang="ts">
import { useRoute, useRouter } from 'vue-router'

import AuthForm from '../components/AuthForm.vue'
import { safeNext, useSession } from '../session'

const session = useSession()
const route = useRoute()
const router = useRouter()

async function submit(email: string, password: string) {
  await session.register(email, password)
  await router.replace(safeNext(route.query.next))
}
</script>

<template>
  <AuthForm
    title="Регистрация"
    submit-label="Создать аккаунт"
    password-hint="Не короче 12 символов. Подойдёт фраза из нескольких слов."
    password-autocomplete="new-password"
    :submit="submit"
  >
    Уже есть аккаунт? <RouterLink :to="{ path: '/login', query: route.query }">
      Войти
    </RouterLink>
  </AuthForm>
</template>
