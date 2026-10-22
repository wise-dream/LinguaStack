<script setup lang="ts">
// Строка читалки: блок оригинала и его перевод, на полях — пометки и
// правка. Код, формулы и картинки не переводятся и идут во всю ширину.
import { NButton, NInput } from 'naive-ui'
import { computed, nextTick, ref } from 'vue'

import { assetUrl } from '../api/books'
import { errorMessage } from '../api/client'
import { patchSegment, retrySegment } from '../api/translations'
import type { Block, ReadMode, Segment } from '../api/types'
import { renderInline } from '../lib/inline'
import ErrorAlert from '../components/ErrorAlert.vue'
import ImageViewer from '../components/ImageViewer.vue'
import ProofMark from './ProofMark.vue'

const props = withDefaults(defineProps<{
  block: Block
  segment?: Segment
  mode: ReadMode
  bookId: string
  found?: boolean
  /** Свой перевод: можно править и повторять. Чужой из общей библиотеки — только читать. */
  editable?: boolean
}>(), { segment: undefined, found: false, editable: true })
const emit = defineEmits<{ update: [segment: Segment] }>()

const spanTypes = ['code_block', 'math_block', 'image', 'thematic_break', 'raw_html']
const isSpan = computed(() => spanTypes.includes(props.block.type))

const showOriginal = computed(() => props.mode !== 'translation' || !props.segment?.translated_text)
const showTranslation = computed(() => props.mode !== 'original' && !!props.segment)
const fallback = computed(() => props.mode === 'translation' && !props.segment?.translated_text)

const headingTag = computed(() => `h${Math.min((props.block.level ?? 1) + 1, 6)}`)
const tag = computed(() => {
  switch (props.block.type) {
    case 'heading':
      return headingTag.value
    case 'blockquote':
      return 'blockquote'
    default:
      return 'p'
  }
})

const editing = ref(false)
/** Картинка открыта в просмотре с масштабом. */
const viewing = ref(false)
// На телефоне наведения нет: действия появляются по касанию строки.
const revealed = ref(false)

function toggleReveal(e: MouseEvent) {
  if ((e.target as HTMLElement).closest('a, button, textarea, form')) return
  revealed.value = !revealed.value
}
const draft = ref('')
const error = ref('')
const busy = ref(false)
const area = ref<InstanceType<typeof NInput> | null>(null)

async function startEdit() {
  draft.value = props.segment?.translated_text ?? ''
  error.value = ''
  editing.value = true
  await nextTick()
  area.value?.focus()
}

function cancel() {
  editing.value = false
  error.value = ''
}

async function save() {
  if (!props.segment || busy.value) return
  busy.value = true
  error.value = ''
  try {
    emit('update', await patchSegment(props.segment.id, draft.value))
    editing.value = false
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    busy.value = false
  }
}

async function retry() {
  if (!props.segment) return
  error.value = ''
  try {
    emit('update', await retrySegment(props.segment.id))
  } catch (e) {
    error.value = errorMessage(e)
  }
}
</script>

<template>
  <div
    class="row"
    :class="[`mode-${mode}`, block.type, { span: isSpan, found, revealed }]"
    :data-node="block.node_id"
    @click="toggleReveal"
  >
    <template v-if="isSpan">
      <pre v-if="block.type === 'code_block' || block.type === 'raw_html'"><code>{{ block.text }}</code></pre>
      <pre
        v-else-if="block.type === 'math_block'"
        class="math"
      >{{ block.text }}</pre>
      <figure v-else-if="block.type === 'image' && block.meta?.src">
        <button
          type="button"
          class="zoom"
          :aria-label="block.meta.alt ? `Открыть картинку: ${block.meta.alt}` : 'Открыть картинку'"
          @click="viewing = true"
        >
          <img
            :src="assetUrl(bookId, block.meta.src)"
            :alt="block.meta.alt ?? ''"
            loading="lazy"
          >
        </button>
        <ImageViewer
          v-if="viewing"
          :src="assetUrl(bookId, block.meta.src)"
          :alt="block.meta.alt ?? ''"
          @close="viewing = false"
        />
      </figure>
      <hr v-else-if="block.type === 'thematic_break'">
    </template>

    <template v-else>
      <!-- eslint-disable vue/no-v-html, vue/no-v-text-v-html-on-component -- renderInline сначала экранирует
           текст книги; tag — всегда имя HTML-элемента (p, h2…h6, blockquote), не компонент -->
      <component
        :is="tag"
        v-if="showOriginal"
        class="original"
        :class="{ fallback }"
        v-html="renderInline(block.text)"
      />
      <div
        v-if="showTranslation && !fallback"
        class="cell"
      >
        <form
          v-if="editing"
          class="editor"
          @submit.prevent="save"
        >
          <NInput
            ref="area"
            v-model:value="draft"
            type="textarea"
            :autosize="{ minRows: 4 }"
            :input-props="{ 'aria-label': 'Перевод фрагмента' }"
            placeholder=""
            @keydown.esc.prevent="cancel"
            @keydown.enter.ctrl.prevent="save"
            @keydown.enter.meta.prevent="save"
          />
          <div class="editor-actions">
            <NButton
              attr-type="submit"
              type="primary"
              size="small"
              :loading="busy"
              :disabled="busy"
            >
              Сохранить
            </NButton>
            <NButton
              size="small"
              @click="cancel"
            >
              Отмена
            </NButton>
            <small>Ctrl+Enter — сохранить, Esc — отмена</small>
          </div>
        </form>
        <component
          :is="tag"
          v-else-if="segment?.translated_text"
          class="translation"
          v-html="renderInline(segment.translated_text)"
        />
        <!-- eslint-enable vue/no-v-html, vue/no-v-text-v-html-on-component -->
        <p
          v-else
          class="translation pending"
        >
          {{ segment?.status === 'failed' ? 'Не переведено' : 'Ещё не переведено' }}
        </p>
      </div>

      <div
        v-if="segment && mode !== 'original'"
        class="margin"
      >
        <ProofMark :segment="segment" />
        <div
          v-if="!editing && editable"
          class="actions"
        >
          <NButton
            class="edit"
            size="tiny"
            text
            @click="startEdit"
          >
            Править
          </NButton>
          <NButton
            class="retry"
            size="tiny"
            text
            @click="retry"
          >
            Перевести заново
          </NButton>
        </div>
        <ErrorAlert
          :message="error"
          size="small"
        />
      </div>
    </template>
  </div>
