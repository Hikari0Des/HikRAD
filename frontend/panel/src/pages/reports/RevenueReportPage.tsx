import { useSearchParams } from 'react-router-dom'

import { EmptyState, ErrorState, IQDAmount, LoadingState, useT } from '@hikrad/shared'

import { downloadAuthorized } from '../../api/security'
import { getRevenueReport, revenueExportUrl, type RevenueGroupBy } from '../../api/reports'
import { useAuth } from '../../auth/AuthContext'
import { PERM_EXPORT } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { Select } from '../../components/form'
import { PageHeader } from '../../components/PageHeader'
import { useAsync } from '../../hooks/useAsync'
import { DateRangeFilter } from './DateRangeFilter'
import { PrintHeader } from './PrintHeader'
import { useReportRange } from './useReportRange'

const GROUP_BYS: RevenueGroupBy[] = ['day', 'month', 'manager', 'profile', 'method']

/** Revenue report (FR-45.1): headline total, then a grouped breakdown table. */
export function RevenueReportPage() {
  const t = useT()
  const { can } = useAuth()
  const range = useReportRange()
  const [params, setParams] = useSearchParams()
  const groupBy = (params.get('group_by') as RevenueGroupBy) || 'day'

  const { data, error, loading, reload } = useAsync(
    () => getRevenueReport({ from: range.apiFrom, to: range.apiTo }, groupBy),
    [range.apiFrom, range.apiTo, groupBy],
  )

  function setGroupBy(v: string) {
    const next = new URLSearchParams(params)
    next.set('group_by', v)
    setParams(next, { replace: true })
  }

  async function exportCsv() {
    await downloadAuthorized(
      revenueExportUrl({ from: range.apiFrom, to: range.apiTo }, groupBy),
      'revenue.csv',
    )
  }

  return (
    <section className="print-report">
      <PrintHeader reportTitle={t('reports.revenue.title')} />
      <PageHeader
        title={t('reports.revenue.title')}
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
          <span className="mb-1 block text-ink-muted">{t('reports.groupBy')}</span>
          <Select value={groupBy} onChange={(e) => setGroupBy(e.target.value)}>
            {GROUP_BYS.map((g) => (
              <option key={g} value={g}>
                {t(`reports.groupBy.${g}`)}
              </option>
            ))}
          </Select>
        </label>
      </div>

      {error ? (
        <ErrorState onRetry={reload} />
      ) : loading ? (
        <LoadingState />
      ) : !data || data.rows.length === 0 ? (
        <EmptyState title={t('reports.empty.title')} body={t('reports.empty.body')} />
      ) : (
        <>
          {/* Per-currency totals (v2 phase 4, FR-70.2) — never blended into one figure. */}
          <div className="mb-4 flex flex-wrap gap-3">
            {Object.entries(data.totals).map(([currency, total]) => (
              <div
                key={currency}
                className="rounded-md border border-surface-sunken bg-surface-raised p-4"
              >
                <p className="text-xs text-ink-muted">
                  {t('reports.revenue.total')} · {currency}
                </p>
                <p className="text-2xl font-semibold">
                  <IQDAmount amount={total} currency={currency} />
                </p>
              </div>
            ))}
          </div>
          <div className="overflow-x-auto rounded-md border border-surface-sunken">
            <table className="w-full text-sm">
              <thead className="bg-surface-sunken/40 text-start text-xs text-ink-muted">
                <tr>
                  <th className="px-3 py-2 text-start font-medium">
                    {t(`reports.groupBy.${groupBy}`)}
                  </th>
                  <th className="px-3 py-2 text-end font-medium">{t('reports.col.amount')}</th>
                  <th className="px-3 py-2 text-end font-medium">{t('reports.col.count')}</th>
                </tr>
              </thead>
              <tbody>
                {data.rows.map((row) => (
                  <tr
                    key={`${row.key}-${row.currency}`}
                    className="border-t border-surface-sunken/60"
                  >
                    <td className="px-3 py-2">{row.key || t('reports.unattributed')}</td>
                    <td className="px-3 py-2 text-end">
                      <IQDAmount amount={row.amount} currency={row.currency} />
                    </td>
                    <td className="px-3 py-2 text-end">{row.count}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
    </section>
  )
}
