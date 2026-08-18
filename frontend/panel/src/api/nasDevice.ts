/** Router manager I client (v4-6, FR-139-143): monitor grid + read-only
 * device tabs + the reboot/ping action tools. Every tab read hits a cache
 * (device_config_cache / device_metrics / device_interfaces) — never a
 * synchronous router call — so these calls are safe to fire on page load. */
import { request } from './client'
import type {
  DeviceGeneral,
  DeviceBridge,
  DeviceDns,
  DeviceInterfaces,
  DeviceIpTab,
  DeviceMonitorRow,
  DeviceOverview,
  DeviceOwnedItem,
  DevicePingResult,
  DeviceTabEnvelope,
  DeviceVlan,
} from './types'

export function listDeviceMonitor(): Promise<{ items: DeviceMonitorRow[] }> {
  return request<{ items: DeviceMonitorRow[] }>('/nas/monitor')
}

export function getOverviewTab(
  nasId: string,
  hours?: number,
): Promise<DeviceTabEnvelope<DeviceOverview>> {
  return request(`/nas/${nasId}/device/overview`, { query: { hours } })
}

export function getInterfacesTab(
  nasId: string,
  iface?: string,
  hours?: number,
): Promise<DeviceTabEnvelope<DeviceInterfaces>> {
  return request(`/nas/${nasId}/device/interfaces`, { query: { iface, hours } })
}

export function getGeneralTab(nasId: string): Promise<DeviceTabEnvelope<DeviceGeneral>> {
  return request(`/nas/${nasId}/device/general`)
}

export function getBridgeTab(
  nasId: string,
): Promise<DeviceTabEnvelope<DeviceOwnedItem<DeviceBridge>[]>> {
  return request(`/nas/${nasId}/device/bridge`)
}

export function getVlanTab(
  nasId: string,
): Promise<DeviceTabEnvelope<DeviceOwnedItem<DeviceVlan>[]>> {
  return request(`/nas/${nasId}/device/vlan`)
}

export function getIpTab(nasId: string): Promise<DeviceTabEnvelope<DeviceIpTab>> {
  return request(`/nas/${nasId}/device/ip`)
}

export function getDnsTab(nasId: string): Promise<DeviceTabEnvelope<DeviceDns>> {
  return request(`/nas/${nasId}/device/dns`)
}

/** Explicit "refresh now" (FR-139/140): kicks one off-cycle worker tick in
 * the background and returns immediately — never waits on the router. Poll
 * the tab endpoint afterward to see `read_at` advance. */
export function refreshDevice(nasId: string): Promise<{ status: string }> {
  return request(`/nas/${nasId}/device/refresh`, { method: 'POST' })
}

/** FR-142: the first genuinely destructive router action in the product.
 * Requires confirm=true; refused (422 no_api_credentials) when HikRAD holds
 * no saved API credentials for the router — the only consent gesture that
 * exists for "ownership" of this device. */
export function rebootDevice(nasId: string): Promise<{ status: string }> {
  return request(`/nas/${nasId}/device/reboot`, { method: 'POST', body: { confirm: true } })
}

export function pingDevice(nasId: string, target: string, count = 4): Promise<DevicePingResult> {
  return request(`/nas/${nasId}/device/ping`, { method: 'POST', body: { target, count } })
}