</template>

<style scoped>
.row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) 11rem;
  column-gap: var(--space-6);
  padding-block: var(--space-2);
  scroll-margin-top: 6rem;
}

.row.mode-original,
.row.mode-translation {
  grid-template-columns: minmax(0, var(--measure)) 11rem;
}

.row.span {
  grid-template-columns: minmax(0, 1fr);
}

.row.found {
  background: color-mix(in srgb, var(--highlight) 35%, transparent);
  box-shadow: 0 0 0 var(--space-2) color-mix(in srgb, var(--highlight) 35%, transparent);
}

.original,
.translation {
  margin: 0;
  font-family: var(--font-book);
  font-size: var(--book-size);
  line-height: var(--book-leading);
  hyphens: auto;
}

.original {
  color: var(--ink-original);
}

.translation {
  color: var(--ink-translation);
}

h2.original,
h2.translation {
  font-size: var(--step-2);
  font-weight: 600;
  line-height: 1.25;
  margin-top: var(--space-5);
}

h3.original,
h3.translation,
h4.original,
h4.translation {
  font-size: var(--step-1);
  font-weight: 600;
  margin-top: var(--space-4);
}

blockquote.original,
blockquote.translation {
  padding-left: var(--space-4);
  border-left: 2px solid var(--rule);
  font-style: italic;
}

.list_item .original::before,
.list_item .translation::before {
  content: '• ';
}

.fallback {
  color: var(--muted);
}

.pending {
  color: var(--muted);
  font-family: var(--font-ui);
  font-size: var(--step--1);
  font-style: italic;
}

.margin {
  display: grid;
  align-content: start;
  gap: var(--space-2);
}

.actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-1) var(--space-3);
  opacity: 0;
}

.row:hover .actions,
.row:focus-within .actions,
.row.revealed .actions {
  opacity: 1;
}

.editor {
  display: grid;
  gap: var(--space-2);
}

.editor :deep(textarea) {
  font-family: var(--font-book);
  font-size: var(--book-size);
  line-height: var(--book-leading);
  color: var(--ink-translation);
}

.editor-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2);
}

.editor-actions small {
  color: var(--muted);
}

pre {
  margin: var(--space-2) 0;
  padding: var(--space-3) var(--space-4);
  overflow-x: auto;
  background: var(--paper-raised);
  border: 1px solid var(--rule);
  border-radius: var(--radius);
  font-size: 0.9rem;
}

figure {
  margin: var(--space-3) 0;
  text-align: center;
}

/* Картинка не шире колонки и не выше экрана, пропорции сохраняются;
   разглядеть подробности — в просмотре по нажатию. */
.zoom {
  display: inline-block;
  max-width: 100%;
  padding: 0;
  border: none;
  border-radius: var(--radius);
  background: none;
  cursor: zoom-in;
}

figure img {
  display: block;
  max-width: 100%;
  max-height: 70vh;
  height: auto;
}

hr {
  border: none;
  border-top: 1px solid var(--rule);
  margin: var(--space-5) 20%;
}

@media (max-width: 56rem) {
  .row,
  .row.mode-original,
  .row.mode-translation {
    grid-template-columns: minmax(0, 1fr);
    row-gap: var(--space-2);
  }

  /* Перевод — ближе к своему оригиналу, чем к соседней паре. */
  .row {
    row-gap: var(--space-1);
    padding-block: var(--space-3);
  }

  /* Скрытые действия не занимают строку под абзацем, но остаются
     доступны с клавиатуры: при фокусе строка их показывает. */
  .actions {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
    white-space: nowrap;
    opacity: 1;
  }

  .row.revealed .actions,
  .row:focus-within .actions {
    position: static;
    width: auto;
    height: auto;
    overflow: visible;
    clip: auto;
    white-space: normal;
  }
}
</style>
