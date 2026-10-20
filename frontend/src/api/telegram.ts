import { request } from './client'
import type { TelegramLink, TelegramSettings, TelegramState } from './types'

export const getTelegram = () => request<TelegramState>('GET', '/api/telegram')

/** Ссылка t.me/<бот>?start=<код>: новый код заменяет прежний. */
export const linkTelegram = () => request<TelegramLink>('POST', '/api/telegram/link')

export const patchTelegram = (s: TelegramSettings) => request<TelegramState>('PATCH', '/api/telegram', s)

export const unlinkTelegram = () => request<void>('DELETE', '/api/telegram')
