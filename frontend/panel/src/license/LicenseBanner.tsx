import { useState } from 'react'
import { Link } from 'react-router-dom'

import { useFormatters, useT } from '@hikrad/shared'

import { useLicense } from './LicenseContext'

const DISMISS_KEY = 'hikrad:license:grace-dismissed'

/**
 * License banners (FR-50.3): the grace warning is persistent but dismissible
 * per browser session; once grace has actually expired the read-only banner
 * is never dismissible (mutations are genuinely blocked until fixed).
 */
export function LicenseBanner() {
  const t = useT()
  const { formatDate } = useFormatters()
  const { license, isReadOnly } = useLicense()
  const [dismissed, setDismissed] = useState(() => sessionStorage.getItem(DISMISS_KEY) === '1')

  if (!license) return null

  if (isReadOnly) {
    return (
      <div className="bg-danger px-4 py-2 text-center text-sm text-ink-inverse">
        {t('license.banner.readOnly')}{' '}
        <Link to="/license" className="underline">
          {t('license.banner.action')}
        </Link>
      </div>
    )
  }

  if (license.state === 'grace' && !dismissed) {
    return (
      <div className="flex items-center justify-center gap-3 bg-warn px-4 py-2 text-center text-sm text-ink-inverse">
        <span>
          {license.grace_expires_at
            ? t('license.banner.grace', { at: formatDate(license.grace_expires_at) })
            : t('license.banner.graceNoDate')}
        </span>
        <Link to="/license" className="underline">
          {t('license.banner.action')}
        </Link>
        <button
          type="button"
          className="opacity-80 hover:opacity-100"
          aria-label={t('ui.close')}
          onClick={() => {
            sessionStorage.setItem(DISMISS_KEY, '1')
            setDismissed(true)
          }}
        >
          ×
        </button>
      </div>
    )
  }

  return null
}
