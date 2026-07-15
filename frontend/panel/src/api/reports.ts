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
  amount_iqd: number
  count: number
}

export interface RevenueReport {
  total_iqd: number
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

// --- settlement (FR-45.2) ----------------------------------------------------

export interface SettlementReport {
  opening_iqd: number
  topups_iqd: number
  renewals: { count: number; amount_iqd: number }
  refunds_iqd: number
  closing_iqd: number
}

export function getSettlementReport(
  range: ReportRange,
  managerId?: string,
): Promise<SettlementReport> {
  return request<SettlementReport>('/reports/settlement', {
    query: { from: range.from, to: range.to, manager_id: managerId },
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
