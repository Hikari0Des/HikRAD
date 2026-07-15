import { Link } from 'react-router-dom'

import { ErrorState, StatusBadge, useFormatters, useT } from '@hikrad/shared'

import { getMyCardPayment } from '../api/cardPayments'
import { getMe } from '../api/me'
import { PullToRefresh } from '../components/PullToRefresh'
import { HomeSkeleton } from '../components/Skeleton'
import { useAsync } from '../hooks/useAsync'
import { formatBytes, formatKbps } from '../lib/units'

/**
 * Home (FR-41.2): the above-the-fold answer to Noor's questions — status,
 * days remaining, data consumed this cycle, current speed. Per Decision 21
 * this NEVER shows a quota ceiling, remaining balance, or progress-toward-
 * limit bar — only what she has consumed, as a plain figure.
 */
export function HomePage() {
  const t = useT()
  const { formatDate, formatNumber } = useFormatters()
  const me = useAsync(getMe, [])
  const card = useAsync(getMyCardPayment, [])

  async function refresh() {
    me.reload()
    card.reload()
  }

  if (me.loading) return <HomeSkeleton />

  if (me.error || !me.data) {
    return <ErrorState body={t('portal.home.loadError')} onRetry={me.reload} />
  }

  const data = me.data
  const cardPayment = card.data

  return (
    <PullToRefresh onRefresh={refresh}>
      <section className="flex flex-col gap-4 pb-24">
        <h1 className="text-lg font-semibold">{t('portal.home.title')}</h1>

        {cardPayment?.state === 'pending' ? (
          <div
            role="status"
            className="flex flex-col gap-1 rounded-xl border border-warning bg-warning/10 p-4 text-sm"
          >
            <p className="font-semibold">{t('portal.home.cardPendingTitle')}</p>
            <p className="text-ink-muted">
              {t('portal.home.cardPendingBody', {
                time: formatDate(cardPayment.trial_expires_at, { timeStyle: 'short' }),
              })}
            </p>
          </div>
        ) : null}

        {cardPayment?.state === 'rejected' ? (
          <div
            role="alert"
            className="flex flex-col gap-2 rounded-xl border border-danger bg-danger/10 p-4 text-sm"
          >
            <p className="font-semibold">{t('portal.home.cardRejectedTitle')}</p>
            {cardPayment.reject_reason ? (
              <p className="text-ink-muted">{cardPayment.reject_reason}</p>
            ) : null}
            <Link to="/renew" className="font-semibold text-brand underline">
              {t('portal.home.cardRejectedAction')}
            </Link>
          </div>
        ) : null}

        <div className="flex flex-col gap-3 rounded-xl bg-surface-raised p-4 shadow-sm">
          <div className="flex items-center justify-between gap-2 text-sm">
            <span className="text-ink-muted">{t('portal.home.status')}</span>
            <div className="flex items-center gap-2">
              <StatusBadge status={data.status} />
              {data.online_now ? (
                <span className="flex items-center gap-1 text-xs text-ok">
                  <span aria-hidden="true" className="h-2 w-2 rounded-full bg-ok" />
                  {t('portal.home.onlineNow')}
                </span>
              ) : null}
            </div>
          </div>
          <div className="flex items-center justify-between gap-2 text-sm">
            <span className="text-ink-muted">{t('portal.home.expires')}</span>
            <span>{data.expires_at ? formatDate(data.expires_at) : '—'}</span>
          </div>
          <div className="flex items-center justify-between gap-2 text-sm">
            <span className="text-ink-muted">{t('portal.home.daysLeft')}</span>
            <span className="font-semibold">{formatNumber(Math.max(0, data.days_left))}</span>
          </div>
          <div className="flex items-center justify-between gap-2 text-sm">
            <span className="text-ink-muted">{t('portal.home.profile')}</span>
            <span>{data.profile_name}</span>
          </div>
        </div>

        <div className="flex flex-col gap-2 rounded-xl bg-surface-raised p-4 shadow-sm">
          <h2 className="text-sm font-semibold">{t('portal.home.consumedTitle')}</h2>
          <p className="text-2xl font-bold">{formatBytes(data.usage.used_total, formatNumber)}</p>
          <p className="text-xs text-ink-muted">
            {t('portal.home.consumedBreakdown', {
              down: formatBytes(data.usage.used_down, formatNumber),
              up: formatBytes(data.usage.used_up, formatNumber),
            })}
          </p>
        </div>

        <div className="flex flex-col gap-2 rounded-xl bg-surface-raised p-4 shadow-sm">
          <h2 className="text-sm font-semibold">{t('portal.home.speedTitle')}</h2>
          <p className="text-sm">
            {t('portal.home.speedProfile', {
              down: formatKbps(data.speed.profile_down, formatNumber),
              up: formatKbps(data.speed.profile_up, formatNumber),
            })}
          </p>
          {data.online_now && data.speed.live_down !== undefined ? (
            <p className="text-xs text-ink-muted">
              {t('portal.home.speedLive', {
                down: formatKbps(data.speed.live_down, formatNumber),
                up: formatKbps(data.speed.live_up ?? 0, formatNumber),
              })}
            </p>
          ) : null}
        </div>
      </section>

      <Link
        to="/renew"
        className="fixed bottom-24 end-4 z-10 rounded-full bg-brand px-5 py-3 font-semibold text-ink-inverse shadow-lg transition-colors hover:bg-brand-strong"
      >
        {t('portal.home.renewAction')}
      </Link>
    </PullToRefresh>
  )
}
