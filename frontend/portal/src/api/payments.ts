/**
 * E-wallet payment API — contract C3. Routes for create/poll are frozen
 * exactly; the "enabled gateway list" the task brief calls for has no frozen
 * route, so this assumes `GET /portal/payments/gateways` (server returns only
 * gateways enabled in settings, per FR-42) — narrowest addition consistent
 * with C3, flagged as a seam in the phase status note.
 */
import { request } from './client'

export interface Gateway {
  id: string
  name: string
}

export function listGateways(): Promise<{ items: Gateway[] }> {
  return request<{ items: Gateway[] }>('/portal/payments/gateways')
}

/**
 * `profile_id` is optional here even though C3 lists it in the body: the
 * portal only ever renews the subscriber's current plan (no plan-upgrade UI
 * in scope), so it is omitted and the server renews at the existing profile
 * — the same default `renewSubscriber` uses on the panel side.
 */
export function createPayment(
  gateway: string,
  profileId?: string,
): Promise<{ redirect_url: string; intent_id: string }> {
  return request(`/portal/payments/${gateway}/create`, {
    body: profileId ? { profile_id: profileId } : {},
  })
}

export type PaymentIntentState = 'pending' | 'confirmed' | 'renewed' | 'failed' | 'expired'

export interface PaymentIntent {
  id: string
  gateway: string
  state: PaymentIntentState
  amount: number
  currency: string
  gateway_ref: string
  new_expires_at?: string
}

export function getIntent(id: string): Promise<PaymentIntent> {
  return request<PaymentIntent>(`/portal/payments/intents/${id}`)
}
