/** Voucher redemption — contract C2 (FR-42), the renewal path with no staff. */
import { ApiError, request } from './client'

export type CoAResult = 'restored' | 'disconnect_fallback' | 'failed' | 'not_online'

export interface RenewResult {
  ledger_tx_id: string
  receipt_no: string
  new_expires_at: string
  price_iqd: number
  coa_result: CoAResult
}

export type VoucherRedeemOutcome =
  { kind: 'ok'; result: RenewResult } | { kind: 'used' } | { kind: 'expired' } | { kind: 'invalid' }

export async function redeemVoucher(code: string): Promise<VoucherRedeemOutcome> {
  try {
    const result = await request<RenewResult>('/portal/vouchers/redeem', { body: { code } })
    return { kind: 'ok', result }
  } catch (err) {
    if (err instanceof ApiError) {
      if (err.code === 'voucher_used') return { kind: 'used' }
      if (err.code === 'voucher_expired') return { kind: 'expired' }
      if (err.code === 'voucher_invalid' || err.status === 404 || err.status === 422) {
        return { kind: 'invalid' }
      }
    }
    throw err
  }
}
