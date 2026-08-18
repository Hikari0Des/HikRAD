/** Subscribers + bulk + search REST client (contract C7-D). */
import { API_BASE, listPage, request, type Page } from './client'
import { tokenStore } from '../auth/tokenStore'
import type {
  BulkFilter,
  BulkJob,
  BulkRequest,
  LimitProgress,
  SearchHit,
  Subscriber,
  SubscriberDetail,
  SubscriberUpdateResult,
  SubscriberWrite,
} from './types'

export interface SubscriberListParams {
  cursor?: string
  limit?: number
  // Server-side filters (mirror the FilterBar) so a search/filter matches across
  // ALL subscribers and paginates the matches, not just the loaded page.
  q?: string
  status?: string
  service_type?: string
  profile_id?: string
  owner_manager_id?: string
  expiring_days?: string
}

export function listSubscribers(params: SubscriberListParams): Promise<Page<Subscriber>> {
  const { cursor, limit, ...filters } = params
  return listPage<Subscriber>('/subscribers', { cursor, limit }, filters)
}

/** FR-135: the same list endpoint, also carrying the batched `limits` map. */
export interface SubscriberListPage extends Page<Subscriber> {
  limits: Record<string, LimitProgress>
}

export function listSubscribersWithLimits(
  params: SubscriberListParams,
): Promise<SubscriberListPage> {
  const { cursor, limit, ...filters } = params
  return request<SubscriberListPage>('/subscribers', { query: { ...filters, cursor, limit } })
}

export function getSubscriber(id: string): Promise<SubscriberDetail> {
  return request<SubscriberDetail>(`/subscribers/${id}`)
}

export function createSubscriber(body: SubscriberWrite): Promise<Subscriber> {
  return request<Subscriber>('/subscribers', { method: 'POST', body })
}

export function updateSubscriber(
  id: string,
  body: SubscriberWrite,
): Promise<SubscriberUpdateResult> {
  return request<SubscriberUpdateResult>(`/subscribers/${id}`, { method: 'PUT', body })
}

export function deleteSubscriber(id: string): Promise<void> {
  return request<void>(`/subscribers/${id}`, { method: 'DELETE' })
}

/** Reset MAC (FR-5.2): the one-click "customer changed router" fix. */
export function resetMac(id: string): Promise<Subscriber> {
  return request<Subscriber>(`/subscribers/${id}/reset-mac`, { method: 'POST' })
}

export function search(q: string, signal?: AbortSignal): Promise<{ items: SearchHit[] }> {
  return request<{ items: SearchHit[] }>('/search', { query: { q }, signal })
}

/** Kick off an async bulk mutation (FR-4). Returns the initial job snapshot. */
export function startBulk(body: BulkRequest): Promise<BulkJob> {
  return request<BulkJob>('/subscribers/bulk', { method: 'POST', body })
}

export function getBulkJob(id: string): Promise<BulkJob> {
  return request<BulkJob>(`/subscribers/bulk/${id}`)
}

// --- Batch / numbered creation (v4-5, FR-133) ------------------------------

export interface BatchInput {
  prefix: string
  start_index: number
  count: number
  digit_padding?: number
  password_mode: 'shared' | 'generated'
  shared_password?: string
  profile_id: string
  expires_at?: string | null
  auto_renew?: boolean
  owner_manager_id?: string
  service_type?: 'pppoe' | 'hotspot' | 'dual'
  invoicing?: 'each' | 'batch' | 'none'
  dry_run?: boolean
}

export interface BatchDryRunResult {
  usernames: string[]
  collisions: string[]
  total: number
  cap_blocked: boolean
  cap_message?: string
}

export interface BatchJob extends BulkJob {
  passwords?: Record<string, string>
}

export function dryRunBatch(body: BatchInput): Promise<BatchDryRunResult> {
  return request<BatchDryRunResult>('/subscribers/batch', {
    method: 'POST',
    body: { ...body, dry_run: true },
  })
}

export function startBatch(body: BatchInput): Promise<BatchJob> {
  return request<BatchJob>('/subscribers/batch', {
    method: 'POST',
    body: { ...body, dry_run: false },
  })
}

export function getBatchJob(id: string): Promise<BatchJob> {
  return request<BatchJob>(`/subscribers/batch/${id}`)
}

/**
 * CSV export (FR-4, export-gated). The endpoint streams `text/csv`, not JSON,
 * so we fetch the blob directly and hand back a saveable object URL + filename.
 */
/** Exports the ticked rows when ids are given, else the whole filter match. */
export async function exportCsv(
  filter: BulkFilter,
  subscriberIds?: string[],
): Promise<{ url: string; filename: string }> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }
  const token = tokenStore.getAccessToken()
  if (token) headers['Authorization'] = `Bearer ${token}`
  const res = await fetch(`${API_BASE}/subscribers/bulk`, {
    method: 'POST',
    headers,
    body: JSON.stringify({
      filter,
      subscriber_ids: subscriberIds?.length ? subscriberIds : undefined,
      action: 'export',
    }),
  })
  if (!res.ok) {
    throw new Error(`export failed (HTTP ${res.status})`)
  }
  const blob = await res.blob()
  return { url: URL.createObjectURL(blob), filename: 'subscribers.csv' }
}
