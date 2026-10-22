<script setup lang="ts">
// Смена модели перевода (дополнение §5): переведённое остаётся, дальше —
// новая модель; идущий перевод подхватывает её со следующих фрагментов.
import { NButton } from 'naive-ui'
import { computed, onMounted, reactive, ref } from 'vue'

import { errorMessage } from '../api/client'
import { changeTranslationModel } from '../api/translations'
import type { Project } from '../api/types'
import { useModelChoice } from '../lib/modelChoice'
import EgressNotice from './EgressNotice.vue'
import ErrorAlert from './ErrorAlert.vue'
import SelectField from './SelectField.vue'

const props = defineProps<{ project: Project }>()
const emit = defineEmits<{ changed: [project: Project]; cancel: [] }>()

const form = reactive({ provider_config_id: '', model_id: props.project.model_id })
const { configs, models, modelsError, config, configOptions, modelOptions, egress, loadConfigs } = useModelChoice(form)
const error = ref('')
const busy = ref(false)

const unchanged = computed(
  () => form.provider_config_id === props.project.provider_config_id && form.model_id === props.project.model_id,
)
const canSubmit = computed(() => !!form.model_id && egress.value !== 'blocked' && !unchanged.value && !busy.value)

onMounted(async () => {
  try {
    const list = await loadConfigs()
    form.provider_config_id = list.some((c) => c.id === props.project.provider_config_id)
      ? props.project.provider_config_id
      : (list[0]?.id ?? '')
  } catch (e) {
    error.value = errorMessage(e)
  }
})

async function submit() {
  if (!canSubmit.value) return
  busy.value = true
  error.value = ''
  try {
    emit('changed', await changeTranslationModel(props.project.id, { ...form }))
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <form
    class="model-change"
    @submit.prevent="submit"
  >
    <div class="pair">
      <SelectField
        v-model="form.provider_config_id"
        label="Подключение"
        name="provider_config_id"
        :options="configOptions"
        :disabled="!configs.length"
      />
      <SelectField
        v-model="form.model_id"
        label="Модель"
        name="model_id"
        :options="modelOptions"
        :disabled="!models.length"
        filterable
      />
    </div>
    <ErrorAlert :message="modelsError || error" />
    <EgressNotice
      :egress="egress"
      :name="config?.name"
    />
    <p class="hint">
      Переведённые фрагменты останутся как есть. Новая модель переведёт остальные и те, что вы отправите заново.
    </p>
    <div class="actions">
      <NButton
        attr-type="submit"
        type="primary"
        :loading="busy"
        :disabled="!canSubmit"
      >
        Сменить модель
      </NButton>
      <NButton @click="emit('cancel')">
        Отмена
      </NButton>
    </div>
  </form>
</template>

<style scoped>
.model-change {
  display: grid;
  gap: var(--space-3);
  padding: var(--space-4);
  border: 1px solid var(--rule-soft);
  border-radius: var(--radius);
  background: var(--paper-raised);
}

.pair {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 12rem), 1fr));
  gap: var(--space-3);
}

.hint {
  margin: 0;
  color: var(--muted);
  font-size: var(--step--1);
}

.actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
}
</style>
