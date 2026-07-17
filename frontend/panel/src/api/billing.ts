/**
 * Billing API (contract C2/C3): the single renewal money path, refunds, agent
 * balances + top-ups, the append-only ledger, vouchers and receipts. Response
 * shapes mirror internal/billing exactly (see 00-phase.md C2/C3).
 */
import { API_BASE, listPage, request, type Page, type PageParams } from './client'
import { tokenStore } from '../auth/tokenStore'
import { notifyBalanceChanged } from '../lib/balanceEvents'

/** Resolve-through wrapper: fire the balance-changed signal on success. */
async function touchingBalance<T>(p: Promise<T>): Promise<T> {
  const res = await p
  notifyBalanceChanged()
  return res
}

export type CoAResult = 'restored' | 'disconnect_fallback' | 'failed' | 'not_online'

export interface RenewResult {
  ledger_tx_id: string
  receipt_no: string
  new_expires_at: string
  currency: string
  coa_result: CoAResult
}

/** Renew a subscriber (FR-19). The idempotency key makes a retried submit safe. */
export function renewSubscriber(
  id: string,
  body: { profile_id?: string; note?: string },
  idempotencyKey: string,
): Promise<RenewResult> {
  return touchingBalance(
    request<RenewResult>(`/subscribers/${id}/renew`, {
      method: 'POST',
      body,
      headers: { 'Idempotency-Key': idempotencyKey },
    }),
  )
}

export function refundRenewal(
  id: string,
  body: { ledger_tx_id: string; reason: string },
): Promise<RenewResult> {
  return touchingBalance(
    request<RenewResult>(`/subscribers/${id}/refund`, { method: 'POST', body }),
  )
}

// --- balances (FR-20, per-currency since v2 phase 4 FR-69.2) ---------------

export function getManagerBalance(
  id: string,
  currency = 'IQD',
): Promise<{ currency: string; balance: number }> {
  return request<{ currency: string; balance: number }>(
    `/managers/${id}/balance?currency=${encodeURIComponent(currency)}`,
  )
}

/** Every currency the manager has ever touched (new, plural — C7). */
export function listManagerBalances(
  id: string,
): Promise<{ balances: { currency: string; balance: number }[] }> {
  return request(`/managers/${id}/balances`)
}

export function topupManager(
  id: string,
  body: { amount: number; currency?: string; note?: string },
): Promise<{ ledger_tx_id: string; currency: string; balance: number }> {
  return touchingBalance(request(`/managers/${id}/topup`, { method: 'POST', body }))
}

/** Currency catalog for building currency <select>s (C7). */
export interface Currency {
  code: string
  minor_unit_digits: number
  symbol: string
}

export function listCurrencies(): Promise<{ items: Currency[] }> {
  return request<{ items: Currency[] }>('/currencies')
}

export interface CurrencyRate {
  id: string
  from_currency: string
  to_currency: string
  rate: number
  effective_from: string
  created_by: string | null
  created_at: string
}

export function listCurrencyRates(from?: string, to?: string): Promise<{ items: CurrencyRate[] }> {
  const params = new URLSearchParams()
  if (from) params.set('from', from)
  if (to) params.set('to', to)
  const qs = params.toString()
  return request<{ items: CurrencyRate[] }>(`/currency-rates${qs ? `?${qs}` : ''}`)
}

export function createCurrencyRate(body: {
  from_currency: string
  to_currency: string
  rate: number
}): Promise<CurrencyRate> {
  return request<CurrencyRate>('/currency-rates', { method: 'POST', body })
}

export function exchangeManagerBalance(
  id: string,
  body: { from_currency: string; to_currency: string; amount: number; currency_rate_id: string },
): Promise<{
  exchange_reference: string
  from_ledger_tx_id: string
  to_ledger_tx_id: string
  from_balance: number
  to_balance: number
}> {
  return touchingBalance(request(`/managers/${id}/exchange`, { method: 'POST', body }))
}

// --- ledger (FR-24) ---------------------------------------------------------

export type LedgerType =
  'renewal' | 'topup' | 'manual_payment' | 'voucher_redeem' | 'refund' | 'adjustment' | 'discount'

export interface LedgerItem {
  id: string
  at: string
  type: LedgerType
  amount: number
  currency: string
  actor_manager_id: string | null
  subscriber_id: string | null
  source: string
  reference: string
  reverses_id: string | null
  note: string
}

export interface LedgerFilters {
  manager_id?: string
  subscriber_id?: string
  type?: string
  currency?: string
  from?: string
  to?: string
}

export function listLedger(params: PageParams, filters: LedgerFilters): Promise<Page<LedgerItem>> {
  return listPage<LedgerItem>('/ledger', params, { ...filters })
}

/** Absolute URL to the CSV export (opened via an authorized fetch download). */
export function ledgerExportUrl(filters: LedgerFilters): string {
  const params = new URLSearchParams()
  for (const [k, v] of Object.entries(filters)) if (v) params.set(k, v)
  const qs = params.toString()
  return `${API_BASE}/ledger/export${qs ? `?${qs}` : ''}`
}

// --- vouchers (FR-22) -------------------------------------------------------

export interface VoucherBatch {
  id: string
  profile_id: string
  prefix: string
  count: number
  unit_price: number
  currency: string
  state: string
  expires_at: string | null
  created_at: string
  used: number
  unused: number
  void: number
  expired: number
}

