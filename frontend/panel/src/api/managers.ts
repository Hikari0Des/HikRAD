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
  totp_enabled?: boolean
  created_at: string
}

export function listManagers(): Promise<{ items: ManagerView[] }> {
  return request<{ items: ManagerView[] }>('/managers')
}

export interface ManagerCreate {
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
  body: { role?: string; scoped?: boolean; password?: string },
): Promise<ManagerView> {
  return request<ManagerView>(`/managers/${id}`, { method: 'PUT', body })
}

export function unlockManager(id: string): Promise<void> {
  return request<void>(`/managers/${id}/unlock`, { method: 'POST' })
}

export function resetManagerTotp(id: string): Promise<void> {
  return request<void>(`/managers/${id}/totp/reset`, { method: 'POST' })
}
