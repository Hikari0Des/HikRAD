/**
 * FR-148 hotspot login page client, scoped to one hotspot service instance.
 * Save and push are separate calls on purpose: a design can be drafted
 * without touching a live router, and a re-push (identical design, nothing to
 * save) is itself the fix after a RouterOS upgrade wiped router flash.
 */
import { request } from './client'
import type { HotspotPage, HotspotPageMode, HotspotPagePushResult } from './types'

export function getHotspotPage(nasId: string, serviceId: string): Promise<HotspotPage> {
  return request<HotspotPage>(`/nas/${nasId}/services/${serviceId}/hotspot-page`)
}

export function saveHotspotPage(
  nasId: string,
  serviceId: string,
  mode: HotspotPageMode,
  customHtml?: string,
): Promise<HotspotPage> {
  return request<HotspotPage>(`/nas/${nasId}/services/${serviceId}/hotspot-page`, {
    method: 'PUT',
    body: { mode, custom_html: customHtml ?? '' },
  })
}

/** Refused (422 nothing_to_push) when the mode is router_default. */
export function pushHotspotPage(nasId: string, serviceId: string): Promise<HotspotPagePushResult> {
  return request<HotspotPagePushResult>(`/nas/${nasId}/services/${serviceId}/hotspot-page/push`, {
    method: 'POST',
  })
}
