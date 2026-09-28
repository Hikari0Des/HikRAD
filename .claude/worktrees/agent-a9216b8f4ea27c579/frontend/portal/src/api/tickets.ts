/** Portal support tickets (FR-170, consuming v4-10's support seam). Internal
 * staff notes never appear in these shapes — the backend structurally
 * cannot include them (see internal/support/public.go). */
import { request } from './client'

export type TicketPriority = 'normal' | 'moderate' | 'critical'
export type TicketStatus = 'open' | 'in_progress' | 'resolved' | 'closed' | 'withdrawn'

export interface PortalTicket {
  id: string
  number: string
  subject: string
  priority: TicketPriority
  status: TicketStatus
  created_at: string
  updated_at: string
}

export interface PortalTicketMessage {
  id: string
  author_type: 'manager' | 'subscriber' | 'system' | 'bot'
  body: string
  created_at: string
}

export interface PortalTicketDetail extends PortalTicket {
  messages: PortalTicketMessage[]
}

export function getTickets(): Promise<{ items: PortalTicket[] }> {
  return request<{ items: PortalTicket[] }>('/portal/support-tickets')
}

export function getTicket(id: string): Promise<PortalTicketDetail> {
  return request<PortalTicketDetail>(`/portal/support-tickets/${id}`)
}

export function createTicket(subject: string, body?: string): Promise<PortalTicket> {
  return request<PortalTicket>('/portal/support-tickets', { body: { subject, body } })
}

export function replyTicket(id: string, body: string): Promise<PortalTicketMessage> {
  return request<PortalTicketMessage>(`/portal/support-tickets/${id}/messages`, { body: { body } })
}

export function withdrawTicket(id: string): Promise<void> {
  return request<void>(`/portal/support-tickets/${id}/withdraw`, { method: 'POST' })
}
