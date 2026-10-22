<script setup lang="ts">
// Настройки аккаунта и Telegram (ТЗ §2.1, §8.1 №11, §9): смена пароля и
// почты, привязка Telegram по одноразовой ссылке на бота и выбор
// уведомлений.
import { NButton, NSwitch } from 'naive-ui'
import { onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'

import { changeEmail, changePassword, deleteAccount } from '../api/auth'
import { errorMessage } from '../api/client'
import { getTelegram, linkTelegram, patchTelegram, unlinkTelegram } from '../api/telegram'
import type { TelegramLink, TelegramSettings, TelegramState } from '../api/types'
import ConfirmButton from '../components/ConfirmButton.vue'
import ErrorAlert from '../components/ErrorAlert.vue'
import TextField from '../components/TextField.vue'
import { formatDate } from '../lib/format'
import { useSession } from '../session'

const POLL_MS = 3000

const session = useSession()
const router = useRouter()
const telegram = ref<TelegramState | null>(null)
const link = ref<TelegramLink | null>(null)
const error = ref('')
const done = ref('')
let timer: ReturnType<typeof setTimeout> | undefined
let stopped = false

// Формы профиля: у каждой своя ошибка и своё «готово».
const password = reactive({ current: '', next: '', error: '', done: '', busy: false })
const email = reactive({ value: '', current: '', error: '', done: '', busy: false })
const removal = reactive({ password: '', error: '', busy: false })

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

async function removeAccount() {
  Object.assign(removal, { error: '', busy: true })
  try {
    await deleteAccount(removal.password)
    session.forget()
    await router.replace({ path: '/login', query: { deleted: '1' } })
  } catch (e) {
    removal.error = errorMessage(e)
  } finally {
    removal.busy = false
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

    <div class="layout">
      <div class="col">
        <section
          class="account card"
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
          class="telegram card"
          aria-labelledby="telegram-title"
        >
          <h2 id="telegram-title">
            Telegram
          </h2>
          <ErrorAlert :message="error" />
          <p
            v-if="done"
            role="status"
            class="notice"
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
                <div
                  v-for="n in notifications"
                  :key="n.key"
                  class="toggle"
                  :data-toggle="n.key"
                >
                  <NSwitch
                    :value="telegram[n.key]"
                    :aria-labelledby="`toggle-${n.key}`"
                    @update:value="(v: boolean) => toggle(n.key, v)"
                  />
                  <span :id="`toggle-${n.key}`">{{ n.label }}</span>
                </div>
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
                <NButton
                  tag="a"
                  class="open-bot"
                  type="primary"
                  :href="link.url"
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  Открыть бота
                </NButton>
              </div>
              <NButton
                v-else
                class="primary"
                type="primary"
                @click="bind"
              >
                Привязать Telegram
              </NButton>
            </template>
          </template>
        </section>

        <section
          class="delete-account card"
          aria-labelledby="delete-title"
        >
          <h2 id="delete-title">
            Удаление аккаунта
          </h2>
          <p class="muted">
            Удалятся книги, переводы, экспорты, подключения к моделям и привязка Telegram.
            Отменить нельзя.
          </p>
          <form
            class="delete-form"
            novalidate
            @submit.prevent
          >
            <TextField
              v-model="removal.password"
              label="Текущий пароль"
              name="delete_password"
              type="password"
              autocomplete="current-password"
              required
            />
            <ErrorAlert :message="removal.error" />
            <ConfirmButton
              class="delete"
              label="Удалить аккаунт"
              confirm-label="Точно удалить аккаунт?"
              :disabled="removal.busy || !removal.password"
              @confirm="removeAccount"
            />
          </form>
        </section>
      </div>
      <div class="col">
        <section
          class="password card"
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
            <TextField
              v-model="password.current"
              label="Текущий пароль"
              name="current_password"
              type="password"
              autocomplete="current-password"
              required
            />
            <TextField
              v-model="password.next"
              label="Новый пароль"
              name="new_password"
              type="password"
              autocomplete="new-password"
              hint="Не короче 12 символов. Остальные устройства выйдут из аккаунта."
              required
            />
            <ErrorAlert :message="password.error" />
            <p
              v-if="password.done"
              role="status"
              class="notice"
            >
              {{ password.done }}
            </p>
            <NButton
              attr-type="submit"
              :loading="password.busy"
              :disabled="password.busy"
            >
              Сменить пароль
            </NButton>
          </form>
        </section>

        <section
          class="email card"
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
            <TextField
              v-model="email.value"
              label="Новый адрес"
              name="email"
              type="email"
              autocomplete="email"
              required
            />
            <TextField
              v-model="email.current"
              label="Текущий пароль"
              name="email_password"
              type="password"
              autocomplete="current-password"
              required
            />
            <ErrorAlert :message="email.error" />
            <p
              v-if="email.done"
              role="status"
              class="notice"
            >
              {{ email.done }}
            </p>
            <NButton
              attr-type="submit"
              :loading="email.busy"
              :disabled="email.busy"
            >
              Сменить почту
            </NButton>
          </form>
        </section>
      </div>
    </div>
  </main>
</template>

<style scoped>
/* Две колонки: слева — аккаунт и Telegram, справа — пароль и почта. */
.layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: var(--space-5);
  align-items: start;
}

.col {
  display: grid;
  gap: var(--space-5);
}

@media (max-width: 56rem) {
  .layout {
    grid-template-columns: minmax(0, 1fr);
  }
}

section {
  display: grid;
  gap: var(--space-3);
  justify-items: start;
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

.toggles {
  border: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: var(--space-2);
}

.toggle {
  display: flex;
  gap: var(--space-3);
  align-items: center;
}

form {
  display: grid;
  gap: var(--space-4);
  justify-items: start;
  width: 100%;
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
