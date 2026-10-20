import { createRouter, createWebHistory, type RouterHistory } from 'vue-router'

import LoginPage from './pages/LoginPage.vue'

/** Раздел навигации, к которому относится страница. */
export type Section = 'library' | 'translations' | 'providers' | 'settings' | 'admin'

declare module 'vue-router' {
  interface RouteMeta {
    title: string
    section?: Section
    /** Страница доступна без входа. */
    public?: boolean
  }
}

// Страницы грузятся по требованию: вход открывается сразу, а кит Naive UI
// и тяжёлые страницы (читалка, админ-панель) — только когда нужны.
export function createAppRouter(history: RouterHistory = createWebHistory()) {
  return createRouter({
    history,
    routes: [
      { path: '/login', component: LoginPage, meta: { title: 'Вход', public: true } },
      { path: '/register', component: () => import('./pages/RegisterPage.vue'), meta: { title: 'Регистрация', public: true } },
      { path: '/forgot-password', component: () => import('./pages/ForgotPasswordPage.vue'), meta: { title: 'Восстановление доступа', public: true } },
      { path: '/reset-password', component: () => import('./pages/ResetPasswordPage.vue'), meta: { title: 'Новый пароль', public: true } },
      { path: '/', component: () => import('./pages/LibraryPage.vue'), meta: { title: 'Библиотека', section: 'library' } },
      { path: '/import', component: () => import('./pages/ImportPage.vue'), meta: { title: 'Загрузка книги', section: 'library' } },
      { path: '/books/:id', component: () => import('./pages/BookPage.vue'), meta: { title: 'Книга', section: 'library' } },
      { path: '/books/:id/translate', component: () => import('./pages/TranslationSetupPage.vue'), meta: { title: 'Новый перевод', section: 'library' } },
      { path: '/translations', component: () => import('./pages/TranslationsPage.vue'), meta: { title: 'Переводы', section: 'translations' } },
      { path: '/translations/:id', component: () => import('./pages/TranslationDetailPage.vue'), meta: { title: 'Перевод', section: 'translations' } },
      { path: '/read/:bookId', component: () => import('./pages/ReaderPage.vue'), meta: { title: 'Чтение', section: 'library' } },
      { path: '/settings', component: () => import('./pages/SettingsPage.vue'), meta: { title: 'Настройки', section: 'settings' } },
      { path: '/settings/providers', component: () => import('./pages/ProvidersPage.vue'), meta: { title: 'Модели', section: 'providers' } },
      { path: '/admin', component: () => import('./pages/AdminPage.vue'), meta: { title: 'Администрирование', section: 'admin' } },
      { path: '/:pathMatch(.*)*', component: () => import('./pages/PendingPage.vue'), meta: { title: 'Страница не найдена' } },
    ],
  })
}
