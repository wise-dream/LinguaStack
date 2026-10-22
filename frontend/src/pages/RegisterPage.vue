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
    <template #after-submit>
      <p class="consent">
        <!-- eslint-disable-next-line vue/singleline-html-element-content-newline -- перенос внутри ссылки дал бы лишний подчёркнутый пробел -->
        Создавая аккаунт, вы принимаете <RouterLink to="/terms">условия и правила обработки данных</RouterLink>.
      </p>
    </template>
    Уже есть аккаунт? <RouterLink :to="{ path: '/login', query: route.query }">
      Войти
    </RouterLink>
  </AuthForm>
</template>

<style scoped>
.consent {
  margin: 0;
  font-size: var(--step--1);
  color: var(--muted);
}
</style>
