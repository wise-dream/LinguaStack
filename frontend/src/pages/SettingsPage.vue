<script setup lang="ts">
// Настройки аккаунта и Telegram (ТЗ §2.1, §8.1 №11, §9): привязка по
// одноразовой ссылке на бота и выбор уведомлений.
import { onBeforeUnmount, onMounted, ref } from 'vue'

import { errorMessage } from '../api/client'
import { getTelegram, linkTelegram, patchTelegram, unlinkTelegram } from '../api/telegram'
import type { TelegramLink, TelegramSettings, TelegramState } from '../api/types'
import ConfirmButton from '../components/ConfirmButton.vue'
import { formatDate } from '../lib/format'
import { useSession } from '../session'

const POLL_MS = 3000

const session = useSession()
const telegram = ref<TelegramState | null>(null)
const link = ref<TelegramLink | null>(null)
const error = ref('')
const done = ref('')
let timer: ReturnType<typeof setTimeout> | undefined
let stopped = false

const notifications: { key: keyof TelegramSettings; label: string }[] = [
  { key: 'notify_chapters', label: 'Глава переведена' },
  { key: 'notify_completed', label: 'Книга переведена' },
  { key: 'notify_errors', label: 'Перевод остановлен из-за ошибки' },
]

onMounted(async () => {
  try {
    const [, t] = await Promise.all([session.load(), getTelegram()])
    telegram.value = t
  } catch (e) {
    error.value = errorMessage(e)
  }
})

onBeforeUnmount(() => {
  stopped = true
  clearTimeout(timer)
})

const expiresAt = (iso: string) => new Date(iso).toLocaleTimeString('ru', { hour: '2-digit', minute: '2-digit' })

// Ждём, пока пользователь нажмёт «Старт» в Telegram: привязку делает бот.
function poll() {
  clearTimeout(timer)
  if (stopped) return
  timer = setTimeout(async () => {
    try {
      const t = await getTelegram()
      telegram.value = t
      if (t.linked) {
        link.value = null
        done.value = 'Telegram привязан'
        return
      }
    } catch (e) {
      error.value = errorMessage(e)
    }
    poll()
  }, POLL_MS)
}

async function bind() {
  error.value = ''
  done.value = ''
  try {
    link.value = await linkTelegram()
    poll()
  } catch (e) {
    error.value = errorMessage(e)
  }
}

async function toggle(key: keyof TelegramSettings, value: boolean) {
  error.value = ''
  try {
    telegram.value = await patchTelegram({ [key]: value })
  } catch (e) {
    error.value = errorMessage(e)
  }
}

async function unlink() {
  error.value = ''
  done.value = ''
  try {
    await unlinkTelegram()
    if (telegram.value) telegram.value = { ...telegram.value, linked: false, username: undefined, linked_at: undefined }
  } catch (e) {
    error.value = errorMessage(e)
  }
}
</script>

<template>
  <main class="page settings">
    <h1>Настройки</h1>

    <section
      class="account"
      aria-labelledby="account-title"
    >
      <h2 id="account-title">
        Аккаунт
      </h2>
      <dl>
        <dt>Электронная почта</dt>
        <dd>{{ session.me.value?.email }}</dd>
      </dl>
      <RouterLink to="/settings/providers">
        Подключения моделей
      </RouterLink>
    </section>

    <section
      class="telegram"
      aria-labelledby="telegram-title"
    >
      <h2 id="telegram-title">
        Telegram
      </h2>
      <p
        v-if="error"
        role="alert"
        class="form-error"
      >
        {{ error }}
      </p>
      <p
        v-if="done"
        role="status"
        class="done"
      >
        {{ done }}
      </p>
      <template v-if="telegram">
        <p
          v-if="!telegram.enabled"
          class="muted"
        >
          Telegram-бот на этом сервере не настроен.
        </p>

        <template v-else-if="telegram.linked">
          <p>
            Привязан{{ telegram.username ? ` к @${telegram.username}` : '' }}{{ telegram.linked_at ? `, с ${formatDate(telegram.linked_at)}` : '' }}.
            Бот пишет сюда:
          </p>
          <fieldset class="toggles">
            <legend class="visually-hidden">
              Уведомления
            </legend>
            <label
              v-for="n in notifications"
              :key="n.key"
            >
              <input
                type="checkbox"
                :name="n.key"
                :checked="telegram[n.key]"
                @change="toggle(n.key, ($event.target as HTMLInputElement).checked)"
              >
              {{ n.label }}
            </label>
          </fieldset>
          <p class="muted">
            В Telegram команда /status показывает ход переводов и ставит их на паузу.
          </p>
          <ConfirmButton
            label="Отвязать Telegram"
            confirm-label="Точно отвязать?"
            @confirm="unlink"
          />
        </template>

        <template v-else>
          <p>
            Бот пришлёт сообщение, когда глава или книга переведена или перевод остановился из-за ошибки.
            Командой /status в Telegram можно смотреть ход переводов и ставить их на паузу.
          </p>
          <div
            v-if="link"
            class="link"
          >
            <p>
              Откройте бота и нажмите «Старт» — ссылка действует до {{ expiresAt(link.expires_at) }}.
              Эта страница обновится сама.
            </p>
            <a
              class="button primary open-bot"
              :href="link.url"
              target="_blank"
              rel="noopener noreferrer"
            >Открыть бота</a>
          </div>
          <button
            v-else
            type="button"
            class="button primary"
            @click="bind"
          >
            Привязать Telegram
          </button>
        </template>
      </template>
    </section>
  </main>
</template>

<style scoped>
.settings {
  max-width: var(--measure);
}

section {
  display: grid;
  gap: var(--space-3);
  justify-items: start;
  padding-block: var(--space-5);
  border-bottom: 1px solid var(--rule);
}

h2 {
  font-size: var(--step-1);
}

dl {
  margin: 0;
}

dt {
  color: var(--muted);
  font-size: var(--step--1);
}

dd {
  margin: 0;
}

p {
  margin: 0;
}

.muted {
  color: var(--muted);
}

.done {
  color: var(--ink-translation);
}

.toggles {
  border: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: var(--space-2);
}

.toggles label {
  display: flex;
  gap: var(--space-2);
  align-items: center;
  cursor: pointer;
}

.link {
  display: grid;
  gap: var(--space-3);
  justify-items: start;
}
</style>
