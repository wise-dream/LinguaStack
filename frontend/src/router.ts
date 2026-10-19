import { createRouter, createWebHistory, type RouterHistory } from 'vue-router'

import ImportPage from './pages/ImportPage.vue'
import LibraryPage from './pages/LibraryPage.vue'
import LoginPage from './pages/LoginPage.vue'
import PendingPage from './pages/PendingPage.vue'
import RegisterPage from './pages/RegisterPage.vue'

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
      { path: '/books/:id', component: PendingPage, meta: { title: 'Книга', section: 'library' } },
      { path: '/books/:id/translate', component: PendingPage, meta: { title: 'Новый перевод', section: 'library' } },
      { path: '/translations', component: PendingPage, meta: { title: 'Переводы', section: 'translations' } },
      { path: '/translations/:id', component: PendingPage, meta: { title: 'Перевод', section: 'translations' } },
      { path: '/read/:bookId', component: PendingPage, meta: { title: 'Чтение', section: 'library' } },
      { path: '/settings/providers', component: PendingPage, meta: { title: 'Модели', section: 'providers' } },
      { path: '/:pathMatch(.*)*', component: PendingPage, meta: { title: 'Страница не найдена' } },
    ],
  })
}
