/** localStorage-backed subscriber token persistence (contract C1 `portal_sessions`).
 * Namespaced separately from the panel's `hikrad.*` keys so an installed panel
 * PWA and portal PWA on the same device never collide or cross-authenticate. */
import type { SessionScope, Subscriber } from '../api/auth'

const ACCESS_KEY = 'hikrad.portal.access_token'
const REFRESH_KEY = 'hikrad.portal.refresh_token'
const SUBSCRIBER_KEY = 'hikrad.portal.subscriber'
const SCOPE_KEY = 'hikrad.portal.scope'

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

  // Step-up (FR-167) returns a new access token only — the refresh token is
  // untouched (same session, same hard cap), so refreshToken is optional
  // here and a missing one simply leaves whatever was already stored.
  setTokens(accessToken: string, refreshToken?: string): void {
    set(ACCESS_KEY, accessToken)
    if (refreshToken) set(REFRESH_KEY, refreshToken)
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

  // Defaults to "full" so a token minted before this phase (or any response
  // shape that omits scope) is never mistaken for a reduced grant — the
  // server is always the source of truth for what a token can actually do
  // (FR-167: scope is enforced server-side regardless of what the client
  // renders), this is only ever used to decide what UI to SHOW.
  getScope(): SessionScope {
    return get(SCOPE_KEY) === 'reduced' ? 'reduced' : 'full'
  },

  setScope(scope: SessionScope): void {
    set(SCOPE_KEY, scope)
  },

  clear(): void {
    remove(ACCESS_KEY)
    remove(REFRESH_KEY)
    remove(SUBSCRIBER_KEY)
    remove(SCOPE_KEY)
  },
}
