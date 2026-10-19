/** Ошибка API с текстом, который можно показать пользователю. */
export class ApiError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

let unauthorized: (() => void) | null = null

/**
 * Обработчик истёкшей сессии: любой 401, кроме ответов /api/auth/*
 * (неверный пароль — это не истёкшая сессия).
 */
export function onUnauthorized(handler: (() => void) | null) {
  unauthorized = handler
}

/** Текст ошибки для показа: ApiError несёт его сам. */
export function errorMessage(e: unknown): string {
  return e instanceof ApiError ? e.message : 'Что-то пошло не так. Обновите страницу.'
}

/** Разбирает ответ fetch или XMLHttpRequest в результат или ApiError. */
export function parseResponse<T>(status: number, contentType: string | null, text: string, path: string): T {
  const json = contentType?.includes('application/json') && text !== '' ? safeJSON(text) : undefined
  if (status >= 200 && status < 300) {
    return (status === 204 ? undefined : json) as T
  }
  if (status === 401 && !path.startsWith('/api/auth/')) {
    unauthorized?.()
  }
  const message =
    json && typeof json === 'object' && 'error' in json && typeof json.error === 'string'
      ? json.error
      : `Сервер недоступен (HTTP ${status})`
  throw new ApiError(status, message)
}

function safeJSON(text: string): unknown {
  try {
    return JSON.parse(text)
  } catch {
    return undefined
  }
}

/**
 * Запрос к API своего origin с cookie сессии. keepalive — запрос
 * доживёт до конца, даже если страницу закрывают (позиция чтения).
 */
export async function request<T>(method: string, path: string, body?: unknown, opts: { keepalive?: boolean } = {}): Promise<T> {
  const isForm = body instanceof FormData
  const init: RequestInit = {
    method,
    credentials: 'same-origin',
    ...(opts.keepalive ? { keepalive: true } : {}),
    headers: body === undefined || isForm ? {} : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : isForm ? body : JSON.stringify(body),
  }
  let response: Response
  try {
    response = await fetch(path, init)
  } catch {
    throw new ApiError(0, 'Нет связи с сервером')
  }
  const text = response.status === 204 ? '' : await response.text()
  return parseResponse<T>(response.status, response.headers.get('Content-Type'), text, path)
}
