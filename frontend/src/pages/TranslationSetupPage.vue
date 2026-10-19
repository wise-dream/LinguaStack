<script setup lang="ts">
// Настройка перевода (ТЗ §8.1 п.5): подключение и модель, языки по
// возможностям модели (§5.2), стиль, точность, глоссарий и
// предупреждение о передаче текста наружу (§6.4).
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { getBook } from '../api/books'
import { errorMessage } from '../api/client'
import { configModels, listProviderConfigs } from '../api/providers'
import { createTranslation, getTranslationOptions } from '../api/translations'
import type { Book, Determinism, ModelCaps, ProviderConfig, TranslationOptions } from '../api/types'
import GlossaryEditor from '../components/GlossaryEditor.vue'
import { glossaryFromRows, type GlossaryRow } from '../components/glossary'
import { languageName } from '../lib/format'

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

const config = computed(() => configs.value.find((c) => c.id === form.provider_config_id))
const model = computed(() => models.value.find((m) => m.model_id === form.model_id))

/** Языки, которые понимает модель; если она их не сообщает — все. */
const languages = computed(() => {
  const all = options.value?.languages.map((l) => l.code) ?? []
  const supported = model.value?.supported_languages
  const allowed = supported?.length ? all.filter((c) => supported.includes(c)) : all
  return [...allowed].sort((a, b) => languageName(a).localeCompare(languageName(b), 'ru'))
})

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
    <p
      v-if="loadError"
      role="alert"
      class="form-error"
    >
      {{ loadError }}
    </p>
    <template v-if="book && options">
      <p class="crumb">
        <RouterLink :to="`/books/${book.id}`">
          {{ book.title }}
        </RouterLink>
      </p>
      <h1>Новый перевод</h1>

      <section
        v-if="!configs.length"
        class="empty"
      >
        <p>Чтобы переводить, подключите модель: локальную Ollama или внешний сервис.</p>
        <RouterLink
          to="/settings/providers"
          class="button primary"
        >
          Подключить модель
        </RouterLink>
      </section>

      <form
        v-else
        @submit.prevent="submit"
      >
        <div class="pair">
          <label class="field">
            <span>Подключение</span>
            <select
              v-model="form.provider_config_id"
              name="provider_config_id"
            >
              <option
                v-for="c in configs"
                :key="c.id"
                :value="c.id"
              >{{ c.name }}</option>
            </select>
          </label>
          <label class="field">
            <span>Модель</span>
            <select
              v-model="form.model_id"
              name="model_id"
              :disabled="!models.length"
            >
              <option
                v-for="m in models"
                :key="m.model_id"
                :value="m.model_id"
              >{{ m.display_name || m.model_id }}</option>
            </select>
          </label>
        </div>
        <p
          v-if="modelsError"
          role="alert"
          class="form-error"
        >
          {{ modelsError }}
        </p>

        <div
          v-if="egress === 'allowed'"
          class="egress"
          role="note"
        >
          Текст книги будет отправлен в «{{ config?.name }}» и покинет этот сервер.
        </div>
        <div
          v-else-if="egress === 'blocked'"
          class="egress blocked"
          role="note"
        >
          Подключение «{{ config?.name }}» не разрешает отправку текста книг наружу. Разрешите её в разделе
          <RouterLink to="/settings/providers">
            «Модели»
          </RouterLink> или выберите локальную модель.
        </div>

        <div class="pair">
          <label class="field">
            <span>Язык оригинала</span>
            <select
              v-model="form.source_lang"
              name="source_lang"
            >
              <option value="auto">{{ languageName('auto') }}</option>
              <option
                v-for="code in languages"
                :key="code"
                :value="code"
              >{{ languageName(code) }}</option>
            </select>
          </label>
          <label class="field">
            <span>Язык перевода</span>
            <select
              v-model="form.target_lang"
              name="target_lang"
            >
              <option
                v-for="code in languages"
                :key="code"
                :value="code"
              >{{ languageName(code) }}</option>
            </select>
          </label>
        </div>

        <fieldset class="choice">
          <legend>Стиль</legend>
          <label
            v-for="s in options.styles"
            :key="s"
          >
            <input
              v-model="form.style"
              type="radio"
              name="style"
              :value="s"
            >
            <span>
              <strong>{{ styleText[s]?.label ?? s }}</strong>
              <small>{{ styleText[s]?.hint }}</small>
            </span>
          </label>
        </fieldset>

        <fieldset class="choice">
          <legend>Точность</legend>
          <label
            v-for="d in options.determinism"
            :key="d"
          >
            <input
              v-model="form.determinism"
              type="radio"
              name="determinism"
              :value="d"
            >
            <span>
              <strong>{{ determinismText[d]?.label ?? d }}</strong>
              <small>{{ determinismText[d]?.hint }}</small>
            </span>
          </label>
        </fieldset>

        <GlossaryEditor v-model="glossary" />

        <p
          v-if="submitError"
          role="alert"
          class="form-error"
        >
          {{ submitError }}
        </p>
        <button
          type="submit"
          class="button primary"
          :disabled="!canSubmit"
        >
          Начать перевод
        </button>
      </form>
    </template>
  </main>
</template>

<style scoped>
.setup {
  max-width: 48rem;
}

.crumb {
  margin: 0 0 var(--space-2);
  color: var(--muted);
  font-family: var(--font-book);
}

h1 {
  font-family: var(--font-book);
  font-size: var(--step-3);
  font-weight: 500;
  margin-bottom: var(--space-5);
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

.egress {
  padding: var(--space-3) var(--space-4);
  border-left: 3px solid var(--highlight);
  background: var(--paper-raised);
}

.egress.blocked {
  border-left-color: var(--proof);
}

.choice {
  border: none;
  margin: 0;
  padding: 0;
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(13rem, 1fr));
  gap: var(--space-2) var(--space-4);
}

.choice legend {
  font-weight: 500;
  padding: 0;
  margin-bottom: var(--space-2);
}

.choice label {
  display: flex;
  gap: var(--space-2);
  align-items: flex-start;
  cursor: pointer;
}

.choice input {
  margin-top: 0.3em;
}

.choice span {
  display: grid;
}

.choice small {
  color: var(--muted);
}

form > .button {
  justify-self: start;
}

.empty p {
  color: var(--muted);
}

@media (max-width: 40rem) {
  .pair {
    grid-template-columns: 1fr;
  }
}
</style>
