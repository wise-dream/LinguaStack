<script setup lang="ts">
// Администрирование (ТЗ §2.1, §8.1 №12; дополнение §18): обзор системы и
// пользователи. Только метаданные и счётчики — ни названий книг, ни
// текстов: к содержимому администратор доступа не имеет.
import { NButton, NEmpty, NInput, NInputGroup, NStatistic, NTable, NTag } from 'naive-ui'
import { computed, onMounted, reactive, ref } from 'vue'

import { getOverview, issueResetLink, listUsers, patchUser } from '../api/admin'
import { ApiError, errorMessage } from '../api/client'
import type { AdminOverview, AdminUser, AdminUserPatch, QuotaDefaults, ResetLink } from '../api/types'
import ConfirmButton from '../components/ConfirmButton.vue'
import ErrorAlert from '../components/ErrorAlert.vue'
import TextField from '../components/TextField.vue'
import { formatBytes, formatCost, formatDate, formatNumber, plural } from '../lib/format'
import { useSession } from '../session'

const GB = 1024 ** 3

const session = useSession()
const overview = ref<AdminOverview | null>(null)
const users = ref<AdminUser[] | null>(null)
const defaults = ref<QuotaDefaults | null>(null)
const notFound = ref(false)
const error = ref('')
const query = ref('')
const busy = ref<string | null>(null)
const quotaOpen = ref<string | null>(null)
const quota = reactive({ max_active_projects: '', max_concurrent_tasks: '', storage_quota_gb: '' })
const resetLinks = ref<Record<string, ResetLink>>({})

onMounted(async () => {
  try {
    await session.load()
    const [o, list] = await Promise.all([getOverview(), listUsers()])
    overview.value = o
    users.value = list.users
    defaults.value = list.defaults
  } catch (e) {
    // Не администратору сервер отвечает 404 — для него раздела нет.
    if (e instanceof ApiError && e.status === 404) notFound.value = true
    else error.value = errorMessage(e)
  }
})

const count = (m: Record<string, number> | undefined, ...keys: string[]) =>
  keys.reduce((sum, k) => sum + (m?.[k] ?? 0), 0)

const figures = computed(() => {
  const m = overview.value?.metrics
  if (!m) return []
  return [
    {
      title: 'Переводы',
      items: [
        ['Идут', count(m.projects, 'running')],
        ['В очереди', count(m.projects, 'queued')],
        ['Приостановлены', count(m.projects, 'paused')],
        ['С ошибкой', count(m.projects, 'failed')],
        ['Готово', count(m.projects, 'completed', 'completed_with_warnings')],
      ],
    },
    {
      title: 'Задачи',
      items: [
        ['Ждут worker', count(m.tasks, 'pending')],
        ['Выполняются', count(m.tasks, 'running')],
        ['Исчерпали попытки', count(m.tasks, 'failed')],
      ],
    },
    {
      title: 'Экспорт и уведомления',
      items: [
        ['Собираются', count(m.exports, 'pending', 'running')],
        ['Сборка не удалась', count(m.exports, 'failed')],
        ['Ждут отправки в Telegram', count(m.notifications, 'pending')],
        ['Не доставлены', count(m.notifications, 'failed')],
      ],
    },
  ] as { title: string; items: [string, number][] }[]
})

const isSelf = (u: AdminUser) => u.id === session.me.value?.id

function storage(u: AdminUser) {
  const limit = u.storage_quota_bytes ?? defaults.value?.storage_quota_bytes ?? 0
  return limit > 0 ? `${formatBytes(u.storage_bytes)} из ${formatBytes(limit)}` : `${formatBytes(u.storage_bytes)}, без предела`
}

async function search() {
  error.value = ''
  try {
    const list = await listUsers(query.value.trim())
    users.value = list.users
    defaults.value = list.defaults
  } catch (e) {
    error.value = errorMessage(e)
  }
}

async function update(u: AdminUser, patch: AdminUserPatch) {
  error.value = ''
  busy.value = u.id
  try {
    const fresh = await patchUser(u.id, patch)
    users.value = (users.value ?? []).map((x) => (x.id === fresh.id ? fresh : x))
    return true
  } catch (e) {
    error.value = errorMessage(e)
    return false
  } finally {
    busy.value = null
  }
}

function openQuota(u: AdminUser) {
  if (quotaOpen.value === u.id) {
    quotaOpen.value = null
    return
  }
  quotaOpen.value = u.id
  quota.max_active_projects = u.max_active_projects?.toString() ?? ''
  quota.max_concurrent_tasks = u.max_concurrent_tasks?.toString() ?? ''
  quota.storage_quota_gb = u.storage_quota_bytes === null ? '' : String(u.storage_quota_bytes / GB).replace('.', ',')
}

