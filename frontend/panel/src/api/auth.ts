/** Auth API — contract C7/C2. Login gains a 2FA branch in Phase 3 (FR-28). */
import { ApiError, request } from './client'

export interface Manager {
  id: string
  username: string
  role: string
}

export interface LoginResponse {
  access_token: string
  refresh_token: string
  manager: Manager
}

interface EnrollChallenge {
  totp_enrollment_required: true
  enrollment_token: string
}

/**
 * Result of a login attempt (FR-28.1). Either a session, a demand for a TOTP
 * code (account has 2FA — resubmit with `totpCode`), or a forced-enrolment
 * grant (2FA mandated but not yet set up — drive the enrolment flow with the
 * returned token, then log in again with a code).
 */
export type LoginOutcome =
  | { kind: 'session'; response: LoginResponse }
  | { kind: 'totp_required' }
  | { kind: 'enroll'; enrollmentToken: string }

export async function login(
  username: string,
  password: string,
  totpCode?: string,
): Promise<LoginOutcome> {
  try {
    const res = await request<LoginResponse | EnrollChallenge>('/auth/login', {
      body: { username, password, totp_code: totpCode },
    })
    if ('access_token' in res) return { kind: 'session', response: res }
    return { kind: 'enroll', enrollmentToken: res.enrollment_token }
  } catch (err) {
    if (err instanceof ApiError && err.code === 'totp_required') return { kind: 'totp_required' }
    throw err
  }
}

/**
 * Rotate the refresh token (FR-29). The server rotates the stored secret on
 * every call and revokes the whole chain if a stale secret is replayed, so a
 * failure here means the session is dead — the caller drops it.
 */
export function refresh(refreshToken: string): Promise<LoginResponse> {
  return request<LoginResponse>('/auth/refresh', { body: { refresh_token: refreshToken } })
}

/** Revoke the current panel session server-side (204, best-effort). */
export function logout(): Promise<void> {
  return request<void>('/auth/logout', { method: 'POST' })
}
