/** Portal session history (FR-166). Self-scoped only (IDOR rule). */
import { listPage, type Page } from './client'

export interface PortalSession {
  id: string
  started_at: string | null
  stopped_at: string | null
  bytes_in: number
  bytes_out: number
  service: string
  service_name: string
  stale: boolean
}

export function getSessions(
  params: { cursor?: string; limit?: number } = {},
): Promise<Page<PortalSession>> {
  return listPage<PortalSession>('/portal/sessions', params)
}
