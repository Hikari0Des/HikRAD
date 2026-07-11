import { useEffect } from 'react'
import { Link } from 'react-router-dom'

import { ErrorState, IQDAmount, LoadingState, useFormatters, useT } from '@hikrad/shared'

import { getDashboard, type Dashboard } from '../api/monitoring'
import { Sparkline } from '../components/Sparkline'
import { useAsync } from '../hooks/useAsync'

const REFRESH_MS = 15000

/** Omar's dashboard (FR-32): glanceable tiles, phone-first single column. */
export function DashboardPage() {
  const t = useT()
  const { data, error, loading, reload } = useAsync<Dashboard>(getDashboard, [])

  // Auto-refresh on the C5 cadence; pause when the tab is hidden.
  useEffect(() => {
    const id = setInterval(() => {
      if (!document.hidden) reload()
    }, REFRESH_MS)
    return () => clearInterval(id)
  }, [reload])

  if (error) return <ErrorState onRetry={reload} />
  if (loading && !data) return <LoadingState />
  if (!data) return null

  return (
    <section className="space-y-4">
      <h1 className="text-xl font-semibold">{t('dashboard.title')}</h1>

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <Tile title={t('dashboard.onlineNow')}>
          <div className="flex items-end justify-between gap-2">
            <span className="text-3xl font-bold">{data.online_now}</span>
            <Sparkline
              values={data.online_24h_sparkline.map((p) => p.online)}
              label={t('dashboard.onlineTrend')}
            />
          </div>
        </Tile>

        <Tile title={t('dashboard.revenueToday')} to="/ledger">
          <span className="text-3xl font-bold">
            <IQDAmount amount={data.revenue_today_iqd} />
          </span>
        </Tile>

        <Tile title={t('dashboard.radiusRps')}>
          <span className="text-3xl font-bold">{data.radius_rps}</span>
          <span className="ms-1 text-sm text-ink-muted">{t('dashboard.rps')}</span>
        </Tile>

        <Tile title={t('dashboard.subsActive')} to="/subscribers?status=active">
          <span className="text-3xl font-bold text-ok">{data.subs.active}</span>
        </Tile>
        <Tile title={t('dashboard.subsExpired')} to="/subscribers?status=expired">
          <span className="text-3xl font-bold text-danger">{data.subs.expired}</span>
        </Tile>
        <Tile title={t('dashboard.subsExpiring')} to="/subscribers?expiring=7d">
          <span className="text-3xl font-bold text-warn">{data.subs.expiring_7d}</span>
        </Tile>
      </div>

      <div>
        <h2 className="mb-2 text-sm font-semibold">{t('dashboard.pipeline')}</h2>
        <div
          className={`rounded-md border p-3 text-sm ${
            data.pipeline.invariant_ok
              ? 'border-ok/40 bg-ok/5 text-ok'
              : 'border-danger/40 bg-danger/5 text-danger'
          }`}
        >
          {data.pipeline.invariant_ok ? t('dashboard.pipelineOk') : t('dashboard.pipelineBroken')}
          <span className="ms-2 text-ink-muted">
            {t('dashboard.queueDepth', { n: data.pipeline.depth })}
          </span>
        </div>
      </div>

      <div>
        <h2 className="mb-2 text-sm font-semibold">{t('dashboard.nasHealth')}</h2>
        {data.nas_cards.length === 0 ? (
          <p className="text-sm text-ink-muted">{t('dashboard.noNas')}</p>
        ) : (
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
            {data.nas_cards.map((n) => (
              <Link
                key={n.id}
                to={`/nas/${n.id}/status`}
                className="flex items-center justify-between rounded-md border border-surface-sunken bg-surface-raised p-3 text-sm hover:border-brand"
              >
                <span className="font-medium">{n.name}</span>
                <NasStatusDot status={n.status} latency={n.latency_ms} />
              </Link>
            ))}
          </div>
        )}
      </div>
    </section>
  )
}

function Tile({ title, children, to }: { title: string; children: React.ReactNode; to?: string }) {
  const inner = (
    <div className="h-full rounded-lg border border-surface-sunken bg-surface-raised p-4">
      <p className="text-xs text-ink-muted">{title}</p>
      <div className="mt-2">{children}</div>
    </div>
  )
  return to ? (
    <Link to={to} className="block hover:opacity-90">
      {inner}
    </Link>
  ) : (
    inner
  )
}

function NasStatusDot({ status, latency }: { status: string; latency: number | null }) {
  const t = useT()
  const { formatNumber } = useFormatters()
  const color =
    status === 'up'
      ? 'bg-ok text-ok'
      : status === 'down'
        ? 'bg-danger text-danger'
        : 'bg-warn text-warn'
  return (
    <span className="flex items-center gap-1.5">
      <span
        className={`inline-block h-2 w-2 rounded-full ${color.split(' ')[0]}`}
        aria-hidden="true"
      />
      <span className={color.split(' ')[1]}>{t(`monitoring.status.${status}`)}</span>
      {latency != null ? (
        <span className="text-ink-muted">{t('monitoring.ms', { n: formatNumber(latency) })}</span>
      ) : null}
    </span>
  )
}
