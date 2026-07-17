/** NAS registry + discovery + config-snippet REST client (contract C7-B). */
import { request } from './client'
import type {
  AutoSetupApplyResult,
  AutoSetupPreview,
  AutoSetupValues,
  DiscoveredNas,
  DiscoveredService,
  Nas,
  NasConfig,
  NasHealthFinding,
  NasService,
  NasSnippet,
  NasStatus,
  NasWrite,
  ServiceApplyResult,
  ServiceProvisionRequest,
  ServiceRouterConfig,
} from './types'

export function listNas(): Promise<{ items: Nas[] }> {
  return request<{ items: Nas[] }>('/nas')
}

export function getNas(id: string): Promise<Nas> {
  return request<Nas>(`/nas/${id}`)
}

export function createNas(body: NasWrite): Promise<Nas> {
  return request<Nas>('/nas', { method: 'POST', body })
}

export function updateNas(id: string, body: NasWrite): Promise<Nas> {
  return request<Nas>(`/nas/${id}`, { method: 'PUT', body })
}

/**
 * Delete a NAS. The server refuses (409 confirmation_required) when it has live
 * sessions unless `confirm` is set; retrying with confirm deletes and marks
 * those sessions stale (FR-13.4).
 */
export function deleteNas(id: string, confirm = false): Promise<void> {
  return request<void>(`/nas/${id}`, {
    method: 'DELETE',
    query: confirm ? { confirm: 'true' } : undefined,
  })
}

export function nasSnippet(id: string, ros: '6' | '7'): Promise<NasSnippet> {
  return request<NasSnippet>(`/nas/${id}/config-snippet`, { query: { ros } })
}

/** FR-14.4 "seen since created" check for the wizard's Test button. */
export function nasStatus(id: string): Promise<NasStatus> {
  return request<NasStatus>(`/nas/${id}/status`)
}

/**
 * Read version/board/identity from the router over its API (read-only; needs
 * saved API credentials) and refresh the stored ros_version (item 8).
 */
export function probeNas(
  id: string,
): Promise<{ ros_version: string; board_name: string; identity: string }> {
  return request(`/nas/${id}/probe`, { method: 'POST' })
}

/**
 * Read the router's real PPPoE/Hotspot service instances (FR-62.6). Read-only
 * on both sides: it touches nothing on the router and saves nothing in HikRAD —
 * it returns rows for the operator to confirm in the form.
 */
export function discoverNasServices(
  id: string,
): Promise<{ items: DiscoveredService[]; health: NasHealthFinding[] }> {
  return request(`/nas/${id}/discover-services`, { method: 'POST' })
}

/**
 * NAS auto-discovery (FR-56.1): passive MNDP listen (+ optional range scan).
 * Read-only — it never touches a router. `mndp_wait_ms` bounds the listen.
 */
export function discoverNas(body?: {
  mndp_wait_ms?: number
  scan_cidr?: string
}): Promise<{ items: DiscoveredNas[] }> {
  return request<{ items: DiscoveredNas[] }>('/nas/discover', { method: 'POST', body: body ?? {} })
}

/**
 * NAS API auto-setup (FR-56.2-56.4, contract C6; extended by v2 phase 2
 * FR-66). Preview connects read-only; apply refuses unless the router's
 * state (and the values/resolutions submitted) hashes to the same
 * preview_hash (recomputed server-side) and unless the NAS's ROS version has
 * a green matrix leg — see docs/ops/ros-matrix.md. `resolutions` maps a
 * conflict's `key` to "update" or "keep"; an omitted/empty map aborts on
 * every conflict, identical to pre-FR-66 behavior.
 */
export function previewAutoSetup(
  id: string,
  opts?: { values?: AutoSetupValues; resolutions?: Record<string, string> },
): Promise<AutoSetupPreview> {
  return request<AutoSetupPreview>(`/nas/${id}/auto-setup/preview`, {
    method: 'POST',
    body: { values: opts?.values ?? {}, resolutions: opts?.resolutions ?? {} },
  })
}

export function applyAutoSetup(
  id: string,
  previewHash: string,
  opts?: { values?: AutoSetupValues; resolutions?: Record<string, string> },
): Promise<AutoSetupApplyResult> {
  return request<AutoSetupApplyResult>(`/nas/${id}/auto-setup/apply`, {
    method: 'POST',
    body: {
      preview_hash: previewHash,
      values: opts?.values ?? {},
      resolutions: opts?.resolutions ?? {},
    },
  })
}

/** Read-only RADIUS-relevant router config (v2 phase 2, FR-65). */
export function nasConfig(id: string): Promise<NasConfig> {
  return request<NasConfig>(`/nas/${id}/config`)
}

/**
 * Live router-side view of one service instance (v2 phase 2, FR-67.2/67.5) —
 * works for both management modes; only writes are gated on mode.
 */
export function serviceRouterConfig(
  nasId: string,
  serviceId: string,
): Promise<ServiceRouterConfig> {
  return request<ServiceRouterConfig>(`/nas/${nasId}/services/${serviceId}/router-config`)
}

/** Preview creating (no service_id) or editing (service_id set, must already be system-managed) one server (FR-67.3/67.4). */
export function planService(
  nasId: string,
  body: ServiceProvisionRequest,
): Promise<AutoSetupPreview> {
  return request<AutoSetupPreview>(`/nas/${nasId}/services/plan`, { method: 'POST', body })
}

export function applyService(
  nasId: string,
  body: ServiceProvisionRequest & { preview_hash: string },
): Promise<ServiceApplyResult> {
  return request<ServiceApplyResult>(`/nas/${nasId}/services/apply`, { method: 'POST', body })
}

/**
 * Adopt a router-managed service (FR-67.5): writes NOTHING to the router,
 * only flips management_mode after an explicit confirm.
 */
export function adoptService(nasId: string, serviceId: string): Promise<NasService> {
  return request<NasService>(`/nas/${nasId}/services/${serviceId}/adopt`, {
    method: 'POST',
    body: { confirm: true },
  })
}
