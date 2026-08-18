/** Card groups REST client (v4-5, FR-136-137). */
import { API_BASE, request } from './client'
import { tokenStore } from '../auth/tokenStore'

export interface CardGroup {
  id: string
  name: string
  description: string
  username_prefix: string
  code_format: 'numeric' | 'alpha' | 'alphanumeric'
  code_length: number
  price: number
  rate_down_kbps: number
  rate_up_kbps: number
  transfer_mode: 'unlimited' | 'combined' | 'separate'
  transfer_total_bytes: number | null
  transfer_down_bytes: number | null
  transfer_up_bytes: number | null
  uptime_limit_seconds: number | null
  expire_after_seconds: number | null
  valid_until: string | null
  enabled: boolean
  source: 'native' | 'legacy_voucher'
  created_at: string
  card_count: number
  unused_count: number
  active_count: number
  exhausted_count: number
  disabled_count: number
}

export interface Card {
  id: string
  group_id: string
  state: 'unused' | 'active' | 'exhausted' | 'disabled'
  first_login_at: string | null
  created_at: string
  usage_down_bytes: number
  usage_up_bytes: number
  uptime_seconds: number
}

export interface CreateGroupInput {
  name: string
  description?: string
  username_prefix?: string
  code_format?: CardGroup['code_format']
  code_length?: number
  count: number
  price?: number
  rate_down_kbps?: number
  rate_up_kbps?: number
  transfer_mode?: CardGroup['transfer_mode']
  transfer_total_bytes?: number | null
  transfer_down_bytes?: number | null
  transfer_up_bytes?: number | null
  uptime_limit_seconds?: number | null
  expire_after_seconds?: number | null
  valid_until?: string | null
}

export function listCardGroups(): Promise<{ items: CardGroup[] }> {
  return request('/cards/groups')
}

export function getCardGroup(id: string): Promise<CardGroup> {
  return request(`/cards/groups/${id}`)
}

export function listCards(groupId: string): Promise<{ items: Card[] }> {
  return request(`/cards/groups/${groupId}/cards`)
}

export function createCardGroup(
  input: CreateGroupInput,
): Promise<{ group: CardGroup; codes: string[] }> {
  return request('/cards/groups', { method: 'POST', body: input })
}

export function setGroupEnabled(
  id: string,
  enabled: boolean,
): Promise<{ id: string; enabled: boolean }> {
  return request(`/cards/groups/${id}/${enabled ? 'enable' : 'disable'}`, {
    method: 'POST',
    body: {},
  })
}

export function extendGroup(
  id: string,
  validUntil: string,
): Promise<{ id: string; valid_until: string }> {
  return request(`/cards/groups/${id}/extend`, {
    method: 'POST',
    body: { valid_until: validUntil },
  })
}

export function addMoreCards(id: string, count: number): Promise<{ codes: string[] }> {
  return request(`/cards/groups/${id}/add-more`, { method: 'POST', body: { count } })
}

export function duplicateGroup(
  id: string,
  count: number,
): Promise<{ group: CardGroup; codes: string[] }> {
  return request(`/cards/groups/${id}/duplicate`, { method: 'POST', body: { count } })
}

export function resetCard(id: string): Promise<{ id: string }> {
  return request(`/cards/${id}/reset`, { method: 'POST', body: {} })
}

export function setCardEnabled(
  id: string,
  enabled: boolean,
): Promise<{ id: string; state: string }> {
  return request(`/cards/${id}/${enabled ? 'enable' : 'disable'}`, { method: 'POST', body: {} })
}

/** FR-137: branded print page for a freshly generated/added set of codes —
 *  returns the raw HTML the panel opens in a new tab for the operator's own
 *  browser print-to-PDF. Bypasses the shared `request()` helper (JSON-only)
 *  because the response body is HTML, not JSON. */
export async function printCardGroup(id: string, codes: string[]): Promise<string> {
  const token = tokenStore.getAccessToken()
  const res = await fetch(`${API_BASE}/cards/groups/${id}/print`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: JSON.stringify({ codes }),
  })
  if (!res.ok) throw new Error(`print failed: ${res.status}`)
  return res.text()
}

export function cardUsageCSVURL(groupId: string): string {
  return `/cards/groups/${groupId}/usage.csv`
}
