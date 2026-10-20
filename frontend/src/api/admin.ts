import { request } from './client'
import type { AdminOverview, AdminUser, AdminUserPatch, AdminUsers, ResetLink } from './types'

export const getOverview = () => request<AdminOverview>('GET', '/api/admin/overview')

export const listUsers = (q = '') =>
  request<AdminUsers>('GET', q ? `/api/admin/users?q=${encodeURIComponent(q)}` : '/api/admin/users')

export const patchUser = (id: string, patch: AdminUserPatch) =>
  request<AdminUser>('PATCH', `/api/admin/users/${encodeURIComponent(id)}`, patch)

/** Ссылка сброса пароля, которую администратор передаёт сам. */
export const issueResetLink = (id: string) =>
  request<ResetLink>('POST', `/api/admin/users/${encodeURIComponent(id)}/password-reset`)
