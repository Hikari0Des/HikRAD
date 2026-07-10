/** Profiles REST client (contract C7-D). Non-paginated (small plan count). */
import { request } from './client'
import type { Profile, ProfileUpdateResult, ProfileWrite } from './types'

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
