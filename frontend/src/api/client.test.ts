import { afterEach, describe, expect, it, vi } from 'vitest'

import { ApiError, onUnauthorized, request } from './client'

function respond(status: number, body: unknown, contentType = 'application/json') {
  const text = typeof body === 'string' ? body : JSON.stringify(body)
  return vi.fn(async () => new Response(status === 204 ? null : text, { status, headers: { 'Content-Type': contentType } }))
}

afterEach(() => {
  onUnauthorized(null)
  vi.unstubAllGlobals()
})

describe('request', () => {
  it('отправляет JSON с cookie своего origin и разбирает ответ', async () => {
    const fetchMock = respond(200, { id: '1' })
    vi.stubGlobal('fetch', fetchMock)
    const got = await request<{ id: string }>('POST', '/api/books', { a: 1 })
    expect(got).toEqual({ id: '1' })
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit]
    expect(url).toBe('/api/books')
    expect(init.credentials).toBe('same-origin')
    expect(init.body).toBe('{"a":1}')
    expect((init.headers as Record<string, string>)['Content-Type']).toBe('application/json')
  })

  it('204 — без тела', async () => {
    vi.stubGlobal('fetch', respond(204, ''))
    expect(await request('DELETE', '/api/books/1')).toBeUndefined()
  })

  it('берёт текст ошибки из ответа сервера', async () => {
    vi.stubGlobal('fetch', respond(422, { error: 'Пароль должен содержать от 12 до 256 символов' }))
    const err = await request('POST', '/api/auth/register', {}).catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err).toMatchObject({ status: 422, message: 'Пароль должен содержать от 12 до 256 символов' })
  })

  it('ответ не от API (прокси, 502) — понятная ошибка', async () => {
    vi.stubGlobal('fetch', respond(502, '<html>Bad gateway</html>', 'text/html'))
    await expect(request('GET', '/api/books')).rejects.toMatchObject({ status: 502, message: 'Сервер недоступен (HTTP 502)' })
  })

  it('сеть недоступна — понятная ошибка', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => { throw new TypeError('Failed to fetch') }))
    await expect(request('GET', '/api/books')).rejects.toMatchObject({ status: 0, message: 'Нет связи с сервером' })
  })

  it('401 вызывает обработчик выхода, но не на входе', async () => {
    const handler = vi.fn()
    onUnauthorized(handler)
    vi.stubGlobal('fetch', respond(401, { error: 'Требуется вход' }))
    await request('GET', '/api/books').catch(() => undefined)
    expect(handler).toHaveBeenCalledTimes(1)
    await request('POST', '/api/auth/login', {}).catch(() => undefined)
    expect(handler).toHaveBeenCalledTimes(1)
  })

  it('FormData уходит без Content-Type — его ставит браузер с границей', async () => {
    const fetchMock = respond(201, { id: '1' })
    vi.stubGlobal('fetch', fetchMock)
    const form = new FormData()
    form.append('file', new Blob(['x']), 'a.txt')
    await request('POST', '/api/books', form)
    const [, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit]
    expect(init.body).toBe(form)
    expect(init.headers).toEqual({})
  })
})
