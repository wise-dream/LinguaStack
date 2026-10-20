<script setup lang="ts">
// Настройка перевода (ТЗ §8.1 п.5): подключение и модель, языки по
// возможностям модели (§5.2), стиль, точность, глоссарий и
// предупреждение о передаче текста наружу (§6.4).
import { NAlert, NButton, NRadio, NRadioGroup } from 'naive-ui'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { getBook } from '../api/books'
import { errorMessage } from '../api/client'
import { configModels, listProviderConfigs } from '../api/providers'
import { createTranslation, getTranslationOptions } from '../api/translations'
import type { Book, Determinism, ModelCaps, ProviderConfig, TranslationOptions } from '../api/types'
import ErrorAlert from '../components/ErrorAlert.vue'
import GlossaryEditor from '../components/GlossaryEditor.vue'
import LinkButton from '../components/LinkButton.vue'
import SelectField from '../components/SelectField.vue'
import { glossaryFromRows, type GlossaryRow } from '../components/glossary'
import { capitalize, languageName } from '../lib/format'

const route = useRoute()
const router = useRouter()
const bookId = String(route.params.id)

const book = ref<Book | null>(null)
const options = ref<TranslationOptions | null>(null)
const configs = ref<ProviderConfig[]>([])
const models = ref<ModelCaps[]>([])
const loadError = ref('')
const modelsError = ref('')
const submitError = ref('')
const busy = ref(false)

const form = reactive({
  provider_config_id: '',
  model_id: '',
  source_lang: 'auto',
  target_lang: 'ru',
  style: 'neutral',
  determinism: 'strict' as Determinism,
})
const glossary = ref<GlossaryRow[]>([])

const styleText: Record<string, { label: string; hint: string }> = {
  literary: { label: 'Художественный', hint: 'Сохраняет голос автора, образы и ритм.' },
  neutral: { label: 'Нейтральный', hint: 'Естественный и точный текст без украшений.' },
  technical: { label: 'Технический', hint: 'Единая терминология, имена в коде не трогаются.' },
  simplified: { label: 'Упрощённый', hint: 'Простыми словами, сложное объясняется проще.' },
}

const determinismText: Record<Determinism, { label: string; hint: string }> = {
  strict: { label: 'Строго', hint: 'Повторный перевод даст тот же текст.' },
  balanced: { label: 'Сбалансированно', hint: 'Немного свободы в формулировках.' },
  creative: { label: 'Свободно', hint: 'Больше вариативности, текст живее.' },
}

// Нейтральный — по умолчанию и первым; дальше от близкого к тексту к свободному.
const styleOrder = ['neutral', 'literary', 'technical', 'simplified']
const styles = computed(() =>
  [...(options.value?.styles ?? [])].sort((a, b) => {
    const ia = styleOrder.indexOf(a)
    const ib = styleOrder.indexOf(b)
    return (ia < 0 ? 99 : ia) - (ib < 0 ? 99 : ib)
  }),
)

const config = computed(() => configs.value.find((c) => c.id === form.provider_config_id))
const model = computed(() => models.value.find((m) => m.model_id === form.model_id))

/** Языки, которые понимает модель; если она их не сообщает — все. */
const languages = computed(() => {
  const all = options.value?.languages.map((l) => l.code) ?? []
  const supported = model.value?.supported_languages
  const allowed = supported?.length ? all.filter((c) => supported.includes(c)) : all
  return [...allowed].sort((a, b) => languageName(a).localeCompare(languageName(b), 'ru'))
})

const langOption = (code: string) => ({ label: capitalize(languageName(code)), value: code })
const configOptions = computed(() => configs.value.map((c) => ({ label: c.name, value: c.id })))
const modelOptions = computed(() => models.value.map((m) => ({ label: m.display_name || m.model_id, value: m.model_id })))
const sourceOptions = computed(() => [langOption('auto'), ...languages.value.map(langOption)])
const targetOptions = computed(() => languages.value.map(langOption))

const egress = computed(() => {
  if (!config.value?.sends_data_externally) return null
  return config.value.allow_external ? 'allowed' : 'blocked'
})

const canSubmit = computed(() => !!form.model_id && egress.value !== 'blocked' && !busy.value)

function primary(lang?: string) {
  return (lang ?? '').toLowerCase().split('-')[0] ?? ''
}

// Выбор языков остаётся допустимым при смене модели.
watch(languages, (allowed) => {
  if (form.source_lang !== 'auto' && !allowed.includes(form.source_lang)) form.source_lang = 'auto'
  if (!allowed.includes(form.target_lang)) {
    form.target_lang = allowed.includes('ru') ? 'ru' : (allowed[0] ?? '')
  }
})

watch(
  () => form.provider_config_id,
  async (id) => {
    models.value = []
    form.model_id = ''
    modelsError.value = ''
    if (!id) return
    try {
      models.value = await configModels(id)
      form.model_id = models.value[0]?.model_id ?? ''
      if (!models.value.length) modelsError.value = 'У этого подключения нет доступных моделей.'
    } catch (e) {
      modelsError.value = errorMessage(e)
    }
  },
)

onMounted(async () => {
  try {
    const [b, o, c] = await Promise.all([getBook(bookId), getTranslationOptions(), listProviderConfigs()])
    book.value = b
    options.value = o
    configs.value = c
    const lang = primary(b.language)
    form.source_lang = o.languages.some((l) => l.code === lang) ? lang : 'auto'
    form.target_lang = form.source_lang === 'ru' ? 'en' : 'ru'
    form.provider_config_id = c[0]?.id ?? ''
  } catch (e) {
    loadError.value = errorMessage(e)
  }
})

