/** localStorage-backed subscriber token persistence (contract C1 `portal_sessions`).
 * Namespaced separately from the panel's `hikrad.*` keys so an installed panel
 * PWA and portal PWA on the same device never collide or cross-authenticate. */
import type { Subscriber } from '../api/auth'

const ACCESS_KEY = 'hikrad.portal.access_token'
const REFRESH_KEY = 'hikrad.portal.refresh_token'
const SUBSCRIBER_KEY = 'hikrad.portal.subscriber'

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

  getSubscriber(): Subscriber | null {
    const raw = get(SUBSCRIBER_KEY)
    if (!raw) return null
    try {
      return JSON.parse(raw) as Subscriber
    } catch {
      return null
    }
  },

  setSubscriber(subscriber: Subscriber): void {
    set(SUBSCRIBER_KEY, JSON.stringify(subscriber))
  },

  clear(): void {
    remove(ACCESS_KEY)
    remove(REFRESH_KEY)
    remove(SUBSCRIBER_KEY)
  },
}
