/** Portal invoices (FR-171, consuming v4-9's invoices via billing's portal
 * seam). Read-only — paying an unpaid invoice reuses the existing Pay
 * screen (api/payMethods.ts). */
import { API_BASE, request } from './client'
import { tokenStore } from '../auth/tokenStore'

export interface InvoiceLine {
  description: string
  category: string
  quantity: number
  unit_price: number
  amount: number
}

export interface PortalInvoice {
  id: string
  invoice_no: string
  mode: string
  issue_date: string
  due_date: string
  tax_rate: number
  subtotal: number
  tax_amount: number
  total: number
  amount_paid: number
  status: 'unpaid' | 'partial' | 'due' | 'paid' | 'void'
  memo: string
  lines?: InvoiceLine[]
}

export function getInvoices(): Promise<{ items: PortalInvoice[] }> {
  return request<{ items: PortalInvoice[] }>('/portal/invoices')
}

export function getInvoice(id: string): Promise<PortalInvoice> {
  return request<PortalInvoice>(`/portal/invoices/${id}`)
}

/** The printable ("PDF" — browser print-to-PDF, matching how receipts
 * already work in this codebase) view. A plain `<a href>` can't carry the
 * Authorization header the endpoint requires, so this fetches the HTML
 * itself (authenticated, same as every other portal call) and opens it as a
 * same-origin blob: URL — the browser's own print dialog then produces the
 * PDF. Never a bare download link (nothing here saves a file on its own). */
export async function openInvoicePrint(id: string): Promise<void> {
  const token = tokenStore.getAccessToken()
  const res = await fetch(`${API_BASE}/portal/invoices/${id}/print`, {
    headers: token ? { Authorization: `Bearer ${token}` } : {},
  })
  if (!res.ok) throw new Error(`invoice print fetch failed: ${res.status}`)
  const html = await res.text()
  const blob = new Blob([html], { type: 'text/html' })
  const url = URL.createObjectURL(blob)
  window.open(url, '_blank', 'noopener')
  setTimeout(() => URL.revokeObjectURL(url), 60_000)
}
