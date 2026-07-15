import { Link, useSearchParams } from 'react-router-dom'

import { EmptyState, ErrorState, LoadingState, useFormatters, useT } from '@hikrad/shared'

import { downloadAuthorized } from '../../api/security'
import {
  getTopConsumers,
  getUsagePerNas,
  usageExportUrl,
  type PerNasRow,
  type TopConsumerRow,
} from '../../api/reports'
import { useAuth } from '../../auth/AuthContext'
import { PERM_EXPORT } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { Select } from '../../components/form'
import { PageHeader } from '../../components/PageHeader'
import { useAsync } from '../../hooks/useAsync'
import { formatBytes } from '../../lib/units'
import { DateRangeFilter } from './DateRangeFilter'
import { PrintHeader } from './PrintHeader'
import { useReportRange } from './useReportRange'

type View = 'top_consumers' | 'per_nas'

/** Usage reports (FR-47): top consumers + per-NAS totals over usage_daily rollups. */
export function UsageReportPage() {
  const t = useT()
  const { formatNumber } = useFormatters()
  const { can } = useAuth()
  const range = useReportRange()
  const [params, setParams] = useSearchParams()
  const view = (params.get('view') as View) || 'top_consumers'

  const { data, error, loading, reload } = useAsync<
    { rows: TopConsumerRow[] } | { rows: PerNasRow[] }
  >(
    () =>
      view === 'top_consumers'
        ? getTopConsumers({ from: range.apiFrom, to: range.apiTo })
        : getUsagePerNas({ from: range.apiFrom, to: range.apiTo }),
    [view, range.apiFrom, range.apiTo],
  )

  function setView(v: string) {
    const next = new URLSearchParams(params)
    next.set('view', v)
    setParams(next, { replace: true })
  }

  async function exportCsv() {
    await downloadAuthorized(
      usageExportUrl(view, { from: range.apiFrom, to: range.apiTo }),
      `usage_${view}.csv`,
    )
  }

  const rows = data?.rows ?? []

  return (
    <section className="print-report">
      <PrintHeader reportTitle={t(`reports.usage.view.${view}`)} />
      <PageHeader
        title={t('reports.usage.title')}
        actions={
          can(PERM_EXPORT) ? (
            <Button
              size="sm"
              variant="secondary"
              className="print:hidden"
              onClick={() => void exportCsv()}
            >
              {t('reports.export')}
            </Button>
          ) : null
        }
      />
      <div className="flex flex-wrap items-end justify-between gap-3">
        <DateRangeFilter range={range} />
        <label className="mb-4 text-xs print:hidden">
          <span className="mb-1 block text-ink-muted">{t('reports.usage.view')}</span>
          <Select value={view} onChange={(e) => setView(e.target.value)}>
            <option value="top_consumers">{t('reports.usage.view.top_consumers')}</option>
            <option value="per_nas">{t('reports.usage.view.per_nas')}</option>
          </Select>
        </label>
      </div>

      {error ? (
        <ErrorState onRetry={reload} />
      ) : loading ? (
        <LoadingState />
      ) : rows.length === 0 ? (
        <EmptyState title={t('reports.empty.title')} body={t('reports.empty.body')} />
      ) : (
        <div className="overflow-x-auto rounded-md border border-surface-sunken">
          <table className="w-full text-sm">
            <thead className="bg-surface-sunken/40 text-start text-xs text-ink-muted">
              <tr>
                <th className="px-3 py-2 text-start font-medium">
                  {view === 'top_consumers' ? t('reports.col.username') : t('reports.col.nas')}
                </th>
                <th className="px-3 py-2 text-end font-medium">{t('reports.col.down')}</th>
                <th className="px-3 py-2 text-end font-medium">{t('reports.col.up')}</th>
              </tr>
            </thead>
            <tbody>
              {view === 'top_consumers'
                ? (
                    rows as {
                      subscriber_id: string
                      username: string
                      down_bytes: number
                      up_bytes: number
                    }[]
                  ).map((r) => (
                    <tr key={r.subscriber_id} className="border-t border-surface-sunken/60">
                      <td className="px-3 py-2">
                        <Link
                          to={`/subscribers/${r.subscriber_id}`}
                          className="text-brand hover:underline print:no-underline print:text-ink"
                        >
                          {r.username}
                        </Link>
                      </td>
                      <td className="px-3 py-2 text-end">
                        {formatBytes(r.down_bytes, formatNumber)}
                      </td>
                      <td className="px-3 py-2 text-end">
                        {formatBytes(r.up_bytes, formatNumber)}
                      </td>
                    </tr>
                  ))
                : (
                    rows as {
                      nas_id: string
                      nas_name: string
                      down_bytes: number
                      up_bytes: number
                    }[]
                  ).map((r) => (
                    <tr key={r.nas_id || 'none'} className="border-t border-surface-sunken/60">
                      <td className="px-3 py-2">{r.nas_name}</td>
                      <td className="px-3 py-2 text-end">
                        {formatBytes(r.down_bytes, formatNumber)}
                      </td>
                      <td className="px-3 py-2 text-end">
                        {formatBytes(r.up_bytes, formatNumber)}
                      </td>
                    </tr>
                  ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}
