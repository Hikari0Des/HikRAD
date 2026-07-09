import { tokenStore } from './tokenStore'

/**
 * Token refresh — Phase-1 STUB. Contract C7 only defines the login shape this
 * phase; the real refresh endpoint arrives with Agent A's Phase-2 auth work
 * and swaps in here without touching callers (the API client already routes
 * every 401 through this function).
 */
export async function tryRefresh(): Promise<boolean> {
  if (!tokenStore.getRefreshToken()) return false
  // Phase 2: POST /api/v1/auth/refresh with the refresh token, store the new
  // pair, return true. Until then a 401 always ends the session.
  return false
}
