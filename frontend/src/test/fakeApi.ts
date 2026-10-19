// Тестовая подмена API: ответы по «МЕТОД путь» (путь без query). Запросы
// записываются, чтобы проверять, что ушло на сервер.
import { vi } from 'vitest'

export interface FakeCall {
  method: string
  path: string
  url: string
  body: unknown
}

type Reply = { status?: number; body?: unknown } | ((call: FakeCall) => { status?: number; body?: unknown })

export function stubApi(routes: Record<string, Reply>) {
  const calls: FakeCall[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init: RequestInit = {}) => {
      const method = (init.method ?? 'GET').toUpperCase()
      const path = url.split('?')[0] ?? url
      const body = typeof init.body === 'string' ? JSON.parse(init.body) : init.body
      const call = { method, path, url, body }
      calls.push(call)
      const route = routes[`${method} ${path}`]
      if (!route) {
        return new Response(JSON.stringify({ error: `нет заглушки для ${method} ${path}` }), {
          status: 599,
          headers: { 'Content-Type': 'application/json' },
        })
      }
      const reply = typeof route === 'function' ? route(call) : route
      const status = reply.status ?? 200
      return new Response(status === 204 ? null : JSON.stringify(reply.body ?? null), {
        status,
        headers: { 'Content-Type': 'application/json' },
      })
    }),
  )
  return calls
}
