/**
 * Reports API (contract C2, FR-45-47). Every endpoint is read-only and
 * ScopeFilter-scoped server-side; `&format=csv` streams the same rows as a
 * download (permission `export`, contract C2).
 */
import { API_BASE, request } from './client'

export interface ReportRange {
  from?: string
  to?: string
}

// --- revenue (FR-45.1) -------------------------------------------------------

export type RevenueGroupBy = 'day' | 'month' | 'manager' | 'profile' | 'method'

export interface RevenueRow {
  key: string
  currency: string
  amount: number
  count: number
}

export interface RevenueReport {
  /** Per-currency totals (v2 phase 4, FR-70.2) — never summed across currency. */
  totals: Record<string, number>
  rows: RevenueRow[]
}

export function getRevenueReport(
  range: ReportRange,
  groupBy: RevenueGroupBy,
): Promise<RevenueReport> {
  return request<RevenueReport>('/reports/revenue', {
    query: { from: range.from, to: range.to, group_by: groupBy },
  })
}

// --- settlement (FR-45.2, scoped to one currency since v2 phase 4) ----------

export interface SettlementReport {
  currency: string
  opening: number
  topups: number
  renewals: { count: number; amount: number }
  refunds: number
  closing: number
}

export function getSettlementReport(
  range: ReportRange,
  managerId?: string,
  currency = 'IQD',
): Promise<SettlementReport> {
  return request<SettlementReport>('/reports/settlement', {
    query: { from: range.from, to: range.to, manager_id: managerId, currency },
  })
}

// --- subscribers (FR-46) -----------------------------------------------------

export type SubscriberReportView = 'new' | 'expired' | 'expiring' | 'by_profile' | 'inactive'

export interface SubscriberReportRow {
  id: string
  username: string
  name: string
  phone: string
  status: string
  profile_id: string
  expires_at: string | null
}

export interface ProfileCountRow {
  profile_id: string
  profile_name: string
  count: number
}

export interface SubscriberReport {
  rows: SubscriberReportRow[]
  total: number
}

export interface ByProfileReport {
  rows: ProfileCountRow[]
}

export function getSubscriberReport(
  view: Exclude<SubscriberReportView, 'by_profile'>,
  range: ReportRange,
  n?: number,
): Promise<SubscriberReport> {
  return request<SubscriberReport>('/reports/subscribers', {
    query: { view, from: range.from, to: range.to, n },
  })
}

export function getByProfileReport(range: ReportRange): Promise<ByProfileReport> {
  return request<ByProfileReport>('/reports/subscribers', {
    query: { view: 'by_profile', from: range.from, to: range.to },
  })
}

// --- usage (FR-47) -----------------------------------------------------------

export interface TopConsumerRow {
  subscriber_id: string
  username: string
  down_bytes: number
  up_bytes: number
}

export interface PerNasRow {
  nas_id: string
  nas_name: string
  down_bytes: number
  up_bytes: number
}

export function getTopConsumers(
  range: ReportRange,
  limit?: number,
): Promise<{ rows: TopConsumerRow[] }> {
  return request('/reports/usage', {
    query: { view: 'top_consumers', from: range.from, to: range.to, limit },
  })
}

export function getUsagePerNas(range: ReportRange): Promise<{ rows: PerNasRow[] }> {
  return request('/reports/usage', { query: { view: 'per_nas', from: range.from, to: range.to } })
}

// --- CSV export URLs ---------------------------------------------------------

function csvUrl(path: string, query: Record<string, string | number | undefined>): string {
  const params = new URLSearchParams()
  for (const [k, v] of Object.entries(query))
    if (v !== undefined && v !== '') params.set(k, String(v))
  params.set('format', 'csv')
  return `${API_BASE}${path}?${params.toString()}`
}

export function revenueExportUrl(range: ReportRange, groupBy: RevenueGroupBy): string {
  return csvUrl('/reports/revenue', { from: range.from, to: range.to, group_by: groupBy })
}

export function settlementExportUrl(range: ReportRange, managerId?: string): string {
  return csvUrl('/reports/settlement', { from: range.from, to: range.to, manager_id: managerId })
}

export function subscribersExportUrl(
  view: SubscriberReportView,
  range: ReportRange,
  n?: number,
): string {
  return csvUrl('/reports/subscribers', { view, from: range.from, to: range.to, n })
}

export function usageExportUrl(
  view: 'top_consumers' | 'per_nas',
  range: ReportRange,
  limit?: number,
): string {
  return csvUrl('/reports/usage', { view, from: range.from, to: range.to, limit })
}

// --- margin (v2 phase 9, FR-72.3/FR-75) --------------------------------------

export interface MarginRow {
  profile_id: string
  profile_name: string
  currency: string
  revenue: number
  wholesale: number
  count: number
  reseller_margin: number
  // Owner-only (FR-75.2) — absent entirely (not null) for a reseller-scoped
  // caller; the shared frontend never assumes these are present.
  cost?: number
  unknown_cost_count?: number
  owner_margin?: number
}

export function getMarginReport(range: ReportRange): Promise<{ rows: MarginRow[] }> {
  return request('/reports/margin', { query: { from: range.from, to: range.to } })
}

export interface SiteMarginRow {
  nas_id: string
  nas_name: string
  currency: string
  revenue: number
  site_overheads: number
  net_margin: number
  global_overheads: number
}

export function getSiteMarginReport(range: ReportRange): Promise<{ rows: SiteMarginRow[] }> {
  return request('/reports/margin/sites', { query: { from: range.from, to: range.to } })
}
