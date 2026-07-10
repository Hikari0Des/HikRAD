/** IP pools REST client (contract C7-B). Utilization arrives in the list rows. */
import { request } from './client'
import type { Pool, PoolWrite } from './types'

export function listPools(): Promise<{ items: Pool[] }> {
  return request<{ items: Pool[] }>('/pools')
}

export function createPool(body: PoolWrite): Promise<Pool> {
  return request<Pool>('/pools', { method: 'POST', body })
}

export function updatePool(id: string, body: PoolWrite): Promise<Pool> {
  return request<Pool>(`/pools/${id}`, { method: 'PUT', body })
}

export function deletePool(id: string): Promise<void> {
  return request<void>(`/pools/${id}`, { method: 'DELETE' })
}
