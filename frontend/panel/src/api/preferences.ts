/** Per-manager preferences — contract C2/C3 (v2-6, FR-84.2). Self-only: the
 * route carries no id, so this always reads/writes the caller's own row. */
import { request } from './client'

export interface NotifChannels {
  in_app: boolean
  push: boolean
}

/** v2-10 contract C2: one more optional key, no schema version bump. */
export interface DashboardWidgetRef {
  id: string
  size: '1x' | '2x'
}

export interface DashboardLayout {
  widgets: DashboardWidgetRef[]
}

export interface Preferences {
  language?: '' | 'en' | 'ar' | 'ku'
  theme?: '' | 'light' | 'dark' | 'system'
  numerals?: '' | 'auto' | 'latn' | 'arab'
  landing_page?: string
  table_page_size?: number
  notification_prefs?: Record<string, NotifChannels>
  /** Absent/undefined = default layout; present (even empty) = an explicit
   * choice. Full-document PUT semantics apply — see putPreferences. */
  dashboard_layout?: DashboardLayout
}

export function getPreferences(): Promise<Preferences> {
  return request<Preferences>('/me/preferences')
}

export function putPreferences(body: Preferences): Promise<Preferences> {
  return request<Preferences>('/me/preferences', { method: 'PUT', body })
}
