import { Link } from 'react-router-dom'

import { EmptyState, ErrorState, LoadingState, useFormatters, useT } from '@hikrad/shared'

import { getDailyTraffic, getTop10, getTrends, type Top10Row } from '../../api/reports'
import { MultiLineChart, type LineSeries } from '../../components/charts/MultiLineChart'
import { StackedBarChart } from '../../components/charts/StackedBarChart'
import { PageHeader } from '../../components/PageHeader'
import { useAsync } from '../../hooks/useAsync'
import { formatBytes } from '../../lib/units'
import { PrintHeader } from './PrintHeader'
import { TrendRangeFilter } from './TrendRangeFilter'
import { useReportRange } from './useReportRange'

function Top10List({ title, rows }: { title: string; rows: Top10Row[] }) {
  const { formatNumber } = useFormatters()
  const t = useT()
  if (rows.length === 0) {
    return (
      <div className="rounded-md border border-border p-3">
        <p className="mb-2 text-sm font-medium">{title}</p>
        <p className="text-xs text-ink-muted">{t('reports.empty.title')}</p>
      </div>
    )
  }
  return (
    <div className="rounded-md border border-border p-3">
      <p className="mb-2 text-sm font-medium">{title}</p>
      <ol className="space-y-1 text-sm">
        {rows.map((r, i) => (
          <li key={r.subscriber_id} className="flex items-center justify-between gap-2">
            <span className="flex min-w-0 items-center gap-2">
              <span className="w-4 shrink-0 text-end text-xs text-ink-muted">{i + 1}</span>
              <Link
                to={`/subscribers/${r.subscriber_id}`}
                className="truncate text-brand hover:underline"
              >
                {r.username}
              </Link>
            </span>
            <span className="shrink-0 text-ink-muted">{formatBytes(r.bytes, formatNumber)}</span>
          </li>
        ))}
      </ol>
    </div>
  )
}

/** FR-173: trend lines, daily traffic (stacked bars), and the four top-10
 * leaderboards — the new "trends & traffic" report, alongside the 5
 * pre-existing reports (revenue/settlement/subscribers/usage/margin), which
 * keep their own shapes unchanged. */
export function TrendsReportPage() {
  const t = useT()
  const { formatNumber } = useFormatters()
  const range = useReportRange()

  const trends = useAsync(
    () => getTrends({ from: range.apiFrom, to: range.apiTo }),
    [range.apiFrom, range.apiTo],
  )
  const traffic = useAsync(
    () => getDailyTraffic({ from: range.apiFrom, to: range.apiTo }),
    [range.apiFrom, range.apiTo],
  )
  const top10 = useAsync(() => getTop10(), [])

  const trendRows = trends.data?.rows ?? []
  const trafficRows = traffic.data?.rows ?? []

  const series: LineSeries[] = [
    {
      key: 'active',
      label: t('reports.trends.series.active'),
      colorClass: 'text-chart-1',
      values: trendRows.map((r) => r.active_subscribers),
    },
    {
      key: 'online',
      label: t('reports.trends.series.online'),
      colorClass: 'text-chart-2',
      values: trendRows.map((r) => r.online_sessions),
    },
    {
      key: 'expired',
      label: t('reports.trends.series.expired'),
      colorClass: 'text-chart-3',
      values: trendRows.map((r) => r.expired_subscribers),
    },
  ]

  const loading = trends.loading || traffic.loading || top10.loading
  const error = trends.error || traffic.error || top10.error

  return (
    <section className="print-report">
      <PrintHeader reportTitle={t('reports.trends.title')} />
      <PageHeader title={t('reports.trends.title')} subtitle={t('reports.trends.subtitle')} />
      <TrendRangeFilter range={range} />

      {error ? (
        <ErrorState
          onRetry={() => {
            trends.reload()
            traffic.reload()
            top10.reload()
          }}
        />
      ) : loading ? (
        <LoadingState />
      ) : (
        <div className="space-y-6">
          <div className="rounded-md border border-border bg-surface-raised p-4">
            <p className="mb-3 text-sm font-medium">{t('reports.trends.chart.title')}</p>
            {trendRows.length < 2 ? (
              <EmptyState title={t('reports.empty.title')} body={t('reports.empty.body')} />
            ) : (
              <MultiLineChart series={series} labels={trendRows.map((r) => r.date)} />
            )}
          </div>

          <div className="rounded-md border border-border bg-surface-raised p-4">
            <p className="mb-3 text-sm font-medium">{t('reports.trends.dailyTraffic.title')}</p>
            {trafficRows.length === 0 ? (
              <EmptyState title={t('reports.empty.title')} body={t('reports.empty.body')} />
            ) : (
              <StackedBarChart
                points={trafficRows.map((r) => ({ label: r.date, a: r.down_bytes, b: r.up_bytes }))}
                aLabel={t('reports.col.down')}
                bLabel={t('reports.col.up')}
                totalLabel={t('reports.col.total')}
                formatValue={(v) => formatBytes(v, formatNumber)}
              />
            )}
          </div>

          <div>
            <p className="mb-3 text-sm font-medium">{t('reports.trends.top10.title')}</p>
            <div className="grid gap-3 sm:grid-cols-2">
              <Top10List
                title={t('reports.trends.top10.downloadToday')}
                rows={top10.data?.download_today ?? []}
              />
              <Top10List
                title={t('reports.trends.top10.uploadToday')}
                rows={top10.data?.upload_today ?? []}
              />
              <Top10List
                title={t('reports.trends.top10.downloadMonth')}
                rows={top10.data?.download_month ?? []}
              />
              <Top10List
                title={t('reports.trends.top10.uploadMonth')}
                rows={top10.data?.upload_month ?? []}
              />
            </div>
          </div>
        </div>
      )}
    </section>
  )
}
