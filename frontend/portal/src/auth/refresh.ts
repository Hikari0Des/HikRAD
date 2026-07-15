import { refresh as apiRefresh } from '../api/auth'
import { tokenStore } from './tokenStore'

/**
 * Access-token refresh for the portal session, mirroring the panel's
 * single-flight refresh (frontend/panel/src/auth/refresh.ts) so a burst of
 * parallel 401s collapses into one rotate.
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
    tokenStore.setSubscriber(res.subscriber)
    return true
  } catch {
    return false
  }
}
