/**
 * Security API (contract C7): roles + permission matrix, per-manager overrides
 * and IP allowlists, TOTP self-service, panel sessions, and the audit-log
 * reader. Shapes mirror internal/auth.
 */
import { API_BASE, request } from './client'
import { tokenStore } from '../auth/tokenStore'

// --- roles + permission catalog (FR-27.1) -----------------------------------

export interface Role {
  id: string
  name: string
  description: string
  is_builtin: boolean
  require_2fa: boolean
  permissions: string[]
  member_count: number
}

export interface RoleWrite {
  name: string
  description?: string
  require_2fa?: boolean
  permissions: string[]
}

export interface PermissionGroup {
  module: string
  permissions: string[]
}

export function listRoles(): Promise<{ items: Role[] }> {
  return request<{ items: Role[] }>('/roles')
}

export function getPermissionCatalog(): Promise<{ modules: PermissionGroup[] }> {
  return request<{ modules: PermissionGroup[] }>('/permissions')
}

export function createRole(body: RoleWrite): Promise<Role> {
  return request<Role>('/roles', { method: 'POST', body })
}

export function updateRole(id: string, body: Partial<RoleWrite>): Promise<Role> {
  return request<Role>(`/roles/${id}`, { method: 'PUT', body })
}

export function deleteRole(id: string): Promise<void> {
  return request<void>(`/roles/${id}`, { method: 'DELETE' })
}

// --- per-manager overrides + allowlist (FR-27/FR-30) ------------------------

export interface OverrideEntry {
  permission: string
  granted: boolean
}

export function getManagerPermissions(
  id: string,
): Promise<{ overrides: OverrideEntry[]; effective: string[] }> {
  return request(`/managers/${id}/permissions`)
}

export function putManagerOverrides(
  id: string,
  overrides: OverrideEntry[],
): Promise<{ overrides: OverrideEntry[] }> {
  return request(`/managers/${id}/permissions`, { method: 'PUT', body: { overrides } })
}

export interface AllowlistEntry {
  cidr: string
  note: string
}

export function getManagerAllowlist(id: string): Promise<{ entries: AllowlistEntry[] }> {
  return request(`/managers/${id}/ip-allowlist`)
}

export function putManagerAllowlist(
  id: string,
  entries: AllowlistEntry[],
): Promise<{ entries: AllowlistEntry[] }> {
  return request(`/managers/${id}/ip-allowlist`, { method: 'PUT', body: { entries } })
}

// --- TOTP 2FA (FR-28.1) -----------------------------------------------------

export interface EnrollResponse {
  otpauth_uri: string
  secret: string
}

/** Begin enrolment. Uses the enrolment grant token when forced at login. */
export function enrollTotp(enrollmentToken?: string): Promise<EnrollResponse> {
  return request<EnrollResponse>('/auth/totp/enroll', {
    method: 'POST',
    headers: enrollmentToken ? { Authorization: `Bearer ${enrollmentToken}` } : undefined,
  })
}

/** Confirm enrolment with the first code → returns one-time backup codes. */
export function verifyTotp(
  code: string,
  enrollmentToken?: string,
): Promise<{ backup_codes: string[] }> {
  return request('/auth/totp/verify', {
    method: 'POST',
    body: { code },
    headers: enrollmentToken ? { Authorization: `Bearer ${enrollmentToken}` } : undefined,
  })
}

export function disableTotp(body: { password: string; code: string }): Promise<void> {
  return request<void>('/auth/totp/disable', { method: 'POST', body })
}

// --- panel sessions (FR-29) -------------------------------------------------

export interface PanelSession {
  id: string
  manager_id: string
  ua: string
  ip: string
  created_at: string
  last_seen_at?: string
  current: boolean
}

export function listPanelSessions(managerId?: string): Promise<{ items: PanelSession[] }> {
  return request('/panel-sessions', { query: { manager_id: managerId } })
}

export function revokePanelSession(id: string): Promise<void> {
  return request<void>(`/panel-sessions/${id}`, { method: 'DELETE' })
}

// --- audit log (FR-27) ------------------------------------------------------

export interface AuditRow {
  id: number
  actor_id: string | null
  action: string
  entity_type: string
  entity_id: string
  before: unknown
  after: unknown
  ip: string
  ua: string
  at: string
  summary_key: string
  summary_params: Record<string, string>
}

export interface AuditFilters {
  actor_id?: string
  entity_type?: string
  action?: string
  from?: string
  to?: string
}

export function listAuditLog(
  params: { cursor?: string; limit?: number },
  filters: AuditFilters,
): Promise<{ items: AuditRow[]; next_cursor: string | null }> {
  return request('/audit-log', {
    query: { ...filters, cursor: params.cursor, limit: params.limit },
  })
}

export function auditExportUrl(filters: AuditFilters): string {
  const params = new URLSearchParams()
  for (const [k, v] of Object.entries(filters)) if (v) params.set(k, v)
  const qs = params.toString()
  return `${API_BASE}/audit-log/export${qs ? `?${qs}` : ''}`
}

/** Authorized file download for CSV export endpoints (adds the bearer token). */
export async function downloadAuthorized(url: string, filename: string): Promise<void> {
  const token = tokenStore.getAccessToken()
  const res = await fetch(url, { headers: token ? { Authorization: `Bearer ${token}` } : {} })
  if (!res.ok) throw new Error(`export failed (${res.status})`)
  const text = await res.text()
  const blob = new Blob([text], { type: 'text/csv;charset=utf-8' })
  const objectUrl = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = objectUrl
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(objectUrl), 1000)
}
