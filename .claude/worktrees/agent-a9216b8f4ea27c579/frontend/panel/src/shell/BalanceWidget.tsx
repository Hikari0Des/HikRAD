import { useEffect } from 'react'

import { IQDAmount, useT } from '@hikrad/shared'

import { getManagerBalance } from '../api/billing'
import { useAuth } from '../auth/AuthContext'
import { useAsync } from '../hooks/useAsync'
import { BALANCE_CHANGED_EVENT } from '../lib/balanceEvents'

const LOW_BALANCE_IQD = 10000
const REFRESH_INTERVAL_MS = 60_000

/**
 * The signed-in manager's own balance in the header (Hassan, phone-first). Any
 * authenticated manager may read their own balance; admins with no wallet just
 * see nothing (the endpoint returns 0/absent → hidden). A low balance shows a
 * warning badge so a field agent notices before a renewal fails.
 *
 * Stays live without a reload (item 7): refetches when any billing mutation
 * fires BALANCE_CHANGED_EVENT, when the tab regains focus (covers a top-up
 * done from another device/session), and on a slow interval as backstop.
 */
export function BalanceWidget() {
  const t = useT()
  const { manager } = useAuth()
  const q = useAsync(
    () => (manager ? getManagerBalance(manager.id).catch(() => null) : Promise.resolve(null)),
    [manager?.id],
  )

  const { reload } = q
  useEffect(() => {
    function onFocus() {
      if (document.visibilityState === 'visible') reload()
    }
    window.addEventListener(BALANCE_CHANGED_EVENT, reload)
    window.addEventListener('focus', onFocus)
    document.addEventListener('visibilitychange', onFocus)
    const id = setInterval(reload, REFRESH_INTERVAL_MS)
    return () => {
      window.removeEventListener(BALANCE_CHANGED_EVENT, reload)
      window.removeEventListener('focus', onFocus)
      document.removeEventListener('visibilitychange', onFocus)
      clearInterval(id)
    }
  }, [reload])

  if (!q.data) return null
  const low = q.data.balance < LOW_BALANCE_IQD
  return (
    <div className="flex items-center gap-1.5 rounded-md bg-surface-sunken px-2.5 py-1 text-sm">
      <span className="text-xs text-ink-muted">{t('balance.mine')}</span>
      <span className={`font-semibold ${low ? 'text-danger' : ''}`}>
        <IQDAmount amount={q.data.balance} currency={q.data.currency} />
      </span>
      {low ? (
        <span className="rounded bg-danger/10 px-1.5 py-0.5 text-xs text-danger">
          {t('balance.low')}
        </span>
      ) : null}
    </div>
  )
}
