<script setup lang="ts">
// Библиотека (ТЗ §8.2): опись книг пользователя с переводами каждой.
import { computed, onMounted, ref } from 'vue'

import { deleteBook, listBooks } from '../api/books'
import { errorMessage } from '../api/client'
import { listTranslations } from '../api/translations'
import type { Book, Project } from '../api/types'
import ConfirmButton from '../components/ConfirmButton.vue'
import LangPair from '../components/LangPair.vue'
import LinkButton from '../components/LinkButton.vue'
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
    <div class="page-head">
      <h1>Библиотека</h1>
      <LinkButton
        v-if="books?.length"
        to="/import"
        type="primary"
      >
        Загрузить книгу
      </LinkButton>
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
      class="empty card"
    >
      <h2>Загрузите первую книгу</h2>
      <p>Подойдут EPUB, PDF, Word, HTML, Markdown, обычный текст и ZIP-архив документации (mdBook, MkDocs).</p>
      <LinkButton
        to="/import"
        type="primary"
      >
        Загрузить книгу
      </LinkButton>
    </section>

    <div
      v-else-if="books"
      class="card table-card"
    >
      <table class="ledger">
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
              <LinkButton
                :to="`/read/${book.id}`"
                size="small"
              >
                Читать
              </LinkButton>
              <LinkButton
                :to="`/books/${book.id}/translate`"
                size="small"
              >
                Перевести
              </LinkButton>
              <ConfirmButton
                label="Удалить"
                size="small"
                @confirm="remove(book)"
              />
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </main>
</template>

<style scoped>
.empty {
  max-width: var(--measure);
  display: grid;
  gap: var(--space-3);
  justify-items: start;
  padding: var(--space-6);
}

.empty h2 {
  font-family: var(--font-book);
  font-size: var(--step-2);
  font-weight: 600;
}

.empty p {
  margin: 0 0 var(--space-2);
  color: var(--muted);
}

.table-card {
  padding: 0;
  overflow: hidden;
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
  padding: var(--space-3) var(--space-4);
  border-bottom: 1px solid var(--rule-soft);
}

.ledger td {
  vertical-align: middle;
  padding: var(--space-3) var(--space-4);
  border-bottom: 1px solid var(--rule-soft);
}

.ledger tbody tr:last-child td {
  border-bottom: none;
}

.ledger tbody tr:hover {
  background: var(--paper-raised);
}

.book .title {
  display: block;
  font-family: var(--font-book);
  font-size: var(--step-1);
  font-weight: 500;
  text-decoration: none;
}

.book .title:hover {
  text-decoration: underline;
}

.author {
  color: var(--muted);
  font-size: var(--step--1);
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
  align-items: center;
  gap: var(--space-1) var(--space-2);
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
    padding: var(--space-4);
    border-bottom: 1px solid var(--rule-soft);
  }

  .ledger tbody tr:last-child {
    border-bottom: none;
  }

  .ledger td {
    padding: 0;
    border: none;
  }

  .actions {
    text-align: left;
    white-space: normal;
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }

  .actions > * + * {
    margin-left: 0;
  }
}
</style>
