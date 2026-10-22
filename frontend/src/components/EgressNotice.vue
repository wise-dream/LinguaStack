<script setup lang="ts">
// Куда уйдёт текст книги (ТЗ §6.3–6.4): во внешний сервис — предупреждение,
// а если подключение это запрещает — что делать.
import { NAlert } from 'naive-ui'

defineProps<{ egress: 'allowed' | 'blocked' | null; name?: string }>()
</script>

<template>
  <NAlert
    v-if="egress === 'allowed'"
    class="egress"
    type="warning"
  >
    Текст книги будет отправлен в «{{ name }}» и покинет этот сервер.
  </NAlert>
  <NAlert
    v-else-if="egress === 'blocked'"
    class="egress blocked"
    type="error"
  >
    Подключение «{{ name }}» не разрешает отправку текста книг наружу. Разрешите её в разделе
    <RouterLink to="/settings/providers">
      «Модели»
    </RouterLink> или выберите локальную модель.
  </NAlert>
</template>
