/**
 * Card-payment verification queue (contract C8, FR-59). Codes never appear in
 * list payloads — `reveal` returns one once, audited, per call.
 */
import { request } from './client'

export type CardPaymentState = 'pending' | 'approved' | 'rejected'

export interface CardPaymentRow {
  id: string
  subscriber_id: string
  username: string
  profile_id: string
  profile_name: string
  requested_amount: number
  currency: string
  card_type: string
  state: CardPaymentState
  created_at: string
  decided_by?: string | null
  decided_at?: string | null
  reject_reason?: string | null
}

export function listCardPayments(state?: CardPaymentState): Promise<{ items: CardPaymentRow[] }> {
  return request('/card-payments', { query: { state } })
}

export function revealCardPayment(id: string): Promise<{ code: string }> {
  return request(`/card-payments/${id}/reveal`, { method: 'POST' })
}

export interface CardPaymentApproveResult {
  ledger_tx_id: string
  receipt_no: string
  new_expires_at: string
  currency: string
  coa_result: string
}

export function approveCardPayment(id: string): Promise<CardPaymentApproveResult> {
  return request(`/card-payments/${id}/approve`, { method: 'POST' })
}

export function rejectCardPayment(
  id: string,
  reason: string,
): Promise<{ id: string; state: string }> {
  return request(`/card-payments/${id}/reject`, { method: 'POST', body: { reason } })
}
