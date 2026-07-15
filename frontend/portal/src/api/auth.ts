/**
 * Portal auth API (contract C2). `POST /portal/login` is frozen exactly; the
 * refresh/logout routes are not spelled out in C2 (only the schema note that
 * `portal_sessions` holds subscriber refresh tokens) — this mirrors the
 * panel's `/auth/refresh` + `/auth/logout` shape under the `/portal` prefix,
 * the narrowest assumption consistent with the frozen login route. Flagged as
 * a seam in the phase status note for D to confirm.
 */
import { ApiError, request } from './client'

export interface Subscriber {
  id: string
  username: string
  name: string
  language: 'en' | 'ar' | 'ku'
}

export interface LoginResponse {
  access_token: string
  refresh_token: string
  subscriber: Subscriber
}

/** Friendly, narrow set of login failure reasons the UI branches on. */
export type LoginOutcome =
  | { kind: 'session'; response: LoginResponse }
  | { kind: 'invalid_credentials' }
  | { kind: 'rate_limited'; retryAfterSeconds?: number }
  | { kind: 'disabled' }

export async function login(username: string, password: string): Promise<LoginOutcome> {
  try {
    const res = await request<LoginResponse>('/portal/login', { body: { username, password } })
    return { kind: 'session', response: res }
  } catch (err) {
    if (err instanceof ApiError) {
      if (err.code === 'rate_limited') {
        const retry = Number(err.message.match(/(\d+)/)?.[1])
        return {
          kind: 'rate_limited',
          retryAfterSeconds: Number.isFinite(retry) ? retry : undefined,
        }
      }
      if (err.code === 'subscriber_disabled' || err.status === 403) return { kind: 'disabled' }
      if (err.status === 401 || err.code === 'invalid_credentials') {
        return { kind: 'invalid_credentials' }
      }
    }
    throw err
  }
}

/** Rotate the refresh token. A failure means the session is dead. */
export function refresh(refreshToken: string): Promise<LoginResponse> {
  return request<LoginResponse>('/portal/refresh', { body: { refresh_token: refreshToken } })
}

/** Revoke the current portal session server-side (best-effort). */
export function logout(): Promise<void> {
  return request<void>('/portal/logout', { method: 'POST' })
}
