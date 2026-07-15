import { useT } from '@hikrad/shared'

import { useOnlineStatus } from './useOnlineStatus'

/**
 * Honest offline state (FR-54.2): "no connection — showing last data" rather
 * than a browser error. The service worker still serves the last cached
 * `/api/*` GET response underneath this banner (network-first + cache
 * fallback, public/sw.js) — this banner is what labels that data as stale
 * rather than fresh, and makes the no-offline-mutations rule explicit.
 */
export function OfflineBanner() {
  const t = useT()
  const online = useOnlineStatus()
  if (online) return null

  return (
    <div role="status" className="bg-warning px-4 py-2 text-center text-xs font-medium text-ink">
      {t('portal.pwa.offline')}
    </div>
  )
}
