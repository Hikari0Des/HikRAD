/** Audit-log reader (Agent A's endpoint) — the user-detail change trail. */
import { listPage, type Page } from './client'
import type { AuditEntry } from './types'

export function listAudit(params: {
  entity_type?: string
  entity_id?: string
  actor_id?: string
  cursor?: string
  limit?: number
}): Promise<Page<AuditEntry>> {
  const { entity_type, entity_id, actor_id, ...page } = params
  return listPage<AuditEntry>('/audit-log', page, { entity_type, entity_id, actor_id })
}
