<script setup lang="ts">
// Переход по ссылке в виде кнопки Naive UI: настоящая <a href> (открывается
// в новой вкладке, видна читалкам экрана), навигация — через роутер.
import { NButton } from 'naive-ui'
import { RouterLink, type RouteLocationRaw } from 'vue-router'

defineOptions({ inheritAttrs: false })

withDefaults(
  defineProps<{
    to: RouteLocationRaw
    type?: 'default' | 'primary' | 'error' | 'tertiary'
    size?: 'tiny' | 'small' | 'medium' | 'large'
    secondary?: boolean
    quaternary?: boolean
    text?: boolean
  }>(),
  { type: 'default', size: 'medium' },
)
</script>

<template>
  <RouterLink
    v-slot="{ href, navigate }"
    :to="to"
    custom
  >
    <NButton
      tag="a"
      :href="href"
      :type="type"
      :size="size"
      :secondary="secondary"
      :quaternary="quaternary"
      :text="text"
      v-bind="$attrs"
      @click="navigate"
    >
      <slot />
    </NButton>
  </RouterLink>
</template>