// Пустое поле — значение по умолчанию (null на сервере).
const whole = (s: string) => (s.trim() === '' ? null : Number(s.trim()))
const gigabytes = (s: string) => (s.trim() === '' ? null : Math.round(Number(s.trim().replace(',', '.')) * GB))

async function saveQuota(u: AdminUser) {
  const patch = {
    max_active_projects: whole(quota.max_active_projects),
    max_concurrent_tasks: whole(quota.max_concurrent_tasks),
    storage_quota_bytes: gigabytes(quota.storage_quota_gb),
  }
  if (Object.values(patch).some((v) => v !== null && (!Number.isFinite(v) || v < 0))) {
    error.value = 'Квота — неотрицательное число или пустое поле'
    return
  }
  if (await update(u, patch)) quotaOpen.value = null
}

async function resetLink(u: AdminUser) {
  error.value = ''
  try {
    const link = await issueResetLink(u.id)
    // Без LINGUASTACK_PUBLIC_URL сервер даёт относительную ссылку.
    resetLinks.value = { ...resetLinks.value, [u.id]: { ...link, url: new URL(link.url, window.location.origin).href } }
  } catch (e) {
    error.value = errorMessage(e)
  }
}

// Копирование ссылки — по нажатию: буфер обмена доступен только по жесту
// пользователя и только по HTTPS.
const copied = ref<string | null>(null)
async function copyLink(id: string) {
  const link = resetLinks.value[id]
  if (!link) return
  try {
    await navigator.clipboard.writeText(link.url)
    copied.value = id
  } catch {
    error.value = 'Не удалось скопировать — выделите ссылку и скопируйте вручную.'
  }
}

const until = (iso: string) => new Date(iso).toLocaleTimeString('ru', { hour: '2-digit', minute: '2-digit' })
</script>

