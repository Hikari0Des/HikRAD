/** Auth API — contract C7 (dev stub this phase; same shape in Phase 2). */
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
