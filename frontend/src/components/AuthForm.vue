<script setup lang="ts">
// Общая форма входа и регистрации: два поля, ошибка сервера у формы.
import { NButton } from 'naive-ui'
import { ref } from 'vue'

import { errorMessage } from '../api/client'
import AuthCard from './AuthCard.vue'
import ErrorAlert from './ErrorAlert.vue'
import TextField from './TextField.vue'

const props = defineProps<{
  title: string
  submitLabel: string
  passwordHint?: string
  passwordAutocomplete: 'current-password' | 'new-password'
  submit: (email: string, password: string) => Promise<void>
}>()

const email = ref('')
const password = ref('')
const error = ref('')
const busy = ref(false)

async function onSubmit() {
  error.value = ''
  busy.value = true
  try {
    await props.submit(email.value.trim(), password.value)
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <AuthCard :title="title">
    <slot name="notice" />
    <form
      novalidate
      @submit.prevent="onSubmit"
    >
      <TextField
        v-model="email"
        label="Электронная почта"
        type="email"
        autocomplete="email"
        required
      />
      <TextField
        v-model="password"
        label="Пароль"
        type="password"
        :autocomplete="passwordAutocomplete"
        :hint="passwordHint"
        required
      />
      <ErrorAlert :message="error" />
      <NButton
        attr-type="submit"
        type="primary"
        block
        :loading="busy"
        :disabled="busy"
      >
        {{ submitLabel }}
      </NButton>
      <slot name="after-submit" />
    </form>
    <template #footer>
      <slot />
    </template>
  </AuthCard>
</template>

<style scoped>
form {
  display: grid;
  gap: var(--space-4);
}
</style>