async function submit() {
  if (!canSubmit.value) return
  submitError.value = ''
  busy.value = true
  try {
    const project = await createTranslation(bookId, { ...form, glossary: glossaryFromRows(glossary.value) })
    await router.push(`/translations/${project.id}`)
  } catch (e) {
    submitError.value = errorMessage(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="page setup">
    <ErrorAlert :message="loadError" />
    <template v-if="book && options">
      <p class="crumb">
        <RouterLink :to="`/books/${book.id}`">
          {{ book.title }}
        </RouterLink>
      </p>
      <h1>Новый перевод</h1>

      <section
        v-if="!configs.length"
        class="empty card"
      >
        <p>Чтобы переводить, подключите модель: локальную Ollama или внешний сервис.</p>
        <LinkButton
          to="/settings/providers"
          type="primary"
        >
          Подключить модель
        </LinkButton>
      </section>

      <form
        v-else
        class="card"
        @submit.prevent="submit"
      >
        <div class="col">
          <div class="pair">
            <SelectField
              v-model="form.provider_config_id"
              label="Подключение"
              name="provider_config_id"
              :options="configOptions"
            />
            <SelectField
              v-model="form.model_id"
              label="Модель"
              name="model_id"
              :options="modelOptions"
              :disabled="!models.length"
              filterable
            />
          </div>
          <ErrorAlert :message="modelsError" />
          <NAlert
            v-if="egress === 'allowed'"
            class="egress"
            type="warning"
          >
            Текст книги будет отправлен в «{{ config?.name }}» и покинет этот сервер.
          </NAlert>
          <NAlert
            v-else-if="egress === 'blocked'"
            class="egress blocked"
            type="error"
          >
            Подключение «{{ config?.name }}» не разрешает отправку текста книг наружу. Разрешите её в разделе
            <RouterLink to="/settings/providers">
              «Модели»
            </RouterLink> или выберите локальную модель.
          </NAlert>
          <div class="pair">
            <SelectField
              v-model="form.source_lang"
              label="Язык оригинала"
              name="source_lang"
              :options="sourceOptions"
              filterable
            />
            <SelectField
              v-model="form.target_lang"
              label="Язык перевода"
              name="target_lang"
              :options="targetOptions"
              filterable
            />
          </div>
          <GlossaryEditor v-model="glossary" />
        </div>
        <div class="col">
          <fieldset class="choice-set">
            <legend>Стиль</legend>
            <NRadioGroup
              v-model:value="form.style"
              name="style"
              class="choice styles"
            >
              <NRadio
                v-for="s in styles"
                :key="s"
                :value="s"
                class="tile"
              >
                <strong>{{ styleText[s]?.label ?? s }}</strong>
                <small>{{ styleText[s]?.hint }}</small>
              </NRadio>
            </NRadioGroup>
          </fieldset>
          <fieldset class="choice-set">
            <legend>Точность</legend>
            <NRadioGroup
              v-model:value="form.determinism"
              name="determinism"
              class="choice"
            >
              <NRadio
                v-for="d in options.determinism"
                :key="d"
                :value="d"
                class="tile"
              >
                <strong>{{ determinismText[d]?.label ?? d }}</strong>
                <small>{{ determinismText[d]?.hint }}</small>
              </NRadio>
            </NRadioGroup>
          </fieldset>
        </div>
        <div class="submit-row">
          <ErrorAlert :message="submitError" />
          <NButton
            attr-type="submit"
            type="primary"
            size="large"
            :disabled="!canSubmit"
          >
            Начать перевод
          </NButton>
        </div>
      </form>
    </template>
  </main>
</template>

<style scoped>
/* Форма в две колонки: слева — модель, языки и глоссарий, справа — как
   переводить; запуск — внизу во всю ширину. */
.setup form {
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  column-gap: var(--space-6);
  align-items: start;
}

.col {
  display: grid;
  gap: var(--space-5);
}

.submit-row {
  grid-column: 1 / -1;
  display: grid;
  gap: var(--space-3);
  justify-items: start;
  padding-top: var(--space-4);
  border-top: 1px solid var(--rule-soft);
}

.crumb {
  margin: 0 0 var(--space-2);
  color: var(--muted);
  font-family: var(--font-book);
}

form {
  display: grid;
  gap: var(--space-5);
}

.pair {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: var(--space-4);
}

/* Варианты стиля и точности — плитки: выбранная обведена краской оригинала. */
.choice-set {
  border: none;
  margin: 0;
  padding: 0;
}

.choice-set legend {
  font-weight: 500;
  padding: 0;
  margin-bottom: var(--space-2);
}

.choice {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(11rem, 1fr));
  gap: var(--space-2);
}

.tile {
  display: flex;
  align-items: flex-start;
  margin: 0;
  padding: var(--space-3);
  border: 1px solid var(--rule-soft);
  border-radius: 8px;
  background: var(--surface);
  transition: border-color 0.2s, background-color 0.2s;
}

.tile:hover {
  border-color: var(--rule);
}

.tile.n-radio--checked {
  border-color: var(--ink-original);
  background: color-mix(in srgb, var(--ink-original) 4%, var(--surface));
}

.tile :deep(.n-radio__label) {
  display: grid;
  gap: 2px;
}

.tile small {
  color: var(--muted);
}

.empty {
  display: grid;
  gap: var(--space-3);
  justify-items: start;
}

.empty p {
  margin: 0;
  color: var(--muted);
}

@media (max-width: 64rem) {
  .setup form {
    grid-template-columns: minmax(0, 1fr);
  }
}

@media (max-width: 40rem) {
  .pair,
  .choice {
    grid-template-columns: 1fr;
  }
}
</style>
