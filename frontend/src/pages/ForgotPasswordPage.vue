<script setup lang="ts">
// Восстановление доступа (ТЗ §2.1): ссылка уходит на почту или в Telegram.
// Ответ сервера одинаков для любого адреса — страница его и показывает.
import { NButton } from 'naive-ui'
import { ref } from 'vue'

import { requestPasswordReset } from '../api/auth'
import { errorMessage } from '../api/client'
import AuthCard from '../components/AuthCard.vue'
import ErrorAlert from '../components/ErrorAlert.vue'
import TextField from '../components/TextField.vue'

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
  <AuthCard title="Восстановление доступа">
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
      <TextField
        v-model="email"
        label="Электронная почта"
        type="email"
        autocomplete="email"
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
        Отправить ссылку
      </NButton>
    </form>
    <template #footer>
      <RouterLink to="/login">
        Вернуться ко входу
      </RouterLink>
    </template>
  </AuthCard>
</template>

<style scoped>
form {
  display: grid;
  gap: var(--space-4);
}

.muted {
  margin: 0;
}
</style>
