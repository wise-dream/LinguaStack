<script setup lang="ts">
// Библиотека (ТЗ §8.2): опись книг пользователя с переводами каждой.
import { computed, onMounted, ref } from 'vue'

import { deleteBook, listBooks } from '../api/books'
import { errorMessage } from '../api/client'
import { listTranslations } from '../api/translations'
import type { Book, Project } from '../api/types'
import ConfirmButton from '../components/ConfirmButton.vue'
import LangPair from '../components/LangPair.vue'
import StatusMark from '../components/StatusMark.vue'
import { formatDate, formatName } from '../lib/format'

const books = ref<Book[] | null>(null)
const translations = ref<Project[]>([])
const error = ref('')

const byBook = computed(() => {
  const map = new Map<string, Project[]>()
  for (const p of translations.value) {
    map.set(p.book_id, [...(map.get(p.book_id) ?? []), p])
  }
  return map
})

onMounted(async () => {
  try {
    const [b, t] = await Promise.all([listBooks(), listTranslations()])
    books.value = b
    translations.value = t
  } catch (e) {
    error.value = errorMessage(e)
  }
})

async function remove(book: Book) {
  error.value = ''
  try {
    await deleteBook(book.id)
    books.value = (books.value ?? []).filter((b) => b.id !== book.id)
  } catch (e) {
    error.value = errorMessage(e)
  }
}
</script>

<template>
  <main class="page">
    <div class="heading">
      <h1>Библиотека</h1>
      <RouterLink
        v-if="books?.length"
        to="/import"
        class="button primary"
      >
        Загрузить книгу
      </RouterLink>
    </div>

    <p
      v-if="error"
      role="alert"
      class="form-error"
    >
      {{ error }}
    </p>

    <p
      v-if="books === null && !error"
      class="muted"
    >
      Загружаю библиотеку…
    </p>

    <section
      v-else-if="books?.length === 0"
      class="empty"
    >
      <h2>Загрузите первую книгу</h2>
      <p>Подойдут EPUB, Markdown, обычный текст и ZIP-архив документации (mdBook, MkDocs).</p>
      <RouterLink
        to="/import"
        class="button primary"
      >
        Загрузить книгу
      </RouterLink>
    </section>

    <table
      v-else-if="books"
      class="ledger"
    >
      <thead>
        <tr>
          <th scope="col">
            Книга
          </th>
          <th scope="col">
            Формат
          </th>
          <th scope="col">
            Добавлена
          </th>
          <th scope="col">
            Переводы
          </th>
          <th scope="col">
            <span class="visually-hidden">Действия</span>
          </th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="book in books"
          :key="book.id"
          :data-book="book.id"
        >
          <td class="book">
            <RouterLink
              :to="`/books/${book.id}`"
              class="title"
            >
              {{ book.title }}
            </RouterLink>
            <span
              v-if="book.author"
              class="author"
            >{{ book.author }}</span>
          </td>
          <td>{{ formatName(book.source_format) }}</td>
          <td>{{ formatDate(book.created_at) }}</td>
          <td>
            <ul
              v-if="byBook.get(book.id)?.length"
              class="translations"
            >
              <li
                v-for="p in byBook.get(book.id)"
                :key="p.id"
              >
                <RouterLink :to="`/translations/${p.id}`">
                  <LangPair
                    :source="p.source_lang"
                    :target="p.target_lang"
                  />
                </RouterLink>
                <StatusMark :status="p.status" />
              </li>
            </ul>
            <span
              v-else
              class="muted"
            >Ещё нет</span>
          </td>
          <td class="actions">
            <RouterLink
              :to="`/read/${book.id}`"
              class="button"
            >
              Читать
            </RouterLink>
            <ConfirmButton
              label="Удалить"
              @confirm="remove(book)"
            />
          </td>
        </tr>
      </tbody>
    </table>
  </main>
</template>

<style scoped>
.heading {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--space-4);
  margin-bottom: var(--space-5);
}

h1 {
  font-family: var(--font-book);
  font-size: var(--step-3);
  font-weight: 500;
}

.muted {
  color: var(--muted);
}

.empty {
  max-width: var(--measure);
  padding-block: var(--space-6);
}

.empty h2 {
  font-family: var(--font-book);
  font-size: var(--step-2);
  font-weight: 500;
  margin-bottom: var(--space-3);
}

.empty p {
  margin: 0 0 var(--space-5);
  color: var(--muted);
}

.ledger {
  width: 100%;
  border-collapse: collapse;
}

.ledger th {
  text-align: left;
  font-weight: 500;
  color: var(--muted);
  font-size: var(--step--1);
  padding: var(--space-2) var(--space-3);
  border-bottom: 1px solid var(--ink-original);
}

.ledger td {
  vertical-align: top;
  padding: var(--space-3);
  border-bottom: 1px solid var(--rule);
}

.book .title {
  display: block;
  font-family: var(--font-book);
  font-size: var(--step-1);
  text-decoration: none;
}

.book .title:hover {
  text-decoration: underline;
}

.author {
  color: var(--muted);
}

.translations {
  margin: 0;
  padding: 0;
  list-style: none;
  display: grid;
  gap: var(--space-1);
}

.translations li {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-1) var(--space-3);
}

.actions {
  white-space: nowrap;
  text-align: right;
}

.actions > * + * {
  margin-left: var(--space-2);
}

@media (max-width: 48rem) {
  .ledger thead {
    display: none;
  }

  .ledger tr {
    display: grid;
    gap: var(--space-2);
    padding-block: var(--space-4);
    border-bottom: 1px solid var(--rule);
  }

  .ledger td {
    padding: 0;
    border: none;
  }

  .actions {
    text-align: left;
  }
}
</style>
