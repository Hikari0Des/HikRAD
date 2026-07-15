/**
 * Platform surface (contract C4/C5/C7): settings groups (FR-53), license
 * lifecycle (FR-50), the first-run wizard (FR-49.3, unauthenticated), backup
 * status (FR-51) and system version (FR-52.4). Shapes mirror
 * backend/internal/platform/setupapi exactly.
 */
import { request } from './client'

// --- settings groups (FR-53) -------------------------------------------------

export type SettingsGroup =
  | 'locale'
  | 'branding'
  | 'notifications'
  | 'billing'
  | 'backups'
  | 'data_retention'
  | 'remote_access'
  /**
   * Card-payment types + rejection cooldown (Decision 22 amendment,
   * internal/billing/cardpay.go's `card_payments.*` keys). Not yet in
   * setupapi's settingsGroups allowlist as of this writing — see
   * docs/phases/phase-5-v1-reports-install-license/status-agent-4.md.
   */
  | 'card_payments'

export function getSettingsGroup<T = Record<string, unknown>>(group: SettingsGroup): Promise<T> {
  return request<T>(`/settings/${group}`)
}

export function putSettingsGroup<T = Record<string, unknown>>(
  group: SettingsGroup,
  body: Record<string, unknown>,
): Promise<T> {
  return request<T>(`/settings/${group}`, { method: 'PUT', body })
}

export type NotificationChannel = 'email' | 'telegram' | 'whatsapp'

export function testNotification(
  channel: NotificationChannel,
  recipient: string,
): Promise<{ ok: boolean; error?: string }> {
  return request('/settings/notifications/test', { method: 'POST', body: { channel, recipient } })
}

// --- license (contract C4, FR-50) -------------------------------------------

export type LicenseState = 'valid' | 'grace' | 'expired_grace'

export interface LicenseResponse {
  installed: boolean
  state: LicenseState | 'valid'
  key_id?: string
  licensee?: string
  tier?: string
  max_subscribers?: number
  entitled_version?: string
  issued_fingerprint?: string
  fingerprint: string
  grace_started_at?: string
  grace_expires_at?: string
}

export function getLicense(): Promise<LicenseResponse> {
  return request<LicenseResponse>('/license')
}

export function uploadLicense(payload: unknown, signature: string): Promise<LicenseResponse> {
  return request<LicenseResponse>('/license', { method: 'POST', body: { payload, signature } })
}

export interface RequestBlobResponse {
  current_key_id?: string
  fingerprint: string
  requested_at: string
}

export function requestLicenseBlob(): Promise<RequestBlobResponse> {
  return request<RequestBlobResponse>('/license/request-blob', { method: 'POST' })
}

// --- first-run wizard (FR-49.3, unauthenticated while no admin exists) -----

export interface SetupStatus {
  admin_exists: boolean
  license_installed: boolean
}

export function getSetupStatus(): Promise<SetupStatus> {
  return request<SetupStatus>('/setup/status')
}

export function getSetupLicense(): Promise<LicenseResponse> {
  return request<LicenseResponse>('/setup/license')
}

export function uploadSetupLicense(payload: unknown, signature: string): Promise<LicenseResponse> {
  return request<LicenseResponse>('/setup/license', {
    method: 'POST',
    body: { payload, signature },
  })
}

export interface SetupAdminResult {
  id: string
  username: string
  role: string
}

export function createSetupAdmin(username: string, password: string): Promise<SetupAdminResult> {
  return request<SetupAdminResult>('/setup/admin', { method: 'POST', body: { username, password } })
}

export function getSetupBranding(): Promise<Record<string, unknown>> {
  return request('/setup/branding')
}

export function putSetupBranding(body: Record<string, unknown>): Promise<Record<string, unknown>> {
  return request('/setup/branding', { method: 'POST', body })
}

// --- backups (FR-51, read-only) ---------------------------------------------

export interface BackupRun {
  id: number
  filename: string
  started_at: string
  finished_at?: string
  size_bytes?: number
  schema_version?: number
  encrypted: boolean
  status: string
  error?: string
  trigger: string
}

export function listBackups(): Promise<{ items: BackupRun[] }> {
  return request<{ items: BackupRun[] }>('/backups')
}

// --- system version (FR-52.4) ------------------------------------------------

export interface SystemVersion {
  app_version: string
  schema_version: number
  schema_dirty: boolean
  channel: string
}

export function getSystemVersion(): Promise<SystemVersion> {
  return request<SystemVersion>('/system/version')
}
