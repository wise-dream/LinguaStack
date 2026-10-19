<script setup lang="ts">
// Строка читалки: блок оригинала и его перевод, на полях — пометки и
// правка. Код, формулы и картинки не переводятся и идут во всю ширину.
import { computed, nextTick, ref } from 'vue'

import { assetUrl } from '../api/books'
import { errorMessage } from '../api/client'
import { patchSegment, retrySegment } from '../api/translations'
import type { Block, ReadMode, Segment } from '../api/types'
import { renderInline } from '../lib/inline'
import ProofMark from './ProofMark.vue'

const props = defineProps<{
  block: Block
  segment?: Segment
  mode: ReadMode
  bookId: string
  found?: boolean
}>()
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
const draft = ref('')
const error = ref('')
const busy = ref(false)
const area = ref<HTMLTextAreaElement | null>(null)

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
    :class="[`mode-${mode}`, block.type, { span: isSpan, found }]"
    :data-node="block.node_id"
  >
    <template v-if="isSpan">
      <pre v-if="block.type === 'code_block' || block.type === 'raw_html'"><code>{{ block.text }}</code></pre>
      <pre
        v-else-if="block.type === 'math_block'"
        class="math"
      >{{ block.text }}</pre>
      <figure v-else-if="block.type === 'image' && block.meta?.src">
        <img
          :src="assetUrl(bookId, block.meta.src)"
          :alt="block.meta.alt ?? ''"
          loading="lazy"
        >
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
          <label class="visually-hidden">Перевод фрагмента</label>
          <textarea
            ref="area"
            v-model="draft"
            rows="4"
            @keydown.esc.prevent="cancel"
            @keydown.enter.ctrl.prevent="save"
            @keydown.enter.meta.prevent="save"
          />
          <div class="editor-actions">
            <button
              type="submit"
              class="button primary"
              :disabled="busy"
            >
              Сохранить
            </button>
            <button
              type="button"
              class="button"
              @click="cancel"
            >
              Отмена
            </button>
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
          v-if="!editing"
          class="actions"
        >
          <button
            type="button"
            class="link edit"
            @click="startEdit"
          >
            Править
          </button>
          <button
            type="button"
            class="link retry"
            @click="retry"
          >
            Перевести заново
          </button>
        </div>
        <p
          v-if="error"
          role="alert"
          class="row-error"
        >
          {{ error }}
        </p>
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
.row:focus-within .actions {
  opacity: 1;
}

@media (hover: none) {
  .actions {
    opacity: 1;
  }
}

.link {
  padding: 0;
  border: none;
  background: none;
  color: var(--muted);
  font-size: var(--step--1);
  text-decoration: underline;
  text-underline-offset: 0.2em;
  cursor: pointer;
}

.link:hover {
  color: var(--ink-original);
}

.row-error {
  margin: 0;
  font-size: var(--step--1);
  color: var(--proof);
}

.editor {
  display: grid;
  gap: var(--space-2);
}

.editor textarea {
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

figure img {
  max-height: 70vh;
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

  .actions {
    opacity: 1;
  }
}
</style>
