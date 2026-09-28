/**
 * v4-10 (FR-164): support tickets. Response shapes mirror internal/support
 * exactly. Distinct from api/paymentTickets.ts (payment-confirmation
 * workflow) — these are customer-support tickets.
 */
import { listPage, request, type Page, type PageParams } from './client'

export type TicketPriority = 'normal' | 'moderate' | 'critical'
export type TicketStatus = 'open' | 'in_progress' | 'resolved' | 'closed' | 'withdrawn'

export interface SupportTicket {
  id: string
  number: string
  subscriber_id: string
  subscriber_username: string
  owner_manager_id?: string
  subject: string
  priority: TicketPriority
  status: TicketStatus
  created_by_type: string
  created_at: string
  updated_at: string
  resolved_at?: string
  closed_at?: string
}

export interface TicketMessage {
  id: string
  author_type: string
  author_id?: string
  body: string
  internal: boolean
  created_at: string
}

export interface TicketDetail extends SupportTicket {
  messages: TicketMessage[]
}

export function listSupportTickets(
  params: PageParams & {
    scope?: 'mine' | 'all'
    status?: string
    priority?: string
    search?: string
    include_withdrawn?: boolean
  },
): Promise<Page<SupportTicket>> {
  const { scope, status, priority, search, include_withdrawn, ...page } = params
  return listPage('/support-tickets', page, {
    scope,
    status,
    priority,
    search,
    include_withdrawn: include_withdrawn ? 'true' : undefined,
  })
}

export function getSupportTicketCounts(
  params: { scope?: 'mine' | 'all' } = {},
): Promise<{ open: number }> {
  return request('/support-tickets/counts', { query: params })
}

export function getSupportTicket(id: string): Promise<TicketDetail> {
  return request(`/support-tickets/${id}`)
}

export function createSupportTicket(body: {
  subscriber_id: string
  subject: string
  body: string
  priority?: TicketPriority
}): Promise<SupportTicket> {
  return request('/support-tickets', { method: 'POST', body })
}

export function addSupportTicketMessage(
  id: string,
  body: { body: string; internal: boolean },
): Promise<TicketMessage> {
  return request(`/support-tickets/${id}/messages`, { method: 'POST', body })
}

export function setSupportTicketStatus(id: string, status: TicketStatus): Promise<SupportTicket> {
  return request(`/support-tickets/${id}/status`, { method: 'POST', body: { status } })
}

export function withdrawSupportTicket(id: string): Promise<{ id: string; status: string }> {
  return request(`/support-tickets/${id}/withdraw`, { method: 'POST' })
}
