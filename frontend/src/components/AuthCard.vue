<script setup lang="ts">
// Каркас страниц без входа: название продукта, карточка с формой и строка
// со ссылками под ней.
defineProps<{ title: string }>()
</script>

<template>
  <main class="page auth">
    <p class="brand">
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
  </main>
</template>

<style scoped>
.auth {
  max-width: 28rem;
  padding-top: clamp(var(--space-6), 8vh, 5rem);
}

.brand {
  margin: 0 0 var(--space-4);
  text-align: center;
  font-family: var(--font-book);
  font-size: var(--step-1);
  font-weight: 600;
  letter-spacing: -0.01em;
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
  margin-top: var(--space-5);
  text-align: center;
  color: var(--muted);
}

@media (max-width: 30rem) {
  .card {
    padding: var(--space-5) var(--space-4);
  }
}
</style>
