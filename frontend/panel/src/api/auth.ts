/** Auth API — contract C7/C2 (real Phase-2 endpoints; login shape unchanged). */
import { request } from './client'

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

export function login(username: string, password: string): Promise<LoginResponse> {
  return request<LoginResponse>('/auth/login', { body: { username, password } })
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
