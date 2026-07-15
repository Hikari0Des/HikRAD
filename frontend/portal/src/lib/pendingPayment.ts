/**
 * Persists the in-flight gateway payment intent across a tab close (edge case,
 * task brief: "payment return route when the user closed the tab mid-payment
 * — intent poll on next open, deep-link safe"). Cleared once the intent
 * reaches a terminal state in PaymentReturnPage.
 */
const KEY = 'hikrad.portal.pending_payment_intent'

export interface PendingIntent {
  gateway: string
  intentId: string
}

export function setPendingIntent(intent: PendingIntent): void {
  try {
    window.localStorage.setItem(KEY, JSON.stringify(intent))
  } catch {
    // best-effort only
  }
}

export function getPendingIntent(): PendingIntent | null {
  try {
    const raw = window.localStorage.getItem(KEY)
    return raw ? (JSON.parse(raw) as PendingIntent) : null
  } catch {
    return null
  }
}

export function clearPendingIntent(): void {
  try {
    window.localStorage.removeItem(KEY)
  } catch {
    // ignore
  }
}
