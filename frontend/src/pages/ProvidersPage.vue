<script setup lang="ts">
// Подключения к моделям (ТЗ §8.1 п.8, §6.4): локальная Ollama и внешние
// сервисы. Ключ API уходит на сервер один раз и больше не показывается.
import { NButton, NCheckbox, NEmpty, NList, NListItem, NSwitch, NTag } from 'naive-ui'
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
import ErrorAlert from '../components/ErrorAlert.vue'
import SelectField from '../components/SelectField.vue'
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
const kindOptions = computed(() => kinds.value.map((k) => ({ label: k.name, value: k.provider_id })))
// Ollama сервера видна не всем (на сервере она может быть только для
// администратора) — введение не обещает того, чего нет в списке.
const serverOllama = computed(() => kinds.value.some((k) => k.provider_id === 'ollama'))
const healthType = (text: string) =>
  text.startsWith('Доступен') ? 'success' : text.startsWith('Недоступен') ? 'error' : 'default'

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
      <template v-if="serverOllama">
        Подключите локальную модель Ollama или внешний сервис.
      </template>
      <template v-else>
        Подключите внешний сервис моделей — Anthropic или OpenAI-совместимый — со своим ключом API.
      </template>
      Ключи API хранятся на сервере зашифрованными и после сохранения не показываются.
    </p>

    <ErrorAlert :message="loadError" />

    <div class="layout">
      <div class="list-col">
        <NList
          v-if="configs.length"
          class="configs card"
        >
          <NListItem
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
            <div
              v-if="c.sends_data_externally"
              class="inline-check"
            >
              <NSwitch
                :value="c.allow_external"
                :aria-label="`Разрешить отправку текста книг в «${c.name}»`"
                @update:value="(v: boolean) => setExternal(c, v)"
              />
              <span>Разрешить отправку текста книг этому сервису</span>
            </div>
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
              <NTag
                v-if="health[c.id]"
                role="status"
                class="health"
                size="small"
                round
                :bordered="false"
                :type="healthType(health[c.id]!)"
              >
                {{ health[c.id] }}
              </NTag>
            </div>
            <ErrorAlert
              :message="rowError[c.id]"
              size="small"
            />
          </NListItem>
        </NList>
        <section
          v-else-if="!loadError"
          class="empty card"
        >
          <NEmpty description="Подключений пока нет. Добавьте первое в форме «Новое подключение»." />
        </section>
      </div>
      <form
        class="new-config card"
        @submit.prevent="create"
      >
        <h2>Новое подключение</h2>
        <SelectField
          v-model="form.provider_id"
          label="Провайдер"
          name="provider_id"
          :options="kindOptions"
        />
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
        <div
          v-if="kind?.sends_data_externally"
          class="consent"
        >
          <NCheckbox v-model:checked="form.allow_external">
            <strong>Разрешить отправку текста книг этому сервису.</strong>
            Текст переводимых книг покинет этот сервер и уйдёт к «{{ kind.name }}». Без разрешения перевод через
            это подключение не запустится.
          </NCheckbox>
        </div>
        <ErrorAlert :message="formError" />
        <NButton
          attr-type="submit"
          type="primary"
          :loading="saving"
          :disabled="saving"
        >
          Добавить подключение
        </NButton>
      </form>
    </div>
  </main>
</template>

<style scoped>
/* Слева — подключения, справа — форма нового: на широком экране обе видны
   сразу, форма не уезжает при прокрутке длинного списка. */
.layout {
  display: grid;
  grid-template-columns: minmax(0, 7fr) minmax(0, 5fr);
  gap: var(--space-5);
  align-items: start;
}

.new-config {
  position: sticky;
  top: var(--space-4);
}

@media (max-width: 56rem) {
  .layout {
    grid-template-columns: minmax(0, 1fr);
  }

  .new-config {
    position: static;
  }
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
  padding-block: var(--space-6);
}

.configs {
  margin: 0;
  padding-block: var(--space-2);
}

.configs :deep(.n-list-item__main) {
  display: grid;
  gap: var(--space-2);
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

.inline-check {
  display: flex;
  gap: var(--space-2);
  align-items: center;
}

.new-config {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: var(--space-4);
}

.new-config h2 {
  font-size: var(--step-1);
}

.new-config > :last-child {
  justify-self: start;
}

.consent {
  padding: var(--space-3);
  border-radius: 8px;
  background: color-mix(in srgb, var(--highlight) 18%, var(--surface));
  border: 1px solid color-mix(in srgb, var(--highlight) 55%, var(--surface));
}

.consent :deep(.n-checkbox) {
  align-items: flex-start;
}
</style>
