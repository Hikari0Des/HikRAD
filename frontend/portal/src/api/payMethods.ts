/**
 * Unified Pay screen API (v2-2, contracts C4/C5/C13, FR-78). Replaces the old
 * gateway list + separate scratch-card submission with one tile list and one
 * ticket-submission endpoint. `submitTicket` posts multipart/form-data (JSON
 * fields alongside file parts) so it cannot go through `request` (which
 * always JSON-encodes) — it duplicates request's auth/401-refresh handling
 * rather than importing it, since FormData bodies need their own branch.
 */
import { tokenStore } from '../auth/tokenStore'
import { tryRefresh } from '../auth/refresh'
import { ApiError, NetworkError, UNAUTHORIZED_EVENT, API_BASE, request } from './client'

export type PayMethodKind = 'provider' | 'scratch_card' | 'voucher'

export interface PayMethod {
  key: string
  kind: PayMethodKind
  provider_name?: string
  account_details?: string
  instructions_text?: string
}

export function listPayMethods(): Promise<{ items: PayMethod[] }> {
  return request<{ items: PayMethod[] }>('/portal/pay-methods')
}

export type TicketState = 'pending' | 'approved' | 'rejected'

export interface MyTicket {
  id: string
  method_key: string
  state: TicketState
  trial_granted: boolean
  trial_expires_at: string
  reject_reason?: string
  created_at: string
}

export function getLatestTicket(): Promise<MyTicket | null> {
  return request<MyTicket | null>('/portal/payment-tickets/latest')
}

export interface SubmitTicketParams {
  methodKey: string
  // Provider fields:
  amount?: number
  transferReference?: string
  transferDate?: string
  note?: string
  attachments?: File[]
  // Scratch-card fields:
  cardType?: string
  cardCode?: string
}

export interface TicketSubmitResponse {
  id: string
  state: TicketState
  trial_granted: boolean
  trial_expires_at?: string
}

export type TicketSubmitOutcome =
  | { kind: 'ok'; result: TicketSubmitResponse }
  | { kind: 'method_not_allowed' }
  | { kind: 'ticket_pending' }
  | { kind: 'card_code_invalid' }
  | { kind: 'no_profile' }

export async function submitTicket(params: SubmitTicketParams): Promise<TicketSubmitOutcome> {
  const form = new FormData()
  form.set('method_key', params.methodKey)
  if (params.amount !== undefined) form.set('amount', String(params.amount))
  if (params.transferReference) form.set('transfer_reference', params.transferReference)
  if (params.transferDate) form.set('transfer_date', params.transferDate)
  if (params.note) form.set('note', params.note)
  if (params.cardType) form.set('card_type', params.cardType)
  if (params.cardCode) form.set('card_code', params.cardCode)
  for (const file of params.attachments ?? []) {
    form.append('attachments', file)
  }

  try {
    const result = await multipartRequest<TicketSubmitResponse>('/portal/payment-tickets', form)
    return { kind: 'ok', result }
  } catch (err) {
    if (err instanceof ApiError) {
      if (err.code === 'method_not_allowed') return { kind: 'method_not_allowed' }
      if (err.code === 'ticket_pending') return { kind: 'ticket_pending' }
      if (err.code === 'card_code_invalid') return { kind: 'card_code_invalid' }
      if (err.code === 'no_profile') return { kind: 'no_profile' }
    }
    throw err
  }
}

async function multipartRequest<T>(path: string, form: FormData): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  const token = tokenStore.getAccessToken()
  if (token) headers['Authorization'] = `Bearer ${token}`

  let res: Response
  try {
    res = await fetch(API_BASE + path, { method: 'POST', headers, body: form })
  } catch (err) {
    throw new NetworkError(err)
  }

  if (res.status === 401) {
    const recovered = await tryRefresh()
    if (recovered) return multipartRequest<T>(path, form)
    tokenStore.clear()
    window.dispatchEvent(new Event(UNAUTHORIZED_EVENT))
  }

  if (!res.ok) {
    let payload: unknown
    try {
      payload = await res.json()
    } catch {
      payload = undefined
    }
    const envelope = payload as { error?: { code?: string; message?: string } } | undefined
    throw new ApiError(
      res.status,
      envelope?.error?.code ?? 'unknown',
      envelope?.error?.message ?? `unexpected response (HTTP ${res.status})`,
    )
  }
  return (await res.json()) as T
}