export interface VoucherCode {
  id: string
  state: 'unused' | 'used' | 'void'
  used_for_subscriber_id: string | null
  used_at: string | null
}

export function listVoucherBatches(): Promise<{ items: VoucherBatch[] }> {
  return request<{ items: VoucherBatch[] }>('/vouchers/batches')
}

export function getVoucherBatch(id: string): Promise<{ items: VoucherCode[] }> {
  return request<{ items: VoucherCode[] }>(`/vouchers/batches/${id}`)
}

export function voidVoucherBatch(
  id: string,
): Promise<{ voided_unused: number; credit: number; currency: string }> {
  return touchingBalance(request(`/vouchers/batches/${id}/void`, { method: 'POST' }))
}

export function redeemVoucher(body: { code: string; subscriber_id: string }): Promise<RenewResult> {
  return touchingBalance(request<RenewResult>('/vouchers/redeem', { method: 'POST', body }))
}

/**
 * Create a batch and trigger the plaintext-CSV download (the only time codes
 * exist in the clear). Returns the new batch id from the X-Batch-Id header.
 */
export async function createVoucherBatch(body: {
  profile_id: string
  count: number
  prefix?: string
  expires_at?: string | null
  code_length?: number
}): Promise<{ batchId: string }> {
  const token = tokenStore.getAccessToken()
  const res = await fetch(`${API_BASE}/vouchers/batches`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'text/csv',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: JSON.stringify(body),
  })
  if (!res.ok) {
    let payload: unknown
    try {
      payload = await res.json()
    } catch {
      payload = undefined
    }
    const env = payload as { error?: { code?: string; message?: string } } | undefined
    throw new VoucherBatchError(
      res.status,
      env?.error?.code ?? 'unknown',
      env?.error?.message ?? '',
    )
  }
  const batchId = res.headers.get('X-Batch-Id') ?? ''
  const csv = await res.text()
  triggerDownload(csv, `vouchers-${batchId}.csv`, 'text/csv')
  notifyBalanceChanged()
  return { batchId }
}

export class VoucherBatchError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
  ) {
    super(message)
    this.name = 'VoucherBatchError'
  }
}

/** Force a client-side file download from an in-memory string. */
export function triggerDownload(content: string, filename: string, mime: string): void {
  const blob = new Blob([content], { type: `${mime};charset=utf-8` })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

// --- receipts (FR-21) -------------------------------------------------------

/** Fetch the print-ready receipt HTML in the requested language. */
export async function fetchReceiptHtml(receiptNo: string, lang: 'ar' | 'en'): Promise<string> {
  const token = tokenStore.getAccessToken()
  const res = await fetch(`${API_BASE}/payments/${receiptNo}/receipt?lang=${lang}`, {
    headers: token ? { Authorization: `Bearer ${token}` } : {},
  })
  if (!res.ok) throw new Error(`receipt fetch failed (${res.status})`)
  return res.text()
}

/** Open the receipt in a print window (fetches the localized HTML first). */
export async function printReceipt(receiptNo: string, lang: 'ar' | 'en'): Promise<void> {
  const html = await fetchReceiptHtml(receiptNo, lang)
  const win = window.open('', '_blank', 'width=420,height=640')
  if (!win) throw new Error('popup blocked')
  win.document.open()
  win.document.write(html)
  win.document.close()
}

// --- v2 phase 9: plan cost, overheads, reseller pricing (FR-71/73/74) ------

export interface CostHistoryEntry {
  id: string
  cost: number
  currency: string
  effective_from: string
}

export function setProfileCost(
  profileId: string,
  body: { cost: number; currency: string },
): Promise<CostHistoryEntry> {
  return request(`/profiles/${profileId}/cost`, { method: 'POST', body })
}

export function listProfileCostHistory(profileId: string): Promise<{ items: CostHistoryEntry[] }> {
  return request(`/profiles/${profileId}/cost-history`)
}

export interface Overhead {
  id: string
  name: string
  amount: number
  currency: string
  nas_id: string | null
  period_start: string
  period_end: string | null
  notes: string
}

export function listOverheads(
  params: { nas_id?: string; as_of?: string } = {},
): Promise<{ items: Overhead[] }> {
  const q = new URLSearchParams()
  if (params.nas_id) q.set('nas_id', params.nas_id)
  if (params.as_of) q.set('as_of', params.as_of)
  const qs = q.toString()
  return request(`/overheads${qs ? `?${qs}` : ''}`)
}

export function createOverhead(body: {
  name: string
  amount: number
  currency: string
  nas_id?: string | null
  period_start: string
  period_end?: string | null
  notes?: string
}): Promise<Overhead> {
  return request('/overheads', { method: 'POST', body })
}

export interface ResellerPrice {
  id: string
  manager_id: string
  profile_id: string
  subscriber_id: string | null
  price: number
  currency: string
  effective_from: string
}

export function listResellerPrices(params: {
  manager_id?: string
  profile_id?: string
}): Promise<{ items: ResellerPrice[] }> {
  const q = new URLSearchParams()
  if (params.manager_id) q.set('manager_id', params.manager_id)
  if (params.profile_id) q.set('profile_id', params.profile_id)
  const qs = q.toString()
  return request(`/reseller-prices${qs ? `?${qs}` : ''}`)
}

export function createResellerPrice(body: {
  manager_id: string
  profile_id: string
  subscriber_id?: string | null
  price: number
  currency: string
}): Promise<ResellerPrice> {
  return request('/reseller-prices', { method: 'POST', body })
}
