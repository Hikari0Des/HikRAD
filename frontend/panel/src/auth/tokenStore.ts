/** localStorage-backed token + manager persistence (keys namespaced hikrad.*). */
import type { Manager } from '../api/auth'

const ACCESS_KEY = 'hikrad.access_token'
const REFRESH_KEY = 'hikrad.refresh_token'
const MANAGER_KEY = 'hikrad.manager'

function get(key: string): string | null {
  try {
    return window.localStorage.getItem(key)
  } catch {
    return null
  }
}

function set(key: string, value: string): void {
  try {
    window.localStorage.setItem(key, value)
  } catch {
    // storage unavailable — session lives until reload
  }
}

function remove(key: string): void {
  try {
    window.localStorage.removeItem(key)
  } catch {
    // ignore
  }
}

export const tokenStore = {
  getAccessToken: (): string | null => get(ACCESS_KEY),
  getRefreshToken: (): string | null => get(REFRESH_KEY),

  setTokens(accessToken: string, refreshToken: string): void {
    set(ACCESS_KEY, accessToken)
    set(REFRESH_KEY, refreshToken)
  },

  getManager(): Manager | null {
    const raw = get(MANAGER_KEY)
    if (!raw) return null
    try {
      return JSON.parse(raw) as Manager
    } catch {
      return null
    }
  },

  setManager(manager: Manager): void {
    set(MANAGER_KEY, JSON.stringify(manager))
  },

  clear(): void {
    remove(ACCESS_KEY)
    remove(REFRESH_KEY)
    remove(MANAGER_KEY)
  },
}
