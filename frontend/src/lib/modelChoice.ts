// Выбор подключения и модели: модели подключения и передача текста наружу
// (ТЗ §6.4). Общий для нового перевода и смены модели идущего (дополнение §5).
import { computed, ref, watch } from 'vue'

import { errorMessage } from '../api/client'
import { configModels, listProviderConfigs } from '../api/providers'
import type { ModelCaps, ProviderConfig } from '../api/types'

export function useModelChoice(form: { provider_config_id: string; model_id: string }) {
  const configs = ref<ProviderConfig[]>([])
  const models = ref<ModelCaps[]>([])
  const modelsError = ref('')

  const config = computed(() => configs.value.find((c) => c.id === form.provider_config_id))
  const model = computed(() => models.value.find((m) => m.model_id === form.model_id))
  const configOptions = computed(() => configs.value.map((c) => ({ label: c.name, value: c.id })))
  const modelOptions = computed(() => models.value.map((m) => ({ label: m.display_name || m.model_id, value: m.model_id })))

  /** null — текст остаётся на сервере; blocked — подключение запрещает отправку наружу. */
  const egress = computed(() => {
    if (!config.value?.sends_data_externally) return null
    return config.value.allow_external ? 'allowed' : 'blocked'
  })

  async function loadConfigs(): Promise<ProviderConfig[]> {
    configs.value = await listProviderConfigs()
    return configs.value
  }

  // Модель остаётся прежней, если новое подключение её предлагает, иначе —
  // первая из его списка. Ответ по уже сменённому подключению не нужен.
  watch(
    () => form.provider_config_id,
    async (id) => {
      const keep = form.model_id
      models.value = []
      form.model_id = ''
      modelsError.value = ''
      if (!id) return
      try {
        const list = await configModels(id)
        if (form.provider_config_id !== id) return
        models.value = list
        form.model_id = list.some((m) => m.model_id === keep) ? keep : (list[0]?.model_id ?? '')
        if (!list.length) modelsError.value = 'У этого подключения нет доступных моделей.'
      } catch (e) {
        modelsError.value = errorMessage(e)
      }
    },
  )

  return { configs, models, modelsError, config, model, configOptions, modelOptions, egress, loadConfigs }
}