<template>
  <main
    v-if="notFound"
    class="page"
  >
    <h1>Страница не найдена</h1>
  </main>

  <main
    v-else
    class="page admin"
  >
    <h1>Администрирование</h1>
    <ErrorAlert :message="error" />

    <section
      v-if="overview"
      class="overview card"
      aria-labelledby="overview-title"
    >
      <h2 id="overview-title">
        Обзор
      </h2>
      <div class="headline">
        <NStatistic label="Пользователей">
          {{ formatNumber(overview.metrics.users) }}
        </NStatistic>
        <NStatistic label="Книг">
          {{ formatNumber(overview.metrics.books) }}
        </NStatistic>
        <NStatistic label="Расход внешних моделей">
          {{ formatCost(overview.metrics.cost_usd) }}
        </NStatistic>
        <NStatistic
          class="tokens"
          label="Токенов: запрос / ответ"
        >
          {{ formatNumber(overview.metrics.input_tokens) }} / {{ formatNumber(overview.metrics.output_tokens) }}
        </NStatistic>
      </div>
      <div class="groups">
        <dl
          v-for="g in figures"
          :key="g.title"
          class="group"
        >
          <dt class="group-title">
            {{ g.title }}
          </dt>
          <div
            v-for="[label, n] in g.items"
            :key="label"
            class="figure"
            :class="{ zero: n === 0 }"
          >
            <dt>{{ label }}</dt>
            <dd>{{ formatNumber(n) }}</dd>
          </div>
        </dl>
      </div>
      <div
        v-for="p in overview.providers"
        :key="p.id"
        class="provider"
        :class="{ down: !p.available }"
      >
        <p>
          <strong>Ollama сервера</strong> <code>{{ p.base_url }}</code> —
          {{ p.available ? 'доступен' : 'недоступен' }}<template v-if="!p.available && p.detail">
            : {{ p.detail }}
          </template>
        </p>
        <p
          v-if="p.available"
          class="models"
        >
          {{ p.models.length ? `Модели: ${p.models.join(', ')}` : 'Моделей нет — загрузите: ollama pull qwen3:8b' }}
        </p>
      </div>
    </section>

    <section
      v-if="users"
      class="users card"
      aria-labelledby="users-title"
    >
      <h2 id="users-title">
        Пользователи
      </h2>
      <form
        class="search"
        role="search"
        @submit.prevent="search"
      >
        <TextField
          v-model="query"
          label="Адрес содержит"
          type="search"
          placeholder="Поиск по адресу"
          hide-label
        />
        <NButton attr-type="submit">
          Найти
        </NButton>
      </form>
      <NEmpty
        v-if="!users.length"
        description="Никого не нашлось."
      />
      <NTable
        v-else
        class="users-table"
        :bordered="false"
        :single-line="true"
      >
        <thead>
          <tr>
            <th scope="col">
              Аккаунт
            </th>
            <th scope="col">
              Данные
            </th>
            <th scope="col">
              Хранилище
            </th>
            <th scope="col">
              Последний визит
            </th>
            <th scope="col">
              <span class="visually-hidden">Действия</span>
            </th>
          </tr>
        </thead>
        <tbody
          v-for="u in users"
          :key="u.id"
          :data-user="u.id"
          :class="{ inactive: !u.is_active }"
        >
          <tr>
            <td class="account">
              <span class="email">{{ u.email }}</span>
              <span class="tags">
                <NTag
                  v-if="u.role === 'admin'"
                  size="small"
                  round
                  :bordered="false"
                  type="info"
                >администратор</NTag>
                <NTag
                  v-if="isSelf(u)"
                  size="small"
                  round
                  :bordered="false"
                >это вы</NTag>
                <NTag
                  v-if="!u.is_active"
                  class="off"
                  size="small"
                  round
                  :bordered="false"
                  type="error"
                >отключён</NTag>
              </span>
            </td>
            <td data-label="Данные">
              {{ u.books }} {{ plural(u.books, ['книга', 'книги', 'книг']) }},
              {{ u.translations }} {{ plural(u.translations, ['перевод', 'перевода', 'переводов']) }}
            </td>
            <td data-label="Хранилище">
              {{ storage(u) }}
            </td>
            <td data-label="Последний визит">
              {{ u.last_seen_at ? formatDate(u.last_seen_at) : 'не входил' }}
            </td>
            <td class="actions">
              <template v-if="!isSelf(u)">
                <NButton
                  class="role"
                  size="small"
                  :disabled="busy === u.id"
                  @click="update(u, { role: u.role === 'admin' ? 'user' : 'admin' })"
                >
                  {{ u.role === 'admin' ? 'Снять администратора' : 'Сделать администратором' }}
                </NButton>
                <ConfirmButton
                  v-if="u.is_active"
                  label="Отключить"
                  confirm-label="Точно отключить?"
                  size="small"
                  :disabled="busy === u.id"
                  @confirm="update(u, { is_active: false })"
                />
                <NButton
                  v-else
                  size="small"
                  type="primary"
                  secondary
                  :disabled="busy === u.id"
                  @click="update(u, { is_active: true })"
                >
                  Включить
                </NButton>
              </template>
              <NButton
                class="quotas"
                size="small"
                :aria-expanded="quotaOpen === u.id"
                @click="openQuota(u)"
              >
                Квоты
              </NButton>
              <NButton
                class="reset"
                size="small"
                @click="resetLink(u)"
              >
                Ссылка для сброса пароля
              </NButton>
            </td>
          </tr>
          <tr
            v-if="quotaOpen === u.id && defaults"
            class="detail"
          >
            <td colspan="5">
              <form
                class="quota-form"
                @submit.prevent="saveQuota(u)"
              >
                <TextField
                  v-model="quota.max_active_projects"
                  label="Активных переводов"
                  name="max_active_projects"
                  inputmode="numeric"
                  :placeholder="String(defaults.max_active_projects)"
                />
                <TextField
                  v-model="quota.max_concurrent_tasks"
                  label="Одновременных задач"
                  name="max_concurrent_tasks"
                  inputmode="numeric"
                  :placeholder="String(defaults.max_concurrent_tasks)"
                />
                <TextField
                  v-model="quota.storage_quota_gb"
                  label="Хранилище, ГБ"
                  name="storage_quota_gb"
                  inputmode="decimal"
                  :placeholder="String(defaults.storage_quota_bytes / GB).replace('.', ',')"
                  hint="0 — без предела"
                />
                <p class="hint">
                  Пустое поле — значение сервера по умолчанию (подсказка в поле).
                </p>
                <NButton
                  attr-type="submit"
                  type="primary"
                  :disabled="busy === u.id"
                >
                  Сохранить квоты
                </NButton>
              </form>
            </td>
          </tr>
          <tr
            v-if="resetLinks[u.id]"
            class="detail"
          >
            <td colspan="5">
              <div class="reset-box">
                <label class="field">
                  <span>Ссылка для сброса пароля</span>
                  <NInputGroup>
                    <NInput
                      class="reset-link"
                      readonly
                      :value="resetLinks[u.id]!.url"
                      @focus="(e: FocusEvent) => (e.target as HTMLInputElement).select()"
                    />
                    <NButton @click="copyLink(u.id)">
                      {{ copied === u.id ? 'Скопировано' : 'Скопировать' }}
                    </NButton>
                  </NInputGroup>
                  <small>
                    Действует до {{ until(resetLinks[u.id]!.expires_at) }} и сработает один раз. Передайте её
                    владельцу аккаунта сами — прежняя ссылка больше не работает.
                  </small>
                </label>
              </div>
            </td>
          </tr>
        </tbody>
      </NTable>
    </section>
  </main>
