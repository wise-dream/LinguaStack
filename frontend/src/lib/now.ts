import { onScopeDispose, ref } from 'vue'

/** Текущее время, обновляемое раз в секунду: для «идёт 2 ч 05 мин». */
export function useNow(intervalMs = 1000) {
  const now = ref(Date.now())
  const timer = setInterval(() => (now.value = Date.now()), intervalMs)
  onScopeDispose(() => clearInterval(timer))
  return now
}
