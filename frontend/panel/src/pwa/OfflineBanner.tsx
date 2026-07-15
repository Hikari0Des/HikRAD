import { useT } from '@hikrad/shared'

import { useOnlineStatus } from './useOnlineStatus'

/** Honest offline state (FR-54.2), panel side — mirrors the portal's banner. */
export function OfflineBanner() {
  const t = useT()
  const online = useOnlineStatus()
  if (online) return null

  return (
    // Inline color: panel's tokens.css (frontend/panel/src/theme/**) is
    // outside this phase's src/pwa/** exception scope, so this avoids adding
    // a new Tailwind token dependency there.
    <div
      role="status"
      className="px-4 py-2 text-center text-xs font-medium"
      style={{ background: '#b47408', color: '#fff' }}
    >
      {t('pwa.offline')}
    </div>
  )
}
