<script setup lang="ts">
// Общая форма входа и регистрации: два поля, ошибка сервера у формы.
import { ref } from 'vue'

import { errorMessage } from '../api/client'

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
  <main class="page auth">
    <h1>{{ title }}</h1>
    <form
      novalidate
      @submit.prevent="onSubmit"
    >
      <label class="field">
        <span>Электронная почта</span>
        <input
          v-model="email"
          type="email"
          autocomplete="email"
          required
        >
      </label>
      <label class="field">
        <span>Пароль</span>
        <input
          v-model="password"
          type="password"
          :autocomplete="passwordAutocomplete"
          required
        >
        <small v-if="passwordHint">{{ passwordHint }}</small>
      </label>
      <p
        v-if="error"
        role="alert"
        class="form-error"
      >
        {{ error }}
      </p>
      <button
        type="submit"
        class="button primary"
        :disabled="busy"
      >
        {{ submitLabel }}
      </button>
    </form>
    <p class="switch">
      <slot />
    </p>
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

.switch {
  margin-top: var(--space-5);
  color: var(--muted);
}
</style>
