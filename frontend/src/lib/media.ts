import { getCurrentInstance, onBeforeUnmount, ref, type Ref } from 'vue'

/**
 * Совпадает ли медиазапрос — и следит за изменением (поворот экрана, окно).
 * Без matchMedia (старый браузер, тесты) — false.
 */
export function useMediaQuery(query: string): Ref<boolean> {
  const matches = ref(false)
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return matches
  const mql = window.matchMedia(query)
  matches.value = mql.matches
  const update = (e: { matches: boolean }) => {
    matches.value = e.matches
  }
  mql.addEventListener?.('change', update)
  if (getCurrentInstance()) onBeforeUnmount(() => mql.removeEventListener?.('change', update))
  return matches
}
