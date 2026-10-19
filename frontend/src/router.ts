import { createRouter, createWebHistory, type RouterHistory } from 'vue-router'

import BookPage from './pages/BookPage.vue'
import ImportPage from './pages/ImportPage.vue'
import LibraryPage from './pages/LibraryPage.vue'
import LoginPage from './pages/LoginPage.vue'
import PendingPage from './pages/PendingPage.vue'
import ProvidersPage from './pages/ProvidersPage.vue'
import ReaderPage from './pages/ReaderPage.vue'
import RegisterPage from './pages/RegisterPage.vue'
import TranslationDetailPage from './pages/TranslationDetailPage.vue'
import TranslationSetupPage from './pages/TranslationSetupPage.vue'
import TranslationsPage from './pages/TranslationsPage.vue'

/** Раздел навигации, к которому относится страница. */
export type Section = 'library' | 'translations' | 'providers'

declare module 'vue-router' {
  interface RouteMeta {
    title: string
    section?: Section
    /** Страница доступна без входа. */
    public?: boolean
  }
}

export function createAppRouter(history: RouterHistory = createWebHistory()) {
  return createRouter({
    history,
    routes: [
      { path: '/login', component: LoginPage, meta: { title: 'Вход', public: true } },
      { path: '/register', component: RegisterPage, meta: { title: 'Регистрация', public: true } },
      { path: '/', component: LibraryPage, meta: { title: 'Библиотека', section: 'library' } },
      { path: '/import', component: ImportPage, meta: { title: 'Загрузка книги', section: 'library' } },
      { path: '/books/:id', component: BookPage, meta: { title: 'Книга', section: 'library' } },
      { path: '/books/:id/translate', component: TranslationSetupPage, meta: { title: 'Новый перевод', section: 'library' } },
      { path: '/translations', component: TranslationsPage, meta: { title: 'Переводы', section: 'translations' } },
      { path: '/translations/:id', component: TranslationDetailPage, meta: { title: 'Перевод', section: 'translations' } },
      { path: '/read/:bookId', component: ReaderPage, meta: { title: 'Чтение', section: 'library' } },
      { path: '/settings/providers', component: ProvidersPage, meta: { title: 'Модели', section: 'providers' } },
      { path: '/:pathMatch(.*)*', component: PendingPage, meta: { title: 'Страница не найдена' } },
    ],
  })
}
