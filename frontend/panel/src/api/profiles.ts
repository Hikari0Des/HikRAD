/** Profiles REST client (contract C7-D). Non-paginated (small plan count). */
import { API_BASE, request } from './client'
import { tokenStore } from '../auth/tokenStore'
import type {
  Profile,
  ProfileBulkFilter,
  ProfileBulkJob,
  ProfileBulkRequest,
  ProfileUpdateResult,
  ProfileWrite,
} from './types'

export function listProfiles(includeArchived = false): Promise<{ items: Profile[] }> {
  return request<{ items: Profile[] }>('/profiles', {
    query: includeArchived ? { archived: 'true' } : undefined,
  })
}

export function createProfile(body: ProfileWrite): Promise<Profile> {
  return request<Profile>('/profiles', { method: 'POST', body })
}

/**
 * Update a profile. `apply=now` (default) pushes it to existing users at once
 * and returns the online sessions the operator may CoA-refresh;
 * `apply=next_renewal` only persists the row.
 */
export function updateProfile(
  id: string,
  body: ProfileWrite,
  apply: 'now' | 'next_renewal' = 'now',
): Promise<ProfileUpdateResult> {
  return request<ProfileUpdateResult>(`/profiles/${id}`, {
    method: 'PUT',
    body,
    query: apply === 'next_renewal' ? { apply: 'next_renewal' } : undefined,
  })
}

export function archiveProfile(id: string): Promise<Profile> {
  return request<Profile>(`/profiles/${id}/archive`, { method: 'POST' })
}

/**
 * Delete a plan. Only succeeds for one nothing has ever used — the backend
 * refuses anything sold with 409 `profile_in_use`, because its price/speeds are
 * what past renewals were sold at. Archive is the answer there.
 */
export function deleteProfile(id: string): Promise<void> {
  return request<void>(`/profiles/${id}`, { method: 'DELETE' })
}

// --- bulk (FR-111.2) ------------------------------------------------------

/** Kick off an async bulk plan mutation. Returns the initial job snapshot. */
export function startProfileBulk(body: ProfileBulkRequest): Promise<ProfileBulkJob> {
  return request<ProfileBulkJob>('/profiles/bulk', { method: 'POST', body })
}

export function getProfileBulkJob(id: string): Promise<ProfileBulkJob> {
  return request<ProfileBulkJob>(`/profiles/bulk/${id}`)
}

/**
 * CSV export of the ticked-or-filtered plans (export-gated). The endpoint
 * streams text/csv, so fetch the blob directly and hand back a saveable URL.
 */
export async function exportProfilesCsv(
  filter: ProfileBulkFilter,
  profileIds?: string[],
): Promise<{ url: string; filename: string }> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }
  const token = tokenStore.getAccessToken()
  if (token) headers['Authorization'] = `Bearer ${token}`
  const res = await fetch(`${API_BASE}/profiles/bulk`, {
    method: 'POST',
    headers,
    body: JSON.stringify({
      filter,
      profile_ids: profileIds?.length ? profileIds : undefined,
      action: 'export',
    }),
  })
  if (!res.ok) {
    throw new Error(`export failed (HTTP ${res.status})`)
  }
  const blob = await res.blob()
  return { url: URL.createObjectURL(blob), filename: 'profiles.csv' }
}
