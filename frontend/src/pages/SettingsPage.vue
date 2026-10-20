<script setup lang="ts">
// Настройки аккаунта и Telegram (ТЗ §2.1, §8.1 №11, §9): смена пароля и
// почты, привязка Telegram по одноразовой ссылке на бота и выбор
// уведомлений.
import { onBeforeUnmount, onMounted, reactive, ref } from 'vue'

import { changeEmail, changePassword } from '../api/auth'
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

// Формы профиля: у каждой своя ошибка и своё «готово».
const password = reactive({ current: '', next: '', error: '', done: '', busy: false })
const email = reactive({ value: '', current: '', error: '', done: '', busy: false })

async function submitPassword() {
  Object.assign(password, { error: '', done: '', busy: true })
  try {
    await changePassword(password.current, password.next)
    Object.assign(password, { current: '', next: '', done: 'Пароль изменён. На других устройствах нужно войти заново.' })
  } catch (e) {
    password.error = errorMessage(e)
  } finally {
    password.busy = false
  }
}

async function submitEmail() {
  Object.assign(email, { error: '', done: '', busy: true })
  try {
    const user = await changeEmail(email.value.trim(), email.current)
    session.me.value = user
    Object.assign(email, { value: '', current: '', done: `Почта изменена: теперь вход по адресу ${user.email}.` })
  } catch (e) {
    email.error = errorMessage(e)
  } finally {
    email.busy = false
  }
}

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
// Ссылка устарела — ждать нечего: опрос останавливается.
function poll() {
  clearTimeout(timer)
  if (stopped) return
  timer = setTimeout(async () => {
    if (link.value && Date.now() > Date.parse(link.value.expires_at)) {
      link.value = null
      done.value = 'Ссылка устарела. Нажмите «Привязать Telegram», чтобы получить новую.'
      return
    }
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
      class="password"
      aria-labelledby="password-title"
    >
      <h2 id="password-title">
        Пароль
      </h2>
      <form
        class="password-form"
        novalidate
        @submit.prevent="submitPassword"
      >
        <label class="field">
          <span>Текущий пароль</span>
          <input
            v-model="password.current"
            name="current_password"
            type="password"
            autocomplete="current-password"
            required
          >
        </label>
        <label class="field">
          <span>Новый пароль</span>
          <input
            v-model="password.next"
            name="new_password"
            type="password"
            autocomplete="new-password"
            required
          >
          <small>Не короче 12 символов. Остальные устройства выйдут из аккаунта.</small>
        </label>
        <p
          v-if="password.error"
          role="alert"
          class="form-error"
        >
          {{ password.error }}
        </p>
        <p
          v-if="password.done"
          role="status"
          class="done"
        >
          {{ password.done }}
        </p>
        <button
          type="submit"
          class="button"
          :disabled="password.busy"
        >
          Сменить пароль
        </button>
      </form>
    </section>

    <section
      class="email"
      aria-labelledby="email-title"
    >
      <h2 id="email-title">
        Электронная почта
      </h2>
      <form
        class="email-form"
        novalidate
        @submit.prevent="submitEmail"
      >
        <label class="field">
          <span>Новый адрес</span>
          <input
            v-model="email.value"
            name="email"
            type="email"
            autocomplete="email"
            required
          >
        </label>
        <label class="field">
          <span>Текущий пароль</span>
          <input
            v-model="email.current"
            name="email_password"
            type="password"
            autocomplete="current-password"
            required
          >
        </label>
        <p
          v-if="email.error"
          role="alert"
          class="form-error"
        >
          {{ email.error }}
        </p>
        <p
          v-if="email.done"
          role="status"
          class="done"
        >
          {{ email.done }}
        </p>
        <button
          type="submit"
          class="button"
          :disabled="email.busy"
        >
          Сменить почту
        </button>
      </form>
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

form {
  display: grid;
  gap: var(--space-3);
  justify-items: start;
  width: min(100%, 26rem);
}

form .field {
  width: 100%;
}

.link {
  display: grid;
  gap: var(--space-3);
  justify-items: start;
}
</style>
