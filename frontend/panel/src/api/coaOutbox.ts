/**
 * CoA Outbox (v4-8, FR-150-152). Durable queue for every CoA/Disconnect the
 * system means to send — enqueue is the only way one ever goes out; this
 * client talks to the read/retry/cancel surface over it, never to a router
 * directly.
 */
import { request, type Page } from './client'

export type CoAOutboxAction = 'update' | 'disconnect'
export type CoAOutboxState = 'pending' | 'sent' | 'nack' | 'done' | 'failed'

export interface CoAAttributeView {
  intent: string
  value: string
}

export interface CoAOutboxItem {
  id: string
  subscriber_id?: string
  nas_id: string
  nas_name: string
  acct_session_id: string
  username: string
  framed_ip: string
  service: string
  action: CoAOutboxAction
  current_op: CoAOutboxAction
  attributes: CoAAttributeView[]
  reason: string
  requested_by?: string
  state: CoAOutboxState
  attempt_count: number
  last_error?: string
  last_outcome?: string
  fallback_used: boolean
  fallback_at?: string
  fallback_reason?: string
  created_at: string
  updated_at: string
  sent_at?: string
  done_at?: string
}

/** One RADIUS attribute exactly as captured off the wire at send time — the
 * inspect view's proof (never regenerated at display time). */
export interface WireAttr {
  type: number
  hex: string
  text?: string
  vendor_id?: number
  vendor_type?: number
  vendor_hex?: string
  vendor_text?: string
}

export interface CoAOutboxAttempt {
  attempt_no: number
  op: CoAOutboxAction
  outcome: string
  error?: string
  wire_attributes: WireAttr[]
  attempted_at: string
}

export interface CoAOutboxDetail extends CoAOutboxItem {
  attempts: CoAOutboxAttempt[]
}

export interface CoAOutboxCounters {
  pending: number
  sent: number
  nack: number
  done: number
  failed: number
  total: number
  failed_24h: number
}

export interface CoAOutboxFilters {
  state?: CoAOutboxState | ''
  action?: CoAOutboxAction | ''
  search?: string
  cursor?: string
  limit?: number
}

export function listCoAOutbox(filters: CoAOutboxFilters = {}): Promise<Page<CoAOutboxItem>> {
  return request('/coa-outbox', {
    query: {
      state: filters.state || undefined,
      action: filters.action || undefined,
      search: filters.search || undefined,
      cursor: filters.cursor,
      limit: filters.limit,
    },
  })
}

export function getCoAOutboxCounters(): Promise<CoAOutboxCounters> {
  return request('/coa-outbox/counters')
}

export function getCoAOutbox(id: string): Promise<CoAOutboxDetail> {
  return request(`/coa-outbox/${id}`)
}

export function retryCoAOutbox(id: string): Promise<{ id: string; state: string }> {
  return request(`/coa-outbox/${id}/retry`, { method: 'POST' })
}

export function cancelCoAOutbox(id: string): Promise<{ id: string; state: string }> {
  return request(`/coa-outbox/${id}/cancel`, { method: 'POST' })
}
