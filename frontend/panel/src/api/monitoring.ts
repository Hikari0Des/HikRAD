/**
 * Monitoring API (contract C5): dashboard tiles, system health, monitored-device
 * CRUD (FR-60), probe history (shared NAS/device shape), alert rules + events,
 * and the in-app notifications SSE. Shapes mirror internal/monitorsvc.
 */
import { API_BASE, request } from './client'
import { tokenStore } from '../auth/tokenStore'

// --- dashboard (FR-32) ------------------------------------------------------

export interface SparkPoint {
  t: string
  online: number
}

export interface DashboardNasCard {
  id: string
  name: string
  status: string
  latency_ms: number | null
  downtime_s?: number
}

export interface DashboardBalance {
  currency: string
  balance: number
}

export interface DashboardAlertItem {
  id: string
  at: string
  type: string
  summary: string
}

/**
 * v2-10 contract C3: every field is optional because a `?widgets=` call only
 * ever returns the keys the (permission-filtered) requested widget ids need
 * — a field's absence means "not requested/not permitted," never "zero."
 */
export interface Dashboard {
  online_now?: number
  online_24h_sparkline?: SparkPoint[]
  subs?: { active: number; expired: number; expiring_7d: number }
  revenue_today_iqd?: number
  nas_cards?: DashboardNasCard[]
  radius_rps?: number
  pipeline?: { invariant_ok: boolean; depth: number }
  my_balance?: DashboardBalance[]
  pending_payment_tickets?: number
  alerts_feed?: DashboardAlertItem[]
}

/**
 * `widgets` omitted → the frozen pre-v2-10 full aggregate (requires
 * `monitoring.view`, contract C3's legacy path). `widgets` given (even `[]`)
 * → the new per-widget path: only the permitted, requested keys come back.
 */
export function getDashboard(widgets?: string[]): Promise<Dashboard> {
  if (!widgets) return request<Dashboard>('/dashboard')
  return request<Dashboard>('/dashboard', { query: { widgets: widgets.join(',') } })
}

// --- health (FR-35) ---------------------------------------------------------

export interface DiskUsage {
  path: string
  total_bytes: number
  used_bytes: number
  free_bytes: number
  used_percent: number
}

export interface Health {
  freeradius: { up: boolean; req_rate: number; reject_rate: number }
  api: { up: boolean }
  db: { up: boolean }
  redis: { up: boolean }
  queue: {
    depth: number
    drain_rate: number
    invariant_ok: boolean
    enforcement_failures: number
    counters: Record<string, unknown>
  }
  disk: DiskUsage[]
  license: Record<string, unknown>
  /**
   * Cloudflare tunnel state (contract C7, FR-57) — optional because, as of
   * this writing, the health handler does not yet publish it; the remote-
   * access settings screen treats an absent field as "unknown", not an error.
   */
  tunnel?: { state: 'disabled' | 'connected' | 'disconnected' }
}

export function getHealth(): Promise<Health> {
  return request<Health>('/health')
}

// --- monitored devices (FR-60) ---------------------------------------------

export type DeviceType = 'ap' | 'switch' | 'router' | 'server' | 'other'

export interface Device {
  id: string
  name: string
  ip: string
  type: DeviceType
  has_snmp: boolean
  location: string
  notes: string
  enabled: boolean
  status: string
  created_at: string
  updated_at: string
}

export interface DeviceWrite {
  name: string
  ip: string
  type?: DeviceType
  snmp_community?: string | null
  location?: string
  notes?: string
  enabled?: boolean
}

export function listDevices(): Promise<{ items: Device[] }> {
  return request<{ items: Device[] }>('/devices')
}

export function createDevice(body: DeviceWrite): Promise<Device> {
  return request<Device>('/devices', { method: 'POST', body })
}

export function updateDevice(id: string, body: DeviceWrite): Promise<Device> {
  return request<Device>(`/devices/${id}`, { method: 'PUT', body })
}

export function deleteDevice(id: string): Promise<void> {
  return request<void>(`/devices/${id}`, { method: 'DELETE' })
}

// --- probe history (shared NAS/device shape) --------------------------------

export interface ProbeSample {
  at: string
  kind: string
  latency_ms?: number
  loss?: number
  cpu?: number
  mem?: number
  uptime?: number
  ok: boolean
}

