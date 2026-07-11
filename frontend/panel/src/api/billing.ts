/**
 * Billing API (contract C2/C3): the single renewal money path, refunds, agent
 * balances + top-ups, the append-only ledger, vouchers and receipts. Response
 * shapes mirror internal/billing exactly (see 00-phase.md C2/C3).
 */
import { API_BASE, listPage, request, type Page, type PageParams } from './client'
import { tokenStore } from '../auth/tokenStore'

export type CoAResult = 'restored' | 'disconnect_fallback' | 'failed' | 'not_online'

export interface RenewResult {
  ledger_tx_id: string
  receipt_no: string
  new_expires_at: string
  price_iqd: number
  coa_result: CoAResult
}

/** Renew a subscriber (FR-19). The idempotency key makes a retried submit safe. */
export function renewSubscriber(
  id: string,
  body: { profile_id?: string; note?: string },
  idempotencyKey: string,
): Promise<RenewResult> {
  return request<RenewResult>(`/subscribers/${id}/renew`, {
    method: 'POST',
    body,
    headers: { 'Idempotency-Key': idempotencyKey },
  })
}

export function refundRenewal(
  id: string,
  body: { ledger_tx_id: string; reason: string },
): Promise<RenewResult> {
  return request<RenewResult>(`/subscribers/${id}/refund`, { method: 'POST', body })
}

// --- balances (FR-20) -------------------------------------------------------

export function getManagerBalance(id: string): Promise<{ balance_iqd: number }> {
  return request<{ balance_iqd: number }>(`/managers/${id}/balance`)
}

export function topupManager(
  id: string,
  body: { amount_iqd: number; note?: string },
): Promise<{ ledger_tx_id: string; balance_iqd: number }> {
  return request(`/managers/${id}/topup`, { method: 'POST', body })
}

// --- ledger (FR-24) ---------------------------------------------------------

export type LedgerType =
  'renewal' | 'topup' | 'manual_payment' | 'voucher_redeem' | 'refund' | 'adjustment' | 'discount'

export interface LedgerItem {
  id: string
  at: string
  type: LedgerType
  amount_iqd: number
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
  unit_price_iqd: number
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
): Promise<{ voided_unused: number; credit_iqd: number }> {
  return request(`/vouchers/batches/${id}/void`, { method: 'POST' })
}

export function redeemVoucher(body: { code: string; subscriber_id: string }): Promise<RenewResult> {
  return request<RenewResult>('/vouchers/redeem', { method: 'POST', body })
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
