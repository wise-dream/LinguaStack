<script setup lang="ts">
// Подключения к моделям (ТЗ §8.1 п.8, §6.4): локальная Ollama и внешние
// сервисы. Ключ API уходит на сервер один раз и больше не показывается.
import { NButton } from 'naive-ui'
import { computed, onMounted, reactive, ref, watch } from 'vue'

import { errorMessage } from '../api/client'
import {
  configHealth,
  createProviderConfig,
  deleteProviderConfig,
  listKinds,
  listProviderConfigs,
  patchProviderConfig,
} from '../api/providers'
import type { ProviderConfig, ProviderKind } from '../api/types'
import ConfirmButton from '../components/ConfirmButton.vue'
import TextField from '../components/TextField.vue'

const kinds = ref<ProviderKind[]>([])
const configs = ref<ProviderConfig[]>([])
const loadError = ref('')

const form = reactive({ provider_id: 'ollama', name: '', base_url: '', api_key: '', allow_external: false })
const formError = ref('')
const saving = ref(false)

// Результат проверки и ошибки — у каждой строки свои.
const health = reactive<Record<string, string>>({})
const rowError = reactive<Record<string, string>>({})

const kind = computed(() => kinds.value.find((k) => k.provider_id === form.provider_id))
const kindName = (id: string) => kinds.value.find((k) => k.provider_id === id)?.name ?? id

watch(
  () => form.provider_id,
  () => {
    form.base_url = ''
    form.api_key = ''
    form.allow_external = false
    formError.value = ''
  },
)

onMounted(async () => {
  try {
    const [k, c] = await Promise.all([listKinds(), listProviderConfigs()])
    kinds.value = k
    configs.value = c
  } catch (e) {
    loadError.value = errorMessage(e)
  }
})

async function create() {
  formError.value = ''
  saving.value = true
  try {
    const created = await createProviderConfig({
      provider_id: form.provider_id,
      name: form.name.trim() || kind.value?.name || form.provider_id,
      base_url: kind.value?.user_base_url ? form.base_url.trim() : undefined,
      api_key: form.api_key.trim() || undefined,
      allow_external: kind.value?.sends_data_externally ? form.allow_external : false,
    })
    configs.value = [...configs.value, created]
    form.name = ''
    form.base_url = ''
    form.api_key = ''
    form.allow_external = false
  } catch (e) {
    formError.value = errorMessage(e)
  } finally {
    saving.value = false
  }
}

async function check(c: ProviderConfig) {
  delete rowError[c.id]
  health[c.id] = 'Проверяю…'
  try {
    const h = await configHealth(c.id)
    health[c.id] = h.available ? `Доступен${h.detail ? `: ${h.detail}` : ''}` : `Недоступен${h.detail ? `: ${h.detail}` : ''}`
  } catch (e) {
    health[c.id] = ''
    rowError[c.id] = errorMessage(e)
  }
}

async function setExternal(c: ProviderConfig, allow: boolean) {
  delete rowError[c.id]
  try {
    const updated = await patchProviderConfig(c.id, { allow_external: allow })
    configs.value = configs.value.map((x) => (x.id === c.id ? updated : x))
  } catch (e) {
    rowError[c.id] = errorMessage(e)
  }
}

async function remove(c: ProviderConfig) {
  delete rowError[c.id]
  try {
    await deleteProviderConfig(c.id)
    configs.value = configs.value.filter((x) => x.id !== c.id)
  } catch (e) {
    rowError[c.id] = errorMessage(e)
  }
}
</script>

