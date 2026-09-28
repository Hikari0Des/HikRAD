/**
 * Web Push subscribe/unsubscribe — contract C4. Not wired to any portal UI
 * yet: the phase brief defers the portal's expiring-reminder push to "only if
 * trivially enabled by the same plumbing" and C's push module (landed
 * mid-Phase-4) currently only mounts the route for `surface: "panel"` — its
 * own doc comment says the portal HTTP route is D's to add. This client is
 * ready infrastructure for whenever that lands; shape verified against
 * `backend/internal/push/module.go` (DELETE takes a JSON body, not a query
 * param).
 */
import { request } from './client'

export interface PushSubscribeBody {
  surface: 'panel' | 'portal'
  subscription: PushSubscriptionJSON
}

export function subscribePush(body: PushSubscribeBody): Promise<void> {
  return request<void>('/push/subscribe', { body })
}

export function unsubscribePush(endpoint: string): Promise<void> {
  return request<void>('/push/subscribe', { method: 'DELETE', body: { endpoint } })
}
