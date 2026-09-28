/** Self-service guest registration (FR-169) + OTP login for passwordless
 * hotspot accounts (FR-172) — both unauthenticated, both anonymous requests. */
import { ApiError, request } from './client'
import type { LoginResponse } from './auth'

export type SendOutcome =
  | { kind: 'sent'; alreadySent: boolean }
  | { kind: 'cooldown' }
  | { kind: 'daily_cap_reached' }
  | { kind: 'not_enabled' }
  | { kind: 'not_licensed' }
  | { kind: 'channel_not_configured' }

function toSendOutcome(err: unknown): SendOutcome {
  if (err instanceof ApiError) {
    switch (err.code) {
      case 'cooldown':
        return { kind: 'cooldown' }
      case 'daily_cap_reached':
        return { kind: 'daily_cap_reached' }
      case 'not_enabled':
        return { kind: 'not_enabled' }
      case 'not_licensed':
        return { kind: 'not_licensed' }
      case 'channel_not_configured':
        return { kind: 'channel_not_configured' }
    }
  }
  throw err
}

export async function registerRequest(
  name: string,
  phone: string,
  mac?: string,
  language?: string,
): Promise<SendOutcome> {
  try {
    const res = await request<{ sent: boolean; already_sent?: boolean }>('/portal/register', {
      body: { name, phone, mac: mac || undefined, language },
      anonymous: true,
    })
    return { kind: 'sent', alreadySent: Boolean(res.already_sent) }
  } catch (err) {
    return toSendOutcome(err)
  }
}

export async function registerVerify(
  phone: string,
  code: string,
  mac?: string,
): Promise<LoginResponse> {
  return request<LoginResponse>('/portal/register/verify', {
    body: { phone, code, mac: mac || undefined },
    anonymous: true,
  })
}

export async function otpRequest(username: string, mac?: string): Promise<SendOutcome> {
  try {
    const res = await request<{ sent: boolean; already_sent?: boolean }>('/portal/otp/request', {
      body: { username, mac: mac || undefined },
      anonymous: true,
    })
    return { kind: 'sent', alreadySent: Boolean(res.already_sent) }
  } catch (err) {
    return toSendOutcome(err)
  }
}

export function otpVerify(username: string, code: string, mac?: string): Promise<LoginResponse> {
  return request<LoginResponse>('/portal/otp/verify', {
    body: { username, code, mac: mac || undefined },
    anonymous: true,
  })
}