</template>

<style scoped>
section {
  display: grid;
  gap: var(--space-4);
}

section + section {
  margin-top: var(--space-5);
}

h2 {
  font-size: var(--step-1);
}

p {
  margin: 0;
}

.muted {
  color: var(--muted);
}

/* Обзор: крупные числа набраны книжным шрифтом, как цифры в выходных
   данных издания; нули приглушены — глаз ищет то, что требует внимания. */
.headline {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(10rem, 1fr));
  gap: var(--space-4);
}

.headline :deep(.n-statistic__label) {
  color: var(--muted);
  font-size: var(--step--1);
}

.headline :deep(.n-statistic-value) {
  font-family: var(--font-book);
  font-size: var(--step-3);
  line-height: 1.1;
  font-variant-numeric: tabular-nums;
}

.headline .tokens :deep(.n-statistic-value) {
  /* Меньший кегль прижат к низу строки, чтобы числа стояли на одной линии
     с соседними. */
  display: flex;
  align-items: flex-end;
  min-height: calc(var(--step-3) * 1.1);
  font-size: var(--step-1);
}

.group-title,
.figure dt {
  color: var(--muted);
  font-size: var(--step--1);
}

.groups {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(14rem, 1fr));
  gap: var(--space-5);
}

.group {
  margin: 0;
  display: grid;
  gap: var(--space-1);
  align-content: start;
}

.group-title {
  font-weight: 500;
  padding-bottom: var(--space-1);
  border-bottom: 1px solid var(--rule-soft);
}

.figure {
  display: flex;
  justify-content: space-between;
  gap: var(--space-3);
}

.figure dt {
  font-size: var(--step-0);
  color: var(--ink-original);
}

.figure dd {
  margin: 0;
  font-variant-numeric: tabular-nums;
  font-weight: 500;
}

.figure.zero dt,
.figure.zero dd {
  color: var(--muted);
  font-weight: 400;
}

.provider {
  display: grid;
  gap: var(--space-1);
  padding-left: var(--space-3);
  border-left: 3px solid var(--ink-translation);
}

.provider.down {
  border-left-color: var(--proof);
}

.provider code {
  font-size: var(--step--1);
}

.models {
  color: var(--muted);
  overflow-wrap: anywhere;
}

/* Пользователи. */
.search {
  display: flex;
  gap: var(--space-2);
  max-width: 28rem;
}

.search .field {
  flex: 1;
}

table {
  width: 100%;
  border-collapse: collapse;
}

th {
  text-align: left;
  font-weight: 500;
  color: var(--muted);
  font-size: var(--step--1);
  padding: var(--space-2);
  border-bottom: 1px solid var(--rule-soft);
}

td {
  padding: var(--space-3) var(--space-2);
  vertical-align: top;
}

/* Счётчики и даты не рвутся посреди строки; переносится только адрес. */
td[data-label] {
  white-space: nowrap;
}

tbody {
  border-bottom: 1px solid var(--rule-soft);
}

/* Линия — под строкой пользователя целиком (tbody), не под каждой ячейкой
   NTable: квоты и ссылка сброса раскрываются внутри той же строки. */
.users-table td {
  border-bottom: none;
}

.account {
  display: grid;
  gap: var(--space-1);
}

.email {
  font-weight: 500;
  overflow-wrap: anywhere;
}

.tags {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-1);
}

.tags:empty {
  display: none;
}

.inactive .email {
  color: var(--muted);
  text-decoration: line-through;
  text-decoration-color: var(--proof);
}

.actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
  justify-content: flex-end;
}

.detail td {
  padding-top: 0;
}

.quota-form {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(11rem, 1fr));
  gap: var(--space-3);
  align-items: start;
  padding: var(--space-3);
  background: var(--paper-raised);
  border: 1px solid var(--rule-soft);
  border-radius: var(--radius);
}

.quota-form .hint {
  grid-column: 1 / -1;
  color: var(--muted);
  font-size: var(--step--1);
}

.quota-form .button {
  justify-self: start;
}

.reset-box {
  padding: var(--space-3);
  background: var(--paper-raised);
  border: 1px solid var(--rule-soft);
  border-radius: var(--radius);
}

/* Телефон: строка таблицы — карточка, подписи берутся из data-label. */
@media (max-width: 40rem) {
  thead {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
  }

  table,
  tbody,
  tr,
  td {
    display: block;
  }

  td {
    padding: var(--space-1) 0;
  }

  td[data-label] {
    white-space: normal;
  }

  td[data-label]::before {
    content: attr(data-label) ': ';
    color: var(--muted);
  }

  tbody tr:first-child {
    padding-block: var(--space-3);
  }

  .actions {
    justify-content: flex-start;
    padding-top: var(--space-2);
  }
}
</style>
