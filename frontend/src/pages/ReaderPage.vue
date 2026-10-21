<script setup lang="ts">
// Читалка (ТЗ §8.3): оглавление, оригинал и перевод рядом, переключение
// языковых версий и режимов, поиск, позиция чтения, правка и повторный
// перевод фрагмента. Адрес: /read/:bookId?t=<перевод>&ch=<глава>&n=<фрагмент>.
import { NButton, NButtonGroup } from 'naive-ui'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { getBook, getChapter, getTOC } from '../api/books'
import { errorMessage } from '../api/client'
import { getPosition, putPosition } from '../api/reader'
import { listTranslations } from '../api/translations'
import type { Book, Chapter, Project, ReadMode, SearchResult, Segment, TocEntry } from '../api/types'
import { languageName, sourceLanguageName } from '../lib/format'
import { fetchChapterSegments, usePositionSaver } from '../lib/position'
import ErrorAlert from '../components/ErrorAlert.vue'
import LinkButton from '../components/LinkButton.vue'
import SelectField from '../components/SelectField.vue'
import ReaderColumns from '../reader/ReaderColumns.vue'
import ReaderSearch from '../reader/ReaderSearch.vue'
import { clearHits, hitRanges, paintHits } from '../reader/highlight'
import SegmentRow from '../reader/SegmentRow.vue'

const route = useRoute()
const router = useRouter()
const bookId = String(route.params.bookId)

const book = ref<Book | null>(null)
const toc = ref<TocEntry[]>([])
const projects = ref<Project[]>([])
const chapter = ref<Chapter | null>(null)
const segments = ref<Map<string, Segment>>(new Map())
const mode = ref<ReadMode>('parallel')
const found = ref('')
// Находка поиска, слова которой подсвечены в тексте.
const hit = ref<SearchResult | null>(null)
const error = ref('')
const tocOpen = ref(false)

const translationId = computed(() => (typeof route.query.t === 'string' ? route.query.t : ''))
const chapterIndex = computed(() => {
  const n = Number(route.query.ch)
  return Number.isInteger(n) && n >= 0 ? n : null
})
const project = computed(() => projects.value.find((p) => p.id === translationId.value))
const effectiveMode = computed<ReadMode>(() => (project.value ? mode.value : 'original'))

/** Главы в порядке чтения, с глубиной для отступа в оглавлении. */
const flatToc = computed(() => {
  const out: { index: number; title: string; depth: number }[] = []
  const walk = (entries: TocEntry[], depth: number) => {
    for (const e of entries) {
      out.push({ index: e.index, title: e.title || 'Без названия', depth })
      walk(e.children ?? [], depth + 1)
    }
  }
  walk(toc.value, 0)
  return out
})
const position = computed(() => flatToc.value.findIndex((c) => c.index === chapterIndex.value))
const prev = computed(() => flatToc.value[position.value - 1])
const next = computed(() => (position.value >= 0 ? flatToc.value[position.value + 1] : undefined))
const chapterTitle = (index: number) => flatToc.value.find((c) => c.index === index)?.title ?? `Глава ${index + 1}`

const translationLabel = (p: Project) => `${sourceLanguageName(p.source_lang)} → ${languageName(p.target_lang)}`
const translationOptions = computed(() => [
  { label: 'Без перевода', value: '' },
  ...projects.value.map((p) => ({ label: translationLabel(p), value: p.id })),
])

const saver = usePositionSaver((p) => putPosition(bookId, p))
// Место, где читатель остановился: к нему прокручиваем без подсветки —
// подсветка (n в адресе) только для явных переходов, например из поиска.
let restoreNode = ''

function remember(nodeId: string) {
  if (chapterIndex.value === null) return
  saver.schedule({
    chapter_index: chapterIndex.value,
    node_id: nodeId,
    translation_id: translationId.value || null,
    mode: mode.value,
  })
}

async function loadChapter() {
  if (chapterIndex.value === null) return
  error.value = ''
  try {
    const [ch, segs] = await Promise.all([
      getChapter(bookId, chapterIndex.value),
      translationId.value ? fetchChapterSegments(translationId.value, chapterIndex.value) : Promise.resolve([]),
    ])
    chapter.value = ch
    segments.value = new Map(segs.map((s) => [s.node_id, s]))
  } catch (e) {
    error.value = errorMessage(e)
    return
  }
  await nextTick()
  observeRows()
  const target = typeof route.query.n === 'string' ? route.query.n : ''
  const restore = restoreNode
  restoreNode = ''
  if (target) {
    found.value = target
    document.querySelector(`[data-node="${CSS.escape(target)}"]`)?.scrollIntoView({ block: 'start' })
    paintHit()
  } else if (restore) {
    document.querySelector(`[data-node="${CSS.escape(restore)}"]`)?.scrollIntoView({ block: 'start' })
  } else {
    window.scrollTo?.({ top: 0 })
  }
}

watch([chapterIndex, translationId], () => void loadChapter())

