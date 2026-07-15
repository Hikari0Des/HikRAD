/** NAS registry + discovery + config-snippet REST client (contract C7-B). */
import { request } from './client'
import type {
  AutoSetupApplyResult,
  AutoSetupPreview,
  DiscoveredNas,
  Nas,
  NasSnippet,
  NasStatus,
  NasWrite,
} from './types'

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
 * Read version/board/identity from the router over its API (read-only; needs
 * saved API credentials) and refresh the stored ros_version (item 8).
 */
export function probeNas(
  id: string,
): Promise<{ ros_version: string; board_name: string; identity: string }> {
  return request(`/nas/${id}/probe`, { method: 'POST' })
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

/**
 * NAS API auto-setup (FR-56.2-56.4, contract C6). Preview connects read-only;
 * apply refuses unless the router's state hashes to the same preview_hash
 * (recomputed server-side) and unless the NAS's ROS version has a green
 * matrix leg — see docs/ops/ros-matrix.md.
 */
export function previewAutoSetup(id: string): Promise<AutoSetupPreview> {
  return request<AutoSetupPreview>(`/nas/${id}/auto-setup/preview`, { method: 'POST' })
}

export function applyAutoSetup(id: string, previewHash: string): Promise<AutoSetupApplyResult> {
  return request<AutoSetupApplyResult>(`/nas/${id}/auto-setup/apply`, {
    method: 'POST',
    body: { preview_hash: previewHash },
  })
}
