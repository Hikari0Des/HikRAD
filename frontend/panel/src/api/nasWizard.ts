/**
 * FR-144 setup wizard client. Five endpoints, and the split between them is
 * the whole design:
 *
 *   state  — persisted per NAS, written on EVERY step transition (Next and
 *            Back alike) so a half-finished setup survives a browser close
 *            (gate item 4) and Back can never lose a later step's entries
 *            (gate item 3).
 *   detect — read-only probe; what lets an already-correct step be shown
 *            pre-marked and collapsed instead of re-asked (gate item 2).
 *   plan   — the literal command list the review step renders (gate item 5).
 *   apply  — hash-gated; refuses if the router (or the form) moved since the
 *            plan that produced the hash.
 */
import { request } from './client'
import type {
  AutoSetupPreview,
  WizardApplyResult,
  WizardDetection,
  WizardFormData,
  WizardPlanRequest,
  WizardState,
} from './types'

export function getWizardState(nasId: string): Promise<WizardState> {
  return request<WizardState>(`/nas/${nasId}/wizard/state`)
}

/**
 * Saves the WHOLE accumulated form, never a partial merge — the backend
 * stores what it is given, so sending only the current step's slice would
 * silently drop every other step on the next resume.
 */
export function saveWizardState(
  nasId: string,
  step: number,
  formData: WizardFormData,
): Promise<WizardState> {
  return request<WizardState>(`/nas/${nasId}/wizard/state`, {
    method: 'PUT',
    body: { step, form_data: formData },
  })
}

export function detectWizard(nasId: string): Promise<WizardDetection> {
  return request<WizardDetection>(`/nas/${nasId}/wizard/detect`, { method: 'POST' })
}

export function planWizard(nasId: string, body: WizardPlanRequest): Promise<AutoSetupPreview> {
  return request<AutoSetupPreview>(`/nas/${nasId}/wizard/plan`, { method: 'POST', body })
}

export function applyWizard(
  nasId: string,
  body: WizardPlanRequest & { preview_hash: string },
): Promise<WizardApplyResult> {
  return request<WizardApplyResult>(`/nas/${nasId}/wizard/apply`, { method: 'POST', body })
}