onMounted(async () => {
  try {
    const [b, t, p, saved] = await Promise.all([getBook(bookId), getTOC(bookId), listTranslations(bookId), getPosition(bookId)])
    book.value = b
    toc.value = t
    projects.value = p
    if (saved?.mode) mode.value = saved.mode
    if (chapterIndex.value === null) {
      const ch = saved?.chapter_index ?? flatToc.value[0]?.index ?? 0
      const tr = translationId.value || saved?.translation_id || ''
      restoreNode = saved?.chapter_index === ch ? saved.node_id : ''
      await router.replace({ query: { ...route.query, ch: String(ch), ...(tr ? { t: tr } : {}) } })
      return // загрузку главы запустит watch
    }
    await loadChapter()
  } catch (e) {
    error.value = errorMessage(e)
  }
})

function openChapter(index: number) {
  tocOpen.value = false
  found.value = ''
  forgetHit()
  void router.push({ query: { ...route.query, ch: String(index), n: undefined } })
}

function setTranslation(id: string) {
  forgetHit()
  void router.replace({ query: { ...route.query, t: id || undefined, n: undefined } })
}

function setMode(m: ReadMode) {
  mode.value = m
  const top = visible.size ? [...visible.keys()][0] ?? '' : ''
  remember(top)
}

function goTo(r: SearchResult) {
  found.value = r.node_id
  hit.value = r
  void router.push({ query: { ...route.query, ch: String(r.chapter_index), n: r.node_id } })
  if (r.chapter_index === chapterIndex.value) {
    document.querySelector(`[data-node="${CSS.escape(r.node_id)}"]`)?.scrollIntoView({ block: 'start' })
    paintHit()
  }
  // Другая глава: подсветит loadChapter, когда текст будет на странице.
}

// Слова находки подсвечиваются в своей колонке: в оригинале или переводе.
function paintHit() {
  const h = hit.value
  const cell = h && document.querySelector(`[data-node="${CSS.escape(h.node_id)}"] .${h.in}`)
  if (h && cell && h.node_id === found.value) paintHits(hitRanges(cell, h.hits))
  else clearHits()
}

function forgetHit() {
  hit.value = null
  clearHits()
}

function updateSegment(s: Segment) {
  segments.value = new Map(segments.value).set(s.node_id, s)
}

// Позиция — самый верхний видимый фрагмент.
const visible = new Map<string, number>()
let observer: IntersectionObserver | null = null

function observeRows() {
  if (typeof IntersectionObserver === 'undefined') return
  observer?.disconnect()
  visible.clear()
  observer = new IntersectionObserver(
    (entries) => {
      for (const e of entries) {
        const node = (e.target as HTMLElement).dataset.node ?? ''
        if (e.isIntersecting) visible.set(node, e.boundingClientRect.top)
        else visible.delete(node)
      }
      const top = [...visible.entries()].sort((a, b) => a[1] - b[1])[0]
      if (top) remember(top[0])
    },
    { rootMargin: '-80px 0px -40% 0px' },
  )
  document.querySelectorAll<HTMLElement>('[data-node]').forEach((el) => observer?.observe(el))
}

const flush = () => saver.flush()
onMounted(() => window.addEventListener('pagehide', flush))
onBeforeUnmount(() => {
  clearHits()
  window.removeEventListener('pagehide', flush)
  observer?.disconnect()
  saver.flush()
})
</script>

