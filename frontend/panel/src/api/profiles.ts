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
  SpeedWindow,
  SpeedWindowWrite,
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

// --- v4-4 plan ergonomics (FR-132) -----------------------------------------

/** One-click duplication: copies the full plan (limits, scopes, speed
 * windows) into a new row named "<name> (copy)". */
export function duplicateProfile(id: string): Promise<Profile> {
  return request<Profile>(`/profiles/${id}/duplicate`, { method: 'POST' })
}

/** Bulk-propagates the plan's current auto_renew_default onto every
 * subscriber already on it — an explicit action, never a save side effect. */
export function propagateAutoRenew(id: string): Promise<{ subscribers_updated: number }> {
  return request<{ subscribers_updated: number }>(`/profiles/${id}/propagate-auto-renew`, {
    method: 'POST',
  })
}

// --- v4-4 speed windows (FR-131) -------------------------------------------

export function listSpeedWindows(profileId: string): Promise<{ items: SpeedWindow[] }> {
  return request<{ items: SpeedWindow[] }>(`/profiles/${profileId}/speed-windows`)
}

export function createSpeedWindow(
  profileId: string,
  body: SpeedWindowWrite,
): Promise<{ id: string }> {
  return request<{ id: string }>(`/profiles/${profileId}/speed-windows`, { method: 'POST', body })
}

export function deleteSpeedWindow(profileId: string, windowId: string): Promise<void> {
  return request<void>(`/profiles/${profileId}/speed-windows/${windowId}`, { method: 'DELETE' })
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
