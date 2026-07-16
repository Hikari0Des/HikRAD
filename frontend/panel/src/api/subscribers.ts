/** Subscribers + bulk + search REST client (contract C7-D). */
import { API_BASE, listPage, request, type Page } from './client'
import { tokenStore } from '../auth/tokenStore'
import type {
  BulkFilter,
  BulkJob,
  BulkRequest,
  SearchHit,
  Subscriber,
  SubscriberDetail,
  SubscriberUpdateResult,
  SubscriberWrite,
} from './types'

export function listSubscribers(params: {
  cursor?: string
  limit?: number
}): Promise<Page<Subscriber>> {
  return listPage<Subscriber>('/subscribers', params)
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