<template>
  <div class="reader">
    <header class="toolbar">
      <NButton
        class="toc-toggle"
        size="small"
        :aria-expanded="tocOpen"
        aria-controls="reader-toc"
        @click="tocOpen = !tocOpen"
      >
        Оглавление
      </NButton>
      <RouterLink
        v-if="book"
        :to="`/books/${bookId}`"
        class="book-title"
      >
        {{ book.title }}
      </RouterLink>
      <!-- Переводов ещё нет — выбирать нечего, ведём к новому переводу. -->
      <LinkButton
        v-if="book && !projects.length"
        :to="`/books/${bookId}/translate`"
        class="translate-link"
        size="small"
        type="primary"
        secondary
      >
        Перевести книгу
      </LinkButton>
      <SelectField
        v-else
        :model-value="translationId"
        class="translation-pick"
        label="Языковая версия"
        name="translation"
        :options="translationOptions"
        size="small"
        hide-label
        @update:model-value="setTranslation"
      />
      <NButtonGroup
        class="modes"
        size="small"
        role="group"
        aria-label="Вид"
      >
        <NButton
          v-for="m in ([['parallel', 'Рядом'], ['original', 'Оригинал'], ['translation', 'Перевод']] as const)"
          :key="m[0]"
          :type="effectiveMode === m[0] ? 'primary' : 'default'"
          :data-mode="m[0]"
          :aria-pressed="effectiveMode === m[0]"
          :disabled="!project && m[0] !== 'original'"
          @click="setMode(m[0])"
        >
          {{ m[1] }}
        </NButton>
      </NButtonGroup>
      <ReaderSearch
        :book-id="bookId"
        :translation-id="translationId"
        :chapter-title="chapterTitle"
        @go="goTo"
      />
    </header>

    <div class="body">
      <nav
        id="reader-toc"
        class="reader-toc"
        :class="{ open: tocOpen }"
        aria-label="Оглавление"
      >
        <ol>
          <li
            v-for="c in flatToc"
            :key="c.index"
            :style="{ paddingLeft: `${c.depth * 0.9}rem` }"
          >
            <button
              type="button"
              :data-chapter="c.index"
              :aria-current="c.index === chapterIndex ? 'page' : undefined"
              @click="openChapter(c.index)"
            >
              {{ c.title }}
            </button>
          </li>
        </ol>
      </nav>

      <main class="text">
        <h1 class="visually-hidden">
          {{ book?.title }}
        </h1>
        <ErrorAlert :message="error" />
        <template v-if="chapter && book">
          <ReaderColumns
            :mode="effectiveMode"
            :source="project?.source_lang ?? book.language ?? ''"
            :target="project?.target_lang"
          >
            <SegmentRow
              v-for="b in chapter.blocks"
              :key="b.node_id"
              :block="b"
              :segment="segments.get(b.node_id)"
              :mode="effectiveMode"
              :book-id="bookId"
              :found="found === b.node_id"
              @update="updateSegment"
            />
          </ReaderColumns>
          <p
            v-if="!chapter.blocks.length"
            class="muted"
          >
            В этой главе нет текста — только вложенные разделы.
          </p>
          <nav
            class="pager"
            aria-label="Соседние главы"
          >
            <button
              v-if="prev"
              type="button"
              class="prev"
              @click="openChapter(prev.index)"
            >
              <small>Предыдущая глава</small>
              <span>{{ prev.title }}</span>
            </button>
            <button
              v-if="next"
              type="button"
              class="next"
              @click="openChapter(next.index)"
            >
              <small>Следующая глава</small>
              <span>{{ next.title }}</span>
            </button>
          </nav>
        </template>
      </main>
    </div>
  </div>
</template>

<style scoped>
.toolbar {
  position: sticky;
  top: env(safe-area-inset-top, 0px);
  z-index: 5;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2) var(--space-4);
  padding: var(--space-2) var(--page-inset);
  background: color-mix(in srgb, var(--surface) 92%, transparent);
  backdrop-filter: blur(6px);
  border-bottom: 1px solid var(--rule-soft);
}

.book-title {
  font-family: var(--font-book);
  font-weight: 600;
  text-decoration: none;
  max-width: 22rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.translation-pick {
  min-width: 13rem;
}

.reader-search {
  margin-left: auto;
}

.body {
  display: grid;
  grid-template-columns: 15rem minmax(0, 1fr);
  gap: var(--space-6);
  width: var(--page-width);
  margin: 0 auto;
  padding-block: var(--space-5) var(--space-7);
}

.toc-toggle {
  display: none;
}

.reader-toc {
  position: sticky;
  top: 4.5rem;
  align-self: start;
  max-height: calc(100vh - 6rem);
  overflow-y: auto;
}

.reader-toc ol {
  margin: 0;
  padding: 0;
  list-style: none;
}

.reader-toc button {
  width: 100%;
  padding: var(--space-1) var(--space-2);
  border: none;
  border-radius: var(--radius);
  background: none;
  font-family: var(--font-book);
  text-align: left;
  color: var(--muted);
  cursor: pointer;
}

.reader-toc button:hover {
  color: var(--ink-original);
}

.reader-toc button:hover {
  background: var(--surface);
}

.reader-toc button[aria-current='page'] {
  color: var(--ink-original);
  font-weight: 600;
  background: color-mix(in srgb, var(--ink-original) 7%, var(--surface));
}

.muted {
  color: var(--muted);
}

.pager {
  display: flex;
  justify-content: space-between;
  gap: var(--space-4);
  margin-top: var(--space-7);
  padding-top: var(--space-4);
  border-top: 1px solid var(--rule-soft);
}

.pager button {
  display: grid;
  gap: var(--space-1);
  max-width: 48%;
  padding: var(--space-3) var(--space-4);
  border: 1px solid var(--rule-soft);
  border-radius: var(--radius-card);
  background: var(--surface);
  box-shadow: var(--shadow-card);
  transition: border-color 0.2s;
  font-family: var(--font-book);
  text-align: left;
  cursor: pointer;
}

.pager button:hover {
  border-color: var(--ink-original);
}

.pager .next {
  margin-left: auto;
  text-align: right;
}

.pager small {
  font-family: var(--font-ui);
  color: var(--muted);
}

@media (max-width: 64rem) {
  .body {
    grid-template-columns: minmax(0, 1fr);
  }

  .toolbar {
    position: static;
  }

  .toc-toggle {
    display: inline-flex;
  }

  .reader-toc {
    display: none;
  }

  .reader-toc.open {
    display: block;
    position: static;
    max-height: none;
    padding-bottom: var(--space-4);
    border-bottom: 1px solid var(--rule);
  }

  .reader-search {
    margin-left: 0;
    width: 100%;
  }

  .reader-search :deep(.search) {
    width: 100%;
  }
}
</style>
