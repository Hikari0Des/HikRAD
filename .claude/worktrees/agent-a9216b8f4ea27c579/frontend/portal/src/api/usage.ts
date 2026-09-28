/** Usage graph + payment history — contract C2 (FR-41.3), self-scoped only. */
import { listPage, request, type Page } from './client'

export interface UsagePoint {
  t: string
  down: number
  up: number
}

export function getUsage(
  granularity: 'daily' | 'monthly',
  from?: string,
  to?: string,
): Promise<{ items: UsagePoint[] }> {
  return request<{ items: UsagePoint[] }>('/portal/usage', { query: { granularity, from, to } })
}

export type PaymentType =
  'renewal' | 'voucher_redeem' | 'portal-mock' | 'portal-zaincash' | 'card-trial' | 'refund'

export interface PortalPaymentItem {
  id: string
  at: string
  type: PaymentType
  amount: number
  currency: string
  source: string
  reference: string
}

export function getPayments(
  params: { cursor?: string; limit?: number } = {},
): Promise<Page<PortalPaymentItem>> {
  return listPage<PortalPaymentItem>('/portal/payments', params)
}
