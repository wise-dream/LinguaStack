import { request } from './client'
import type { User } from './types'

export const register = (email: string, password: string) =>
  request<User>('POST', '/api/auth/register', { email, password })

export const login = (email: string, password: string) =>
  request<User>('POST', '/api/auth/login', { email, password })

export const logout = () => request<void>('POST', '/api/auth/logout')

export const me = () => request<User>('GET', '/api/me')