export interface DowntimeWindow {
  from: string
  to: string | null
  seconds: number
}

export interface ProbeHistory {
  status: string
  series: ProbeSample[]
  downtime: DowntimeWindow[]
}

export function getNasProbes(id: string): Promise<ProbeHistory> {
  return request<ProbeHistory>(`/nas/${id}/probes`)
}

export function getDeviceProbes(id: string): Promise<ProbeHistory> {
  return request<ProbeHistory>(`/devices/${id}/probes`)
}

// --- alert rules + events (FR-36) -------------------------------------------

export type AlertRuleType =
  | 'nas_down'
  | 'nas_up'
  | 'device_down'
  | 'device_up'
  | 'radius_reject_spike'
  | 'acct_backlog'
  | 'disk_low'
  | 'expiring_digest'
  | 'agent_balance_low'

export type AlertChannel = 'inapp' | 'telegram' | 'email' | 'whatsapp'

export interface AlertRule {
  id: string
  name: string
  type: AlertRuleType
  threshold: Record<string, unknown> | null
  channels: AlertChannel[]
  recipients: Record<string, unknown> | null
  quiet_hours: Record<string, unknown> | null
  cooldown_s: number
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface AlertRuleWrite {
  name: string
  type: AlertRuleType
  threshold?: Record<string, unknown> | null
  channels: AlertChannel[]
  recipients?: Record<string, unknown> | null
  quiet_hours?: Record<string, unknown> | null
  cooldown_s?: number
  enabled?: boolean
}

export function listAlertRules(): Promise<{ items: AlertRule[] }> {
  return request<{ items: AlertRule[] }>('/alert-rules')
}

export function createAlertRule(body: AlertRuleWrite): Promise<AlertRule> {
  return request<AlertRule>('/alert-rules', { method: 'POST', body })
}

export function updateAlertRule(id: string, body: AlertRuleWrite): Promise<AlertRule> {
  return request<AlertRule>(`/alert-rules/${id}`, { method: 'PUT', body })
}

export interface AlertEvent {
  id: string
  rule_id: string | null
  at: string
  state: string
  type: string
  summary: string
  payload: Record<string, unknown> | null
  deliveries: Record<string, unknown> | null
}

export function listAlertEvents(
  cursor?: string,
): Promise<{ items: AlertEvent[]; next_cursor: string | null }> {
  return request('/alert-events', { query: { cursor } })
}

// --- notifications SSE (FR-36) ----------------------------------------------

export interface Notification {
  type: string
  state: string
  summary: string
  at: string
}

export interface SseHandle {
  close: () => void
}

/**
 * Open the in-app notifications SSE. EventSource cannot set an Authorization
 * header, so we hand-roll a fetch stream with the bearer token (same technique
 * as the live-sessions feed).
 */
export function openNotificationStream(handlers: {
  onNotification: (n: Notification) => void
  onError?: () => void
}): SseHandle {
  const ctrl = new AbortController()
  const token = tokenStore.getAccessToken()

  void (async () => {
    try {
      const res = await fetch(`${API_BASE}/live/notifications`, {
        headers: {
          Accept: 'text/event-stream',
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
        },
        signal: ctrl.signal,
      })
      if (!res.ok || !res.body) {
        handlers.onError?.()
        return
      }
      const reader = res.body.getReader()
      const decoder = new TextDecoder()
      let buf = ''
      for (;;) {
        const { done, value } = await reader.read()
        if (done) break
        buf += decoder.decode(value, { stream: true })
        let idx: number
        while ((idx = buf.indexOf('\n\n')) !== -1) {
          const frame = buf.slice(0, idx)
          buf = buf.slice(idx + 2)
          const dataLine = frame.split('\n').find((l) => l.startsWith('data:'))
          if (!dataLine) continue
          try {
            handlers.onNotification(JSON.parse(dataLine.slice(5).trim()) as Notification)
          } catch {
            /* ignore malformed frame */
          }
        }
      }
    } catch (err) {
      if (!(err instanceof DOMException && err.name === 'AbortError')) handlers.onError?.()
    }
  })()

  return { close: () => ctrl.abort() }
}
