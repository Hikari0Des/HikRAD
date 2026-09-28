import { refresh as apiRefresh } from '../api/auth'
import { tokenStore } from './tokenStore'

/**
 * Access-token refresh (contract C7/FR-29). The API client routes every 401 on
 * a non-/auth path through here: if we hold a refresh token we rotate it, store
 * the new pair, and report success so the original request replays with the
 * fresh access token. A rotating refresh chain means a revoked/stolen session
 * fails the rotate call — we return false and the client forces a logout.
 *
 * A single-flight guard collapses the burst of 401s a page's parallel requests
 * produce into one rotate, so we don't invalidate our own just-issued token.
 */
let inFlight: Promise<boolean> | null = null

export function tryRefresh(): Promise<boolean> {
  if (inFlight) return inFlight
  inFlight = doRefresh().finally(() => {
    inFlight = null
  })
  return inFlight
}

async function doRefresh(): Promise<boolean> {
  const token = tokenStore.getRefreshToken()
  if (!token) return false
  try {
    const res = await apiRefresh(token)
    tokenStore.setTokens(res.access_token, res.refresh_token)
    tokenStore.setManager(res.manager)
    return true
  } catch {
    // Invalid/revoked refresh token (or the server is down): the session is
    // over. Clearing happens in the client once we report failure.
    return false
  }
}
