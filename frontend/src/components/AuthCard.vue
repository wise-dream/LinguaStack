<script setup lang="ts">
// Каркас страниц без входа. На широком экране слева — что это за продукт и
// разворот двуязычной книги (оригинал и перевод двумя красками, как в
// читалке), справа — форма. На телефоне остаётся одна форма.
defineProps<{ title: string }>()
</script>

<template>
  <main class="page auth">
    <aside
      class="intro"
      aria-label="О LinguaStack"
    >
      <p class="brand">
        LinguaStack
      </p>
      <p class="lead">
        Переводите книги и документацию локальными и облачными моделями и читайте оригинал рядом с переводом.
      </p>
      <figure
        class="spread"
        aria-hidden="true"
      >
        <div class="column original">
          <p class="label">
            Оригинал
          </p>
          <p>It was the best of times, it was the worst of times, it was the age of wisdom, it was the age of foolishness…</p>
        </div>
        <div class="column translation">
          <p class="label">
            Перевод
          </p>
          <p>Это было лучшее из времён, это было худшее из времён; то был век мудрости, то был век безрассудства…</p>
        </div>
      </figure>
      <ul class="points">
        <li>EPUB, PDF, Word, HTML, Markdown и документация</li>
        <li>Ollama на сервере или свой ключ Anthropic и OpenAI-совместимых сервисов</li>
        <li>Экспорт перевода и уведомления в Telegram</li>
      </ul>
    </aside>

    <div class="form-column">
      <p class="brand mobile-brand">
        LinguaStack
      </p>
      <div class="card">
        <h1>{{ title }}</h1>
        <slot />
      </div>
      <p
        v-if="$slots.footer"
        class="switch"
      >
        <slot name="footer" />
      </p>
    </div>
  </main>
</template>

<style scoped>
.auth {
  display: grid;
  grid-template-columns: minmax(0, 1.2fr) minmax(0, 28rem);
  gap: clamp(var(--space-6), 6vw, 7rem);
  align-items: center;
  min-height: calc(100vh - var(--space-7));
  /* Шапки здесь нет, равняться не на что: на широком экране колонки
     не разъезжаются к краям. */
  max-width: 80rem;
  margin-inline: auto;
}

.brand {
  margin: 0;
  font-family: var(--font-book);
  font-size: var(--step-1);
  font-weight: 600;
  letter-spacing: -0.01em;
}

.intro {
  display: grid;
  gap: var(--space-5);
  max-width: 40rem;
}

.lead {
  margin: 0;
  font-family: var(--font-book);
  font-size: var(--step-2);
  line-height: 1.35;
  color: var(--ink-original);
}

/* Разворот: две колонки текста одной книги, краска оригинала и перевода. */
.spread {
  margin: 0;
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: var(--space-5);
  padding: var(--space-5);
  background: var(--paper-raised);
  border: 1px solid var(--rule-soft);
  border-radius: var(--radius-card);
}

.column p {
  margin: 0;
  font-family: var(--font-book);
  font-size: var(--book-size);
  line-height: var(--book-leading);
}

.column .label {
  margin-bottom: var(--space-2);
  font-family: var(--font-ui);
  font-size: var(--step--1);
  line-height: 1.4;
  color: var(--muted);
}

.original p:not(.label) {
  color: var(--ink-original);
}

.translation p:not(.label) {
  color: var(--ink-translation);
}

.translation {
  padding-left: var(--space-5);
  border-left: 1px solid var(--rule-soft);
}

.points {
  margin: 0;
  padding-left: 1.1em;
  display: grid;
  gap: var(--space-1);
  color: var(--muted);
}

.form-column {
  display: grid;
  gap: var(--space-4);
}

.mobile-brand {
  display: none;
  text-align: center;
}

.card {
  padding: var(--space-6);
  display: grid;
  gap: var(--space-4);
}

h1 {
  font-family: var(--font-book);
  font-size: var(--step-2);
  font-weight: 600;
}

.switch {
  margin: 0;
  text-align: center;
  color: var(--muted);
}

@media (max-width: 60rem) {
  .auth {
    grid-template-columns: minmax(0, 28rem);
    justify-content: center;
    align-items: start;
    min-height: 0;
    padding-top: clamp(var(--space-6), 8vh, 5rem);
  }

  .intro {
    display: none;
  }

  .mobile-brand {
    display: block;
  }
}

@media (max-width: 30rem) {
  .card {
    padding: var(--space-5) var(--space-4);
  }
}
</style>
