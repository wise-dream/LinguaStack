import { request } from './client'
import type { Health, ModelCaps, ProviderConfig, ProviderConfigInput, ProviderKind } from './types'

const config = (id: string) => `/api/provider-configs/${encodeURIComponent(id)}`

export const listKinds = () => request<ProviderKind[]>('GET', '/api/providers')

export const listProviderConfigs = () => request<ProviderConfig[]>('GET', '/api/provider-configs')

export const createProviderConfig = (input: ProviderConfigInput) =>
  request<ProviderConfig>('POST', '/api/provider-configs', input)

export const patchProviderConfig = (id: string, input: { name?: string; allow_external?: boolean }) =>
  request<ProviderConfig>('PATCH', config(id), input)

export const deleteProviderConfig = (id: string) => request<void>('DELETE', config(id))

export const configModels = (id: string) => request<ModelCaps[]>('GET', `${config(id)}/models`)

export const configHealth = (id: string) => request<Health>('GET', `${config(id)}/health`)
