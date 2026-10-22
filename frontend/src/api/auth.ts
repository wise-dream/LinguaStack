import { request } from './client'
import type { User } from './types'

export const register = (email: string, password: string) =>
  request<User>('POST', '/api/auth/register', { email, password })

export const login = (email: string, password: string) =>
  request<User>('POST', '/api/auth/login', { email, password })

export const logout = () => request<void>('POST', '/api/auth/logout')

export const me = () => request<User>('GET', '/api/me')

/** Смена пароля: остальные сессии гаснут, текущая остаётся. */
export const changePassword = (currentPassword: string, newPassword: string) =>
  request<void>('POST', '/api/me/password', { current_password: currentPassword, new_password: newPassword })

export const changeEmail = (email: string, currentPassword: string) =>
  request<User>('PATCH', '/api/me', { email, current_password: currentPassword })

/** Удаление аккаунта со всеми данными; сессия гаснет на сервере. */
export const deleteAccount = (currentPassword: string) =>
  request<void>('DELETE', '/api/me', { current_password: currentPassword })

/** Ответ одинаков для любого адреса: аккаунт не выдаётся. */
export const requestPasswordReset = (email: string) =>
  request<{ message: string }>('POST', '/api/auth/password-reset', { email })

export const confirmPasswordReset = (token: string, password: string) =>
  request<void>('POST', '/api/auth/password-reset/confirm', { token, password })
