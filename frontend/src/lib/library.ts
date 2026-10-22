// Список библиотеки — общей или своей (дополнение §9): поиск по названию и
// автору и порции по курсору. Запрос живёт в адресе страницы (?q=): ссылка
// и возврат назад открывают тот же поиск.
import { onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { getLibrary } from '../api/books'
import { errorMessage } from '../api/client'
import type { LibraryBook } from '../api/types'

/** Пауза после ввода, после которой уходит поиск. */
export const SEARCH_DELAY_MS = 300

export function useLibrary(mine: boolean) {
  const route = useRoute()
  const router = useRouter()

  /** Текст в поле поиска. */
  const q = ref(typeof route.query.q === 'string' ? route.query.q : '')
  /** Запрос, по которому показан список: поле могло уйти вперёд. */
  const shown = ref('')
  const books = ref<LibraryBook[] | null>(null)
  const total = ref(0)
  const next = ref<string | undefined>()
  const loading = ref(false)
  const error = ref('')

  // Ответ на устаревший запрос (поле успело измениться) не показывается.
  let seq = 0
  let timer: ReturnType<typeof setTimeout> | undefined

  async function load(after?: string) {
    const my = ++seq
    const query = q.value.trim()
    loading.value = true
    error.value = ''
    try {
      const page = await getLibrary({ mine, q: query, after })
      if (my !== seq) return
      books.value = after ? [...(books.value ?? []), ...page.books] : page.books
      total.value = page.total
      next.value = page.next
      shown.value = query
    } catch (e) {
      if (my === seq) error.value = errorMessage(e)
    } finally {
      if (my === seq) loading.value = false
    }
  }

  /** Искать сейчас (Enter в поле); при наборе поиск уходит сам после паузы. */
  function search() {
    clearTimeout(timer)
    const query = q.value.trim()
    if ((route.query.q ?? '') !== query) void router.replace({ query: { ...route.query, q: query || undefined } })
    void load()
  }

  watch(q, () => {
    clearTimeout(timer)
    timer = setTimeout(search, SEARCH_DELAY_MS)
  })

  /** Следующая порция. */
  function more() {
    if (next.value && !loading.value) void load(next.value)
  }

  /** Книга удалена: из списка и из счёта. */
  function removed(id: string) {
    books.value = (books.value ?? []).filter((b) => b.id !== id)
    total.value = Math.max(0, total.value - 1)
  }

  onBeforeUnmount(() => clearTimeout(timer))
  void load()

  return { q, shown, books, total, next, loading, error, search, more, removed }
}
