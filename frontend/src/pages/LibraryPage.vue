<script setup lang="ts">
// Библиотека (ТЗ §8.2): опись книг пользователя с переводами каждой —
// порциями и с поиском по названию и автору (дополнение §9).
import { NEmpty, NSpin, NTable } from 'naive-ui'

import { deleteBook } from '../api/books'
import { errorMessage } from '../api/client'
import type { LibraryBook } from '../api/types'
import ConfirmButton from '../components/ConfirmButton.vue'
import ErrorAlert from '../components/ErrorAlert.vue'
import LangPair from '../components/LangPair.vue'
import LibrarySearch from '../components/LibrarySearch.vue'
import LinkButton from '../components/LinkButton.vue'
import LoadMore from '../components/LoadMore.vue'
import StatusMark from '../components/StatusMark.vue'
import { formatDate, formatName } from '../lib/format'
import { useLibrary } from '../lib/library'

const { q, shown, books, total, next, loading, error, search, more, removed } = useLibrary(true)

async function remove(book: LibraryBook) {
  error.value = ''
  try {
    await deleteBook(book.id)
    removed(book.id)
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
        v-if="books?.length || shown"
        to="/import"
        type="primary"
      >
        Загрузить книгу
      </LinkButton>
    </div>

    <ErrorAlert :message="error" />

    <LibrarySearch
      v-if="books?.length || shown"
      v-model="q"
      :shown="shown"
      :total="total"
      @search="search"
    />

    <div
      v-if="books === null && !error"
      class="loading"
    >
      <NSpin size="small" />
      <span class="muted">Загружаю библиотеку…</span>
    </div>

    <p
      v-else-if="books?.length === 0 && shown"
      class="not-found muted"
    >
      Ничего не нашлось по запросу «{{ shown }}».
    </p>

    <section
      v-else-if="books?.length === 0"
      class="empty card"
    >
      <NEmpty size="large">
        <h2 class="empty-title">
          Загрузите первую книгу
        </h2>
        <template #extra>
          <p class="muted">
            Подойдут EPUB, PDF, Word, HTML, Markdown, обычный текст и ZIP-архив документации (mdBook, MkDocs).
          </p>
          <LinkButton
            to="/import"
            type="primary"
          >
            Загрузить книгу
          </LinkButton>
        </template>
      </NEmpty>
    </section>

    <div
      v-else-if="books"
      class="card table-card"
    >
      <NTable
        class="ledger"
        :bordered="false"
        :single-line="true"
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
                v-if="book.translations.length"
                class="translations"
              >
                <li
                  v-for="p in book.translations"
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
      </NTable>
    </div>

    <LoadMore
      v-if="next && books?.length"
      :count="books.length"
      :total="total"
      :loading="loading"
      @more="more"
    />
  </main>
</template>

<style scoped>
.loading {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.empty {
  padding: var(--space-7) var(--space-5);
}

.not-found {
  margin: 0;
}

.empty-title {
  font-family: var(--font-book);
  font-size: var(--step-2);
  font-weight: 600;
  color: var(--ink-original);
}

.empty :deep(.n-empty__extra) {
  display: grid;
  gap: var(--space-4);
  justify-items: center;
  text-align: center;
}

.empty p {
  margin: 0;
  max-width: var(--measure);
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
