/**
 * Web Push subscribe API (contract C4). Verified against C's actual
 * `backend/internal/push/module.go` (landed mid-Phase-4, this repo):
 * `POST /push/subscribe {surface,subscription:{endpoint,keys:{p256dh,auth}}}`
 * and `DELETE /push/subscribe {endpoint}` (a JSON body, not a query param —
 * matched here). `getVapidPublicKey()` is a confirmed gap, not just an
 * unfrozen assumption: the package only exposes the VAPID public key via a
 * Go call (`push.EnsureKeys`), no HTTP route for it exists for either
 * surface (its doc comment says the *portal* route is D's to add; the panel
 * one this file needs is unassigned). `GET /api/v1/push/vapid-public-key`
 * here is this file's proposed shape — flagged in the phase status note.
 * Reads the panel's existing token store for the bearer token (import only,
 * no edits to it) to stay entirely inside this phase's src/pwa/** scope.
 */
import { tokenStore } from '../auth/tokenStore'

const API_BASE = '/api/v1'

function authHeaders(): Record<string, string> {
  const token = tokenStore.getAccessToken()
  return token ? { Authorization: `Bearer ${token}` } : {}
}

export async function getVapidPublicKey(): Promise<string | null> {
  try {
    const res = await fetch(`${API_BASE}/push/vapid-public-key`, {
      headers: { Accept: 'application/json', ...authHeaders() },
    })
    if (!res.ok) return null
    const data = (await res.json()) as { key: string }
    return data.key
  } catch {
    return null
  }
}

export async function subscribePush(subscription: PushSubscriptionJSON): Promise<boolean> {
  try {
    const res = await fetch(`${API_BASE}/push/subscribe`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...authHeaders() },
      body: JSON.stringify({ surface: 'panel', subscription }),
    })
    return res.ok
  } catch {
    return false
  }
}

export async function unsubscribePush(endpoint: string): Promise<void> {
  try {
    await fetch(`${API_BASE}/push/subscribe`, {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json', ...authHeaders() },
      body: JSON.stringify({ endpoint }),
    })
  } catch {
    // best-effort
  }
}

/** VAPID keys are URL-safe base64; PushManager wants a raw Uint8Array. */
export function urlBase64ToUint8Array(base64: string): Uint8Array {
  const padding = '='.repeat((4 - (base64.length % 4)) % 4)
  const base64Safe = (base64 + padding).replace(/-/g, '+').replace(/_/g, '/')
  const raw = atob(base64Safe)
  return Uint8Array.from([...raw].map((c) => c.charCodeAt(0)))
}
