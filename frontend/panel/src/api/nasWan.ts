/**
 * FR-145 Internet (WAN uplink) tab client. The list is a LIVE router read
 * annotated with HikRAD's own management_mode — "ours editable, pre-existing
 * shown read-only" is decided by that field, never inferred from router
 * state. adopt writes nothing to the router; it only imports the router's
 * uplinks as read-only references.
 */
import { request } from './client'
import type {
  AutoSetupPreview,
  WanAdoptResult,
  WanUplink,
  WizardApplyResult,
  WizardWanInput,
} from './types'

export function listWanUplinks(nasId: string): Promise<{ items: WanUplink[] }> {
  return request<{ items: WanUplink[] }>(`/nas/${nasId}/wan`)
}

export function planWanUplink(nasId: string, body: WizardWanInput): Promise<AutoSetupPreview> {
  return request<AutoSetupPreview>(`/nas/${nasId}/wan/plan`, { method: 'POST', body })
}

export function applyWanUplink(
  nasId: string,
  body: WizardWanInput & { preview_hash: string },
): Promise<WizardApplyResult> {
  return request<WizardApplyResult>(`/nas/${nasId}/wan/apply`, { method: 'POST', body })
}

/** Imports every router-reported uplink HikRAD doesn't own as a read-only reference. */
export function adoptWanUplinks(nasId: string): Promise<WanAdoptResult> {
  return request<WanAdoptResult>(`/nas/${nasId}/wan/adopt`, {
    method: 'POST',
    body: { confirm: true },
  })
}
