import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vitest/config'

// В разработке API отдаёт Go-сервер на :8000, а страницы — Vite: запросы к
// /api проксируются, и cookie сессии остаются на одном origin.
export default defineConfig({
  plugins: [vue()],
  server: {
    proxy: {
      '/api': { target: 'http://localhost:8000', changeOrigin: false },
    },
  },
  build: {
    target: 'es2022',
  },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.ts'],
    restoreMocks: true,
  },
})
