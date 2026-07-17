/**
 * Unified payment tickets — v2-2 contracts C7-C10 (FR-79). Generalizes
 * cardpay's card_payments queue: every method (provider transfer, scratch
 * card) reviewed through the same submit → approve/reject → timeline shape.
 */
import { tokenStore } from '../auth/tokenStore'
import { API_BASE, ApiError, NetworkError, request } from './client'

export type TicketState = 'pending' | 'approved' | 'rejected'

export interface TicketListItem {
  id: string
  subscriber_id: string
  subscriber_username: string
  profile_id: string
  method_key: string
  provider_id?: string | null
  amount: number
  currency: string
  transfer_reference?: string | null
  transfer_date?: string | null
  note: string
  method_detail: Record<string, unknown>
  state: TicketState
  decided_by?: string | null
  decided_at?: string | null
  reject_reason?: string | null
  created_at: string
  updated_at: string
  owner_manager_id?: string | null
}

export interface TicketListFilters {
  scope?: 'mine' | 'all'
  state?: TicketState | ''
  provider?: string
  agent?: string
  from?: string
  to?: string
  cursor?: string
  limit?: number
}

export function listTickets(
  filters: TicketListFilters = {},
): Promise<{ items: TicketListItem[]; next_cursor: string | null }> {
  return request('/payment-tickets', {
    query: {
      scope: filters.scope,
      state: filters.state || undefined,
      provider: filters.provider,
      agent: filters.agent,
      from: filters.from,
      to: filters.to,
      cursor: filters.cursor,
      limit: filters.limit,
    },
  })
}

export interface TicketEvent {
  event_type: string
  actor_manager_id?: string | null
  note?: string | null
  at: string
}

export interface TicketAttachment {
  id: string
  filename: string
  content_type: string
  size_bytes: number
}

export interface TicketDetail extends TicketListItem {
  events: TicketEvent[]
  attachments: TicketAttachment[]
}

export function getTicket(id: string): Promise<TicketDetail> {
  return request(`/payment-tickets/${id}`)
}

export function approveTicket(
  id: string,
): Promise<{ id: string; state: 'approved'; new_expires_at: string }> {
  return request(`/payment-tickets/${id}/approve`, { method: 'POST' })
}

export function rejectTicket(id: string, reason: string): Promise<{ id: string; state: string }> {
  return request(`/payment-tickets/${id}/reject`, { method: 'POST', body: { reason } })
}

export function revealTicketCard(id: string): Promise<{ code: string }> {
  return request(`/payment-tickets/${id}/reveal`, { method: 'POST' })
}

/**
 * Fetches an attachment as an authenticated blob and returns an object URL
 * the caller must revoke (URL.revokeObjectURL) when done — <img src> cannot
 * carry an Authorization header, so the bytes are fetched here instead of
 * pointing the tag straight at the API route (C10).
 */
export async function fetchAttachmentBlobUrl(
  ticketId: string,
  attachmentId: string,
): Promise<string> {
  const headers: Record<string, string> = {}
  const token = tokenStore.getAccessToken()
  if (token) headers['Authorization'] = `Bearer ${token}`
  let res: Response
  try {
    res = await fetch(`${API_BASE}/payment-tickets/${ticketId}/attachments/${attachmentId}`, {
      headers,
    })
  } catch (err) {
    throw new NetworkError(err)
  }
  if (!res.ok) {
    throw new ApiError(
      res.status,
      'attachment_fetch_failed',
      `could not fetch attachment (HTTP ${res.status})`,
    )
  }
  const blob = await res.blob()
  return URL.createObjectURL(blob)
}
