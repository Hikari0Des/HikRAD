/**
 * FR-147 RouterOS Update tab client. Read and report by default: `getUpdate`
 * and `checkUpdate` are safe, `applyUpdate` reboots the router and drops every
 * live session — it is gated on its own `nas.update` permission and requires
 * confirm=true on the wire on top of the panel's own double confirm.
 */
import { request } from './client'
import type { RouterUpdateInfo } from './types'

export function getUpdateInfo(nasId: string): Promise<RouterUpdateInfo> {
  return request<RouterUpdateInfo>(`/nas/${nasId}/update`)
}

/** Triggers the router's own check first, then re-reads. */
export function checkUpdate(nasId: string): Promise<RouterUpdateInfo> {
  return request<RouterUpdateInfo>(`/nas/${nasId}/update/check`, { method: 'POST' })
}

export function applyUpdate(nasId: string, channel?: string): Promise<{ status: string }> {
  return request<{ status: string }>(`/nas/${nasId}/update/apply`, {
    method: 'POST',
    body: channel ? { confirm: true, channel } : { confirm: true },
  })
}
