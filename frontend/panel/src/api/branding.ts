/** Public branding read — contract C5 (`GET /api/v1/branding`) — plus the
 * v2 phase 11 (FR-91) logo upload/delete, which is admin-gated and must go
 * through multipart/form-data, not the generic JSON settings-group PUT
 * (which rejects a `logo_url` field in its body — see the phase's contract
 * C3). Self-contained fetch for the public read (no dependency on the
 * panel's own API client, matching the portal's equivalent) so this works
 * before login the same way the portal's does. */
import { tokenStore } from '../auth/tokenStore'
import { API_BASE, ApiError } from './client'

export interface Branding {
  name: string
  logo_url: string | null
  theme_color: string | null
  background_color: string | null
}

const FALLBACK: Branding = {
  name: 'HikRAD',
  logo_url: null,
  theme_color: null,
  background_color: null,
}

export async function getBranding(): Promise<Branding> {
  try {
    const res = await fetch(API_BASE + '/branding', { headers: { Accept: 'application/json' } })
    if (!res.ok) return FALLBACK
    return (await res.json()) as Branding
  } catch {
    // Offline-first (NFR-7): a missing/unreachable branding endpoint falls
    // back to the generic product identity rather than breaking the shell.
    return FALLBACK
  }
}

interface BrandingGroup {
  name?: string
  logo_url?: string | null
  primary_color?: string
  secondary_color?: string
}

export async function uploadBrandingLogo(file: File): Promise<BrandingGroup> {
  const form = new FormData()
  form.append('logo', file)
  const token = tokenStore.getAccessToken()
  const res = await fetch(API_BASE + '/settings/branding/logo', {
    method: 'POST',
    headers: token ? { Authorization: `Bearer ${token}` } : {},
    body: form,
  })
  const payload = (await res.json().catch(() => null)) as unknown
  if (!res.ok) {
    const err = payload as {
      error?: {
        code?: string
        message?: string
        field_errors?: { field: string; message: string }[]
      }
    }
    throw new ApiError(
      res.status,
      err?.error?.code ?? 'unknown',
      err?.error?.message ?? 'upload failed',
      err?.error?.field_errors ?? [],
    )
  }
  return payload as BrandingGroup
}

export async function deleteBrandingLogo(): Promise<BrandingGroup> {
  const token = tokenStore.getAccessToken()
  const res = await fetch(API_BASE + '/settings/branding/logo', {
    method: 'DELETE',
    headers: token ? { Authorization: `Bearer ${token}` } : {},
  })
  const payload = (await res.json().catch(() => null)) as unknown
  if (!res.ok) {
    const err = payload as { error?: { code?: string; message?: string } }
    throw new ApiError(
      res.status,
      err?.error?.code ?? 'unknown',
      err?.error?.message ?? 'delete failed',
    )
  }
  return payload as BrandingGroup
}
