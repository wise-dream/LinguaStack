<script setup lang="ts">
// Подключения к моделям (ТЗ §8.1 п.8, §6.4): локальная Ollama и внешние
// сервисы. Ключ API уходит на сервер один раз и больше не показывается.
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
      class="configs"
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
          <button
            type="button"
            class="button check"
            @click="check(c)"
          >
            Проверить
          </button>
          <ConfirmButton
            label="Удалить"
            @confirm="remove(c)"
          />
          <span
            v-if="health[c.id]"
            role="status"
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
      class="muted"
    >
      Подключений пока нет — добавьте первое ниже.
    </p>

    <form
      class="new-config"
      @submit.prevent="create"
    >
      <h2>Новое подключение</h2>
      <label class="field">
        <span>Провайдер</span>
        <select
          v-model="form.provider_id"
          name="provider_id"
        >
          <option
            v-for="k in kinds"
            :key="k.provider_id"
            :value="k.provider_id"
          >{{ k.name }}</option>
        </select>
      </label>
      <label class="field">
        <span>Название</span>
        <input
          v-model="form.name"
          name="name"
          :placeholder="kind?.name"
        >
      </label>
      <label
        v-if="kind?.user_base_url"
        class="field"
      >
        <span>Адрес API</span>
        <input
          v-model="form.base_url"
          name="base_url"
          type="url"
          placeholder="https://api.example.com/v1"
          required
        >
        <small>Адрес OpenAI-совместимого сервиса, например vLLM или LM Studio.</small>
      </label>
      <label
        v-if="kind && kind.provider_id !== 'ollama' && kind.provider_id !== 'fake'"
        class="field"
      >
        <span>Ключ API{{ kind.requires_api_key ? '' : ' (если нужен)' }}</span>
        <input
          v-model="form.api_key"
          name="api_key"
          type="password"
          autocomplete="off"
          spellcheck="false"
        >
      </label>
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
      <button
        type="submit"
        class="button primary"
        :disabled="saving"
      >
        Добавить подключение
      </button>
    </form>
  </main>
</template>

<style scoped>
.providers {
  max-width: 48rem;
}

h1 {
  font-family: var(--font-book);
  font-size: var(--step-3);
  font-weight: 500;
}

.intro,
.muted {
  color: var(--muted);
  max-width: var(--measure);
}

.configs {
  margin: var(--space-5) 0;
  padding: 0;
  list-style: none;
  border-top: 1px solid var(--ink-original);
}

.configs > li {
  display: grid;
  gap: var(--space-2);
  padding-block: var(--space-4);
  border-bottom: 1px solid var(--rule);
}

.config-head {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: var(--space-1) var(--space-4);
}

.config-name {
  font-weight: 600;
  font-size: var(--step-1);
}

.config-kind,
.key {
  color: var(--muted);
}

.row-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2) var(--space-3);
}

.inline-check {
  display: flex;
  gap: var(--space-2);
  align-items: center;
}

.new-config {
  display: grid;
  gap: var(--space-4);
  margin-top: var(--space-6);
  padding-top: var(--space-5);
  border-top: 1px solid var(--rule);
}

.new-config h2 {
  font-size: var(--step-1);
}

.new-config .button {
  justify-self: start;
}

.consent {
  display: flex;
  gap: var(--space-3);
  align-items: flex-start;
  padding: var(--space-3);
  border-left: 3px solid var(--highlight);
  background: var(--paper-raised);
}

.consent input {
  margin-top: 0.3em;
}
</style>
