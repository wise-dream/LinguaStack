import { computed, inject, ref, watch, type InjectionKey } from 'vue'

export type ThemeChoice = 'system' | 'light' | 'dark'

// Ключ читает и public/theme-init.js: он ставит тему до загрузки
// приложения, чтобы тёмная страница не мигала светлой.
export const THEME_STORAGE_KEY = 'linguastack-theme'

/** Нужная часть MediaQueryList для (prefers-color-scheme: dark). */
export interface DarkQuery {
  readonly matches: boolean
  addEventListener(type: 'change', listener: (e: { matches: boolean }) => void): void
}

type ThemeStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>

export interface ThemeEnv {
  storage: ThemeStorage | null
  media: DarkQuery | null
  root: HTMLElement | null
}

function browserEnv(): ThemeEnv {
  let storage: ThemeStorage | null = null
  try {
    // Сам доступ к localStorage бросает, если сайту запрещено хранить данные.
    storage = window.localStorage
  } catch {
    storage = null
  }
  return {
    storage,
    media: typeof window.matchMedia === 'function' ? window.matchMedia('(prefers-color-scheme: dark)') : null,
    root: document.documentElement,
  }
}

function readChoice(storage: ThemeStorage | null): ThemeChoice {
  try {
    const v = storage?.getItem(THEME_STORAGE_KEY)
    return v === 'light' || v === 'dark' ? v : 'system'
  } catch {
    return 'system'
  }
}

// Тема: выбор человека (по умолчанию — как в системе) и вычисленная
// светлая или тёмная. Вычисленная стоит в data-theme на <html> — по нему
// переключаются токены tokens.css; Naive UI получает её из dark.
export function createTheme(env: ThemeEnv = browserEnv()) {
  const choice = ref<ThemeChoice>(readChoice(env.storage))
  const systemDark = ref(env.media?.matches ?? false)
  env.media?.addEventListener('change', (e) => {
    systemDark.value = e.matches
  })

  const dark = computed(() => choice.value === 'dark' || (choice.value === 'system' && systemDark.value))

  watch(
    dark,
    (isDark) => {
      if (env.root) env.root.dataset.theme = isDark ? 'dark' : 'light'
    },
    { immediate: true, flush: 'sync' },
  )

  function set(next: ThemeChoice) {
    choice.value = next
    try {
      if (next === 'system') env.storage?.removeItem(THEME_STORAGE_KEY)
      else env.storage?.setItem(THEME_STORAGE_KEY, next)
    } catch {
      // Не запомнили — тема всё равно сменилась до перезагрузки.
    }
  }

  return { choice: computed(() => choice.value), dark, set }
}

export type Theme = ReturnType<typeof createTheme>

export const THEME: InjectionKey<Theme> = Symbol('theme')

let appTheme: Theme | undefined

/** Тема приложения; тесты подставляют свою через provide(THEME). */
export function useTheme(): Theme {
  return inject(THEME, () => (appTheme ??= createTheme()), true)
}
