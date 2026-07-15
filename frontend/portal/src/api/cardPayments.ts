/**
 * Scratch-card payments — contract C8 (FR-59, amendment 2026-07-11). The
 * submit route is frozen exactly. The card-type picker and the subscriber's
 * own pending/decided status have no frozen route (C8 only specifies the
 * admin-side `GET /card-payments?state=`) — this assumes
 * `GET /portal/card-payments/types` (settings-driven list) and
 * `GET /portal/card-payments/mine` (latest relevant record, for the portal's
 * "pending ISP verification" banner and decision notification, task 3b).
 * Both are narrow additive seams, flagged in the phase status note.
 */
import { ApiError, request } from './client'

export interface CardType {
  id: string
  name: string
}

export function listCardTypes(): Promise<{ items: CardType[] }> {
  return request<{ items: CardType[] }>('/portal/card-payments/types')
}

export type CardPaymentState = 'pending' | 'approved' | 'rejected'

export interface MyCardPayment {
  id: string
  card_type: string
  state: CardPaymentState
  trial_expires_at: string
  reject_reason?: string
  created_at: string
}

/** null when the subscriber has no pending/recently-decided card payment. */
export function getMyCardPayment(): Promise<MyCardPayment | null> {
  return request<MyCardPayment | null>('/portal/card-payments/mine')
}

export type CardPaymentOutcome =
  | { kind: 'ok'; trial_expires_at: string }
  | { kind: 'invalid_code' }
  | { kind: 'no_profile' }
  | { kind: 'already_pending' }
  | { kind: 'cooldown'; retryAt?: string }

export async function submitCardPayment(
  cardType: string,
  code: string,
): Promise<CardPaymentOutcome> {
  try {
    const res = await request<{ state: 'pending'; trial_expires_at: string }>(
      '/portal/card-payments',
      {
        body: { card_type: cardType, code },
      },
    )
    return { kind: 'ok', trial_expires_at: res.trial_expires_at }
  } catch (err) {
    if (err instanceof ApiError) {
      if (err.code === 'card_payment_pending') return { kind: 'already_pending' }
      if (err.code === 'card_payment_cooldown') {
        return { kind: 'cooldown', retryAt: err.message.match(/\d{4}-\d{2}-\d{2}T[\d:.Z]+/)?.[0] }
      }
      if (err.code === 'card_code_invalid') return { kind: 'invalid_code' }
      if (err.code === 'no_profile') return { kind: 'no_profile' }
    }
    throw err
  }
}
