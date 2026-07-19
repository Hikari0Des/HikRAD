/**
 * Managers list (Agent A's endpoint). Admin-only (managers.view); used to
 * populate the owner filter and the "move owner" bulk action. Callers degrade
 * gracefully when a non-admin operator gets 403.
 */
import { request } from './client'

export interface ManagerView {
  id: string
  username: string
  role: string
  role_id?: string | null
  scoped: boolean
  disabled?: boolean
  totp_enabled?: boolean
  created_at: string
  full_name?: string | null
  phone?: string | null
  email?: string | null
  address?: string | null
  notes?: string | null
}

/** Optional contact/profile fields shared by create and update. */
export interface ManagerProfileFields {
  full_name?: string
  phone?: string
  email?: string
  address?: string
  notes?: string
}

export function listManagers(): Promise<{ items: ManagerView[] }> {
  return request<{ items: ManagerView[] }>('/managers')
}

export interface ManagerCreate extends ManagerProfileFields {
  username: string
  password: string
  role: string
  scoped: boolean
}

export function createManager(body: ManagerCreate): Promise<ManagerView> {
  return request<ManagerView>('/managers', { method: 'POST', body })
}

export function updateManager(
  id: string,
  body: {
    role?: string
    scoped?: boolean
    password?: string
    disabled?: boolean
  } & ManagerProfileFields,
): Promise<ManagerView> {
  return request<ManagerView>(`/managers/${id}`, { method: 'PUT', body })
}

/**
 * Hard-delete a manager. The server refuses with 409 codes the page maps to
 * messages: cannot_remove_self, last_admin, has_history (→ disable instead).
 */
export function deleteManager(id: string): Promise<void> {
  return request<void>(`/managers/${id}`, { method: 'DELETE' })
}

export function unlockManager(id: string): Promise<void> {
  return request<void>(`/managers/${id}/unlock`, { method: 'POST' })
}

export function resetManagerTotp(id: string): Promise<void> {
  return request<void>(`/managers/${id}/totp/reset`, { method: 'POST' })
}
