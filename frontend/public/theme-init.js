// Тема до первой отрисовки: пока грузится приложение, тёмная страница иначе
// мигнёт светлой. Отдельный файл, потому что встроенные скрипты запрещает
// CSP. Решает так же, как createTheme в src/theme.ts (тест сверяет).
;(function () {
  var choice = null
  try {
    choice = window.localStorage.getItem('linguastack-theme')
  } catch {
    choice = null
  }
  var systemDark = typeof window.matchMedia === 'function' && window.matchMedia('(prefers-color-scheme: dark)').matches
  var dark = choice === 'dark' || (choice !== 'light' && systemDark)
  document.documentElement.dataset.theme = dark ? 'dark' : 'light'
})()
