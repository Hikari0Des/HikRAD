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
  scoped: boolean
  created_at: string
}

export function listManagers(): Promise<{ items: ManagerView[] }> {
  return request<{ items: ManagerView[] }>('/managers')
}
