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

// --- FR-108: pool ↔ router sync (Decision 48) -------------------------------

export interface PoolPushPlan {
  items: { action: string; path: string; command: string; current_state?: string }[]
  conflicts: { path: string; existing: string; reason: string }[]
  preview_hash: string
}

export interface PoolPushResult {
  results: { path: string; command: string; ok: boolean; error?: string }[]
  all_ok: boolean
  no_op: boolean
}

export interface AdoptedPool {
  name: string
  router_ranges: string
  adopted: boolean
  skipped_reason?: string
}

/** Preview reconciling a HikRAD-owned pool onto one NAS's router. Read-only. */
export function planPoolPush(nasId: string, poolId: string): Promise<PoolPushPlan> {
  return request<PoolPushPlan>(`/nas/${nasId}/pools/${poolId}/push/plan`, { method: 'POST' })
}

/** Apply a previewed push. Refused if the router drifted since the preview. */
export function applyPoolPush(
  nasId: string,
  poolId: string,
  previewHash: string,
): Promise<PoolPushResult> {
  return request<PoolPushResult>(`/nas/${nasId}/pools/${poolId}/push/apply`, {
    method: 'POST',
    body: { preview_hash: previewHash },
  })
}

/** Import a router's pools as read-only references. Never writes to the router. */
export function adoptRouterPools(nasId: string): Promise<{ items: AdoptedPool[] }> {
  return request<{ items: AdoptedPool[] }>(`/nas/${nasId}/pools/adopt`, {
    method: 'POST',
    body: { confirm: true },
  })
}
