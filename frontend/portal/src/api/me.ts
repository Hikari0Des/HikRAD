/**
 * Portal self-service API — contract C2. `GET /portal/me` deliberately never
 * carries a quota total or remaining figure (Decision 21) — do not add one
 * even if a future backend response includes it.
 */
import { request } from './client'

export type SubscriberStatus = 'active' | 'expired' | 'disabled'

export interface PortalMe {
  status: SubscriberStatus
  online_now: boolean
  // Nullable: verified against backend/internal/portalapi/me.go
  // (`ExpiresAt *time.Time`) — a subscriber can have no expiry set yet.
  expires_at: string | null
  days_left: number
  usage: {
    used_down: number
    used_up: number
    used_total: number
  }
  speed: {
    profile_down: number
    profile_up: number
    live_down?: number
    live_up?: number
  }
  profile_name: string
}

export function getMe(): Promise<PortalMe> {
  return request<PortalMe>('/portal/me')
}

export interface UpdateMeBody {
  name?: string
  phone?: string
  email?: string
  password?: string
}

/** FR-44 self-update. A `password` change re-encrypts server-side and
 * invalidates the cached RADIUS policy — the PPPoE credential changes too. */
export function updateMe(body: UpdateMeBody): Promise<PortalMe> {
  return request<PortalMe>('/portal/me', { method: 'PUT', body })
}

export function setLanguage(language: 'en' | 'ar' | 'ku'): Promise<void> {
  return request<void>('/portal/language', { method: 'PUT', body: { language } })
}
