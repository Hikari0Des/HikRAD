/** NAS registry + discovery + config-snippet REST client (contract C7-B). */
import { request } from './client'
import type { DiscoveredNas, Nas, NasSnippet, NasStatus, NasWrite } from './types'

export function listNas(): Promise<{ items: Nas[] }> {
  return request<{ items: Nas[] }>('/nas')
}

export function getNas(id: string): Promise<Nas> {
  return request<Nas>(`/nas/${id}`)
}

export function createNas(body: NasWrite): Promise<Nas> {
  return request<Nas>('/nas', { method: 'POST', body })
}

export function updateNas(id: string, body: NasWrite): Promise<Nas> {
  return request<Nas>(`/nas/${id}`, { method: 'PUT', body })
}

/**
 * Delete a NAS. The server refuses (409 confirmation_required) when it has live
 * sessions unless `confirm` is set; retrying with confirm deletes and marks
 * those sessions stale (FR-13.4).
 */
export function deleteNas(id: string, confirm = false): Promise<void> {
  return request<void>(`/nas/${id}`, {
    method: 'DELETE',
    query: confirm ? { confirm: 'true' } : undefined,
  })
}

export function nasSnippet(id: string, ros: '6' | '7'): Promise<NasSnippet> {
  return request<NasSnippet>(`/nas/${id}/config-snippet`, { query: { ros } })
}

/** FR-14.4 "seen since created" check for the wizard's Test button. */
export function nasStatus(id: string): Promise<NasStatus> {
  return request<NasStatus>(`/nas/${id}/status`)
}

/**
 * NAS auto-discovery (FR-56.1): passive MNDP listen (+ optional range scan).
 * Read-only — it never touches a router. `mndp_wait_ms` bounds the listen.
 */
export function discoverNas(body?: {
  mndp_wait_ms?: number
  scan_cidr?: string
}): Promise<{ items: DiscoveredNas[] }> {
  return request<{ items: DiscoveredNas[] }>('/nas/discover', { method: 'POST', body: body ?? {} })
}
