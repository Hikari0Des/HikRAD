/**
 * FR-146 Firewall tab client. The one write path in this phase that carries a
 * safety net: `apply` may return a `revert` handle, meaning the backend
 * snapshotted the pre-change rules and will restore them automatically unless
 * `confirmFirewallChange` is called before its deadline. `listPendingFirewall`
 * exists so an operator who navigated away (or reloaded after locking
 * themselves out) still sees the countdown on their way back in.
 */
import { request } from './client'
import type {
  AutoSetupPreview,
  FirewallApplyResult,
  FirewallChangeRequest,
  FirewallPendingChange,
  FirewallRule,
} from './types'

export function listFirewallRules(nasId: string): Promise<{ items: FirewallRule[] }> {
  return request<{ items: FirewallRule[] }>(`/nas/${nasId}/firewall/rules`)
}

export function planFirewallChange(
  nasId: string,
  body: FirewallChangeRequest,
): Promise<AutoSetupPreview> {
  return request<AutoSetupPreview>(`/nas/${nasId}/firewall/plan`, { method: 'POST', body })
}

export function applyFirewallChange(
  nasId: string,
  body: FirewallChangeRequest & { preview_hash: string },
): Promise<FirewallApplyResult> {
  return request<FirewallApplyResult>(`/nas/${nasId}/firewall/apply`, { method: 'POST', body })
}

/** Cancels the pending auto-revert — the operator accepting the change. */
export function confirmFirewallChange(
  nasId: string,
  snapshotId: string,
): Promise<{ status: string }> {
  return request<{ status: string }>(`/nas/${nasId}/firewall/${snapshotId}/confirm`, {
    method: 'POST',
  })
}

export function listPendingFirewall(nasId: string): Promise<{ items: FirewallPendingChange[] }> {
  return request<{ items: FirewallPendingChange[] }>(`/nas/${nasId}/firewall/pending`)
}
