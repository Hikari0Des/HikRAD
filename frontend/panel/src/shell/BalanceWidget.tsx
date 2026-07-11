import { IQDAmount, useT } from '@hikrad/shared'

import { getManagerBalance } from '../api/billing'
import { useAuth } from '../auth/AuthContext'
import { useAsync } from '../hooks/useAsync'

const LOW_BALANCE_IQD = 10000

/**
 * The signed-in manager's own balance in the header (Hassan, phone-first). Any
 * authenticated manager may read their own balance; admins with no wallet just
 * see nothing (the endpoint returns 0/absent → hidden). A low balance shows a
 * warning badge so a field agent notices before a renewal fails.
 */
export function BalanceWidget() {
  const t = useT()
  const { manager } = useAuth()
  const q = useAsync(
    () => (manager ? getManagerBalance(manager.id).catch(() => null) : Promise.resolve(null)),
    [manager?.id],
  )

  if (!q.data) return null
  const low = q.data.balance_iqd < LOW_BALANCE_IQD
  return (
    <div className="flex items-center gap-1.5 rounded-md bg-surface-sunken px-2.5 py-1 text-sm">
      <span className="text-xs text-ink-muted">{t('balance.mine')}</span>
      <span className={`font-semibold ${low ? 'text-danger' : ''}`}>
        <IQDAmount amount={q.data.balance_iqd} />
      </span>
      {low ? (
        <span className="rounded bg-danger/10 px-1.5 py-0.5 text-xs text-danger">
          {t('balance.low')}
        </span>
      ) : null}
    </div>
  )
}
