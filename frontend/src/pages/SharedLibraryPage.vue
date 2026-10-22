<script setup lang="ts">
// Общая библиотека (дополнение §9): книги всех пользователей с их
// переводами — читать и скачивать, порциями и с поиском по названию и
// автору. Менять может только тот, кто загрузил.
import { NButton, NEmpty } from 'naive-ui'
import { ref } from 'vue'

import ErrorAlert from '../components/ErrorAlert.vue'
import ExportPanel from '../components/ExportPanel.vue'
import LangPair from '../components/LangPair.vue'
import LibrarySearch from '../components/LibrarySearch.vue'
import LinkButton from '../components/LinkButton.vue'
import LoadMore from '../components/LoadMore.vue'
import StatusMark from '../components/StatusMark.vue'
import { formatName, formatNumber } from '../lib/format'
import { useLibrary } from '../lib/library'

const { q, shown, books, total, next, loading, error, search, more } = useLibrary(false)
/** Перевод, у которого раскрыта панель скачивания. */
const downloading = ref('')

function toggle(id: string) {
  downloading.value = downloading.value === id ? '' : id
}
</script>

<template>
  <main class="page shared">
    <h1>Общая библиотека</h1>
    <ErrorAlert :message="error" />

    <LibrarySearch
      v-if="books?.length || shown"
      v-model="q"
      :shown="shown"
      :total="total"
      @search="search"
    />

    <p
      v-if="books && !books.length && shown"
      class="muted"
    >
      Ничего не нашлось по запросу «{{ shown }}».
    </p>
    <section
      v-else-if="books && !books.length"
      class="empty card"
    >
      <NEmpty description="Книг пока нет." />
    </section>

    <div class="books">
      <section
        v-for="b in books ?? []"
        :key="b.id"
        :data-book="b.id"
        class="book card"
      >
        <header class="book-head">
          <h2>{{ b.title }}</h2>
          <span
            v-if="b.owned"
            class="mine"
          >ваша</span>
        </header>
        <p class="meta">
          <span v-if="b.author">{{ b.author }}</span>
          <span>{{ formatName(b.source_format) }}</span>
        </p>

        <ul
          v-if="b.translations.length"
          class="translations"
        >
          <li
            v-for="t in b.translations"
            :key="t.id"
            :data-translation="t.id"
          >
            <div class="tr-head">
              <LangPair
                :source="t.source_lang"
                :target="t.target_lang"
              />
              <StatusMark :status="t.status" />
            </div>
            <p class="tr-facts">
              <span>{{ t.model_id }}</span>
              <span>{{ formatNumber(t.segments_done) }} из {{ formatNumber(t.segments_total) }}</span>
            </p>
            <div class="tr-actions">
              <LinkButton
                :to="`/read/${b.id}?t=${t.id}`"
                type="primary"
                size="small"
              >
                Читать
              </LinkButton>
              <NButton
                class="download"
                size="small"
                :aria-expanded="downloading === t.id"
                @click="toggle(t.id)"
              >
                Скачать
              </NButton>
            </div>
            <ExportPanel
              v-if="downloading === t.id"
              :book-id="b.id"
              :translation-id="t.id"
            />
          </li>
        </ul>
        <LinkButton
          v-else
          class="read-original"
          :to="`/read/${b.id}`"
          size="small"
        >
          Читать оригинал
        </LinkButton>
      </section>
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
.books {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 30rem), 1fr));
  gap: var(--space-5);
  align-items: start;
}

.book {
  display: grid;
  gap: var(--space-3);
  align-content: start;
}

.book-head {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: var(--space-2) var(--space-3);
}

.book-head h2 {
  margin: 0;
  font-family: var(--font-book);
  font-size: var(--step-2);
  line-height: 1.25;
}

.mine {
  padding: 0 var(--space-2);
  border-radius: 999px;
  background: var(--paper);
  color: var(--muted);
  font-size: var(--step--1);
}

.meta {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-1) var(--space-4);
  margin: calc(-1 * var(--space-2)) 0 0;
  color: var(--muted);
}

.translations {
  display: grid;
  margin: 0;
  padding: 0;
  list-style: none;
}

.translations > li {
  display: grid;
  gap: var(--space-2);
  padding-block: var(--space-3);
  border-top: 1px solid var(--rule-soft);
}

.tr-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2) var(--space-3);
}

.tr-facts {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-1) var(--space-4);
  margin: 0;
  font-size: var(--step--1);
  color: var(--muted);
}

.tr-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
}

.read-original {
  justify-self: start;
}

.empty {
  padding-block: var(--space-6);
}
</style>
