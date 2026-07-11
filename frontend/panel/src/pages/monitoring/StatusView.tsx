import { ChartContainer, ErrorState, LoadingState, useFormatters, useT } from '@hikrad/shared'

import { type ProbeHistory } from '../../api/monitoring'
import { useAsync } from '../../hooks/useAsync'

/**
 * Shared probe-history view for a NAS or a monitored device (FR-33/FR-60): a
 * current-status badge, a latency chart (LTR inside RTL), and a downtime log.
 * The NAS and device status pages reuse this — the only difference is which
 * fetcher and title the parent passes and that a device shows no RADIUS
 * affordances.
 */
export function StatusView({
  title,
  subtitle,
  fetcher,
}: {
  title: string
  subtitle?: string
  fetcher: () => Promise<ProbeHistory>
}) {
  const t = useT()
  const { formatDate, formatNumber } = useFormatters()
  const q = useAsync<ProbeHistory>(fetcher, [title])

  if (q.error) return <ErrorState onRetry={q.reload} />
  if (q.loading || !q.data) return <LoadingState />

  const latencies = q.data.series.filter((s) => s.latency_ms != null).map((s) => s.latency_ms!)
  const maxLat = Math.max(...latencies, 1)

  return (
    <section className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">{title}</h1>
          {subtitle ? <p className="text-sm text-ink-muted">{subtitle}</p> : null}
        </div>
        <StatusBadgeMon status={q.data.status} />
      </div>

      <div className="rounded-md border border-surface-sunken bg-surface-raised p-4">
        <h2 className="mb-2 text-sm font-semibold">{t('monitoring.latency')}</h2>
        {latencies.length < 2 ? (
          <p className="text-sm text-ink-muted">{t('monitoring.noData')}</p>
        ) : (
          <ChartContainer>
            <svg
              viewBox="0 0 300 60"
              className="h-16 w-full text-brand"
              role="img"
              aria-label={t('monitoring.latency')}
            >
              <polyline
                points={latencies
                  .map(
                    (v, i) =>
                      `${(i * (300 / (latencies.length - 1))).toFixed(1)},${(60 - (v / maxLat) * 56 - 2).toFixed(1)}`,
                  )
                  .join(' ')}
                fill="none"
                stroke="currentColor"
                strokeWidth="1.5"
              />
            </svg>
          </ChartContainer>
        )}
        {latencies.length > 0 ? (
          <p className="mt-1 text-xs text-ink-muted">
            {t('monitoring.latencyPeak', { n: formatNumber(maxLat) })}
          </p>
        ) : null}
      </div>

      <div className="rounded-md border border-surface-sunken bg-surface-raised p-4">
        <h2 className="mb-2 text-sm font-semibold">{t('monitoring.downtimeLog')}</h2>
        {q.data.downtime.length === 0 ? (
          <p className="text-sm text-ink-muted">{t('monitoring.noDowntime')}</p>
        ) : (
          <ul className="space-y-1 text-sm">
            {q.data.downtime.map((d, i) => (
              <li
                key={i}
                className="flex items-center justify-between border-b border-surface-sunken/60 py-1.5"
              >
                <span>{formatDate(d.from)}</span>
                <span className="text-ink-muted">
                  {d.to ? formatDate(d.to) : t('monitoring.ongoing')} ·{' '}
                  {t('monitoring.durationS', { n: formatNumber(d.seconds) })}
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </section>
  )
}

export function StatusBadgeMon({ status }: { status: string }) {
  const t = useT()
  const styles: Record<string, string> = {
    up: 'bg-ok/10 text-ok',
    down: 'bg-danger/10 text-danger',
    degraded: 'bg-warn/10 text-warn',
    unknown: 'bg-surface-sunken text-ink-muted',
  }
  return (
    <span className={`rounded px-2 py-1 text-xs font-medium ${styles[status] ?? styles.unknown}`}>
      {t(`monitoring.status.${status}`)}
    </span>
  )
}