<template>
  <main class="page providers">
    <h1>Модели</h1>
    <p class="intro">
      Подключите локальную модель Ollama или внешний сервис. Ключи API хранятся на сервере зашифрованными
      и после сохранения не показываются.
    </p>

    <p
      v-if="loadError"
      role="alert"
      class="form-error"
    >
      {{ loadError }}
    </p>

    <ul
      v-if="configs.length"
      class="configs card"
    >
      <li
        v-for="c in configs"
        :key="c.id"
        :data-config="c.id"
      >
        <div class="config-head">
          <span class="config-name">{{ c.name }}</span>
          <span class="config-kind">{{ kindName(c.provider_id) }}</span>
          <span
            v-if="c.has_api_key"
            class="key"
          >ключ …{{ c.key_last4 }}</span>
        </div>
        <label
          v-if="c.sends_data_externally"
          class="inline-check"
        >
          <input
            type="checkbox"
            :checked="c.allow_external"
            @change="setExternal(c, ($event.target as HTMLInputElement).checked)"
          >
          Разрешить отправку текста книг этому сервису
        </label>
        <div class="row-actions">
          <NButton
            class="check"
            size="small"
            @click="check(c)"
          >
            Проверить
          </NButton>
          <ConfirmButton
            label="Удалить"
            size="small"
            @confirm="remove(c)"
          />
          <span
            v-if="health[c.id]"
            role="status"
            class="health"
          >{{ health[c.id] }}</span>
        </div>
        <p
          v-if="rowError[c.id]"
          role="alert"
          class="form-error"
        >
          {{ rowError[c.id] }}
        </p>
      </li>
    </ul>
    <p
      v-else-if="!loadError"
      class="muted empty"
    >
      Подключений пока нет — добавьте первое ниже.
    </p>

    <form
      class="new-config card"
      @submit.prevent="create"
    >
      <h2>Новое подключение</h2>
      <div class="field">
        <label for="config-provider">Провайдер</label>
        <select
          id="config-provider"
          v-model="form.provider_id"
          name="provider_id"
        >
          <option
            v-for="k in kinds"
            :key="k.provider_id"
            :value="k.provider_id"
          >
            {{ k.name }}
          </option>
        </select>
      </div>
      <TextField
        v-model="form.name"
        label="Название"
        name="name"
        :placeholder="kind?.name ?? ''"
      />
      <TextField
        v-if="kind?.user_base_url"
        v-model="form.base_url"
        label="Адрес API"
        name="base_url"
        type="url"
        placeholder="https://api.example.com/v1"
        hint="Адрес OpenAI-совместимого сервиса, например vLLM или LM Studio."
        required
      />
      <TextField
        v-if="kind && kind.provider_id !== 'ollama' && kind.provider_id !== 'fake'"
        v-model="form.api_key"
        :label="`Ключ API${kind.requires_api_key ? '' : ' (если нужен)'}`"
        name="api_key"
        type="password"
        autocomplete="off"
        :spellcheck="false"
      />
      <label
        v-if="kind?.sends_data_externally"
        class="consent"
      >
        <input
          v-model="form.allow_external"
          name="allow_external"
          type="checkbox"
        >
        <span>
          <strong>Разрешить отправку текста книг этому сервису.</strong>
          Текст переводимых книг покинет этот сервер и уйдёт к «{{ kind.name }}». Без разрешения перевод через
          это подключение не запустится.
        </span>
      </label>
      <p
        v-if="formError"
        role="alert"
        class="form-error"
      >
        {{ formError }}
      </p>
      <NButton
        attr-type="submit"
        type="primary"
        :loading="saving"
        :disabled="saving"
      >
        Добавить подключение
      </NButton>
    </form>
  </main>
</template>

<style scoped>
.providers {
  max-width: 52rem;
}

.providers > h1 {
  margin-bottom: var(--space-2);
}

.intro,
.muted {
  color: var(--muted);
  max-width: var(--measure);
}

.intro {
  margin: 0 0 var(--space-5);
}

.empty {
  margin: 0 0 var(--space-5);
}

.configs {
  margin: 0 0 var(--space-5);
  padding-block: var(--space-2);
  list-style: none;
}

.configs > li {
  display: grid;
  gap: var(--space-2);
  padding-block: var(--space-4);
  border-bottom: 1px solid var(--rule-soft);
}

.configs > li:last-child {
  border-bottom: none;
}

.config-head {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: var(--space-1) var(--space-3);
}

.config-name {
  font-weight: 600;
  font-size: var(--step-1);
}

.config-kind,
.key {
  color: var(--muted);
  font-size: var(--step--1);
}

.row-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2);
}

.health {
  color: var(--muted);
  font-size: var(--step--1);
}

.inline-check {
  display: flex;
  gap: var(--space-2);
  align-items: center;
}

.new-config {
  display: grid;
  gap: var(--space-4);
}

.new-config h2 {
  font-size: var(--step-1);
}

.new-config > :last-child {
  justify-self: start;
}

.consent {
  display: flex;
  gap: var(--space-3);
  align-items: flex-start;
  padding: var(--space-3);
  border-radius: 8px;
  background: color-mix(in srgb, var(--highlight) 18%, var(--surface));
  border: 1px solid color-mix(in srgb, var(--highlight) 55%, var(--surface));
}

.consent input {
  margin-top: 0.25em;
  flex: none;
}
</style>
