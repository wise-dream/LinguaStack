<script setup lang="ts">
// Восстановление доступа (ТЗ §2.1): ссылка уходит на почту или в Telegram.
// Ответ сервера одинаков для любого адреса — страница его и показывает.
import { ref } from 'vue'

import { requestPasswordReset } from '../api/auth'
import { errorMessage } from '../api/client'

const email = ref('')
const sent = ref('')
const error = ref('')
const busy = ref(false)

async function submit() {
  error.value = ''
  busy.value = true
  try {
    const res = await requestPasswordReset(email.value.trim())
    sent.value = res.message
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="page auth">
    <h1>Восстановление доступа</h1>
    <p
      v-if="sent"
      role="status"
      class="notice"
    >
      {{ sent }}
    </p>
    <form
      v-else
      novalidate
      @submit.prevent="submit"
    >
      <p class="muted">
        Пришлём ссылку для нового пароля на почту аккаунта или в привязанный Telegram.
      </p>
      <label class="field">
        <span>Электронная почта</span>
        <input
          v-model="email"
          type="email"
          autocomplete="email"
          required
        >
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
        Отправить ссылку
      </button>
    </form>
    <p class="switch">
      <RouterLink to="/login">
        Вернуться ко входу
      </RouterLink>
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

.muted {
  margin: 0;
  color: var(--muted);
}

.notice {
  margin: 0;
  color: var(--ink-translation);
  border-left: 3px solid var(--ink-translation);
  padding-left: var(--space-3);
}

.switch {
  margin-top: var(--space-5);
  color: var(--muted);
}
</style>
