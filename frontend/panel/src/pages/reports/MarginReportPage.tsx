import { ErrorState, IQDAmount, LoadingState, useT } from '@hikrad/shared'

import { getMarginReport, getSiteMarginReport } from '../../api/reports'
import { PageHeader } from '../../components/PageHeader'
import { useAsync } from '../../hooks/useAsync'
import { DateRangeFilter } from './DateRangeFilter'
import { PrintHeader } from './PrintHeader'
import { useReportRange } from './useReportRange'

/**
 * Margin report (v2 phase 9, FR-72.3/FR-73/FR-75). A reseller-scoped caller
 * sees only reseller_margin (retail - their wholesale) — the backend omits
 * cost/owner_margin/unknown_cost_count entirely for them (never null, simply
 * absent), so this page renders whatever fields the response actually
 * carries rather than assuming the owner-only ones are always present.
 */
export function MarginReportPage() {
  const t = useT()
  const range = useReportRange()

  const { data, error, loading, reload } = useAsync(
    () => getMarginReport({ from: range.apiFrom, to: range.apiTo }),
    [range.apiFrom, range.apiTo],
  )
  const { data: sites } = useAsync(
    () => getSiteMarginReport({ from: range.apiFrom, to: range.apiTo }),
    [range.apiFrom, range.apiTo],
  )

  const showOwnerColumns = (data?.rows ?? []).some((r) => r.owner_margin !== undefined)

  return (
    <section className="print-report">
      <PrintHeader reportTitle={t('reports.margin.title')} />
      <PageHeader title={t('reports.margin.title')} subtitle={t('reports.margin.subtitle')} />
      <DateRangeFilter range={range} />

      {error ? (
        <ErrorState onRetry={reload} />
      ) : loading ? (
        <LoadingState />
      ) : (data?.rows.length ?? 0) === 0 ? (
        <p className="rounded-md border border-dashed border-surface-sunken p-8 text-center text-sm text-ink-muted">
          {t('reports.empty.title')}
        </p>
      ) : (
        <div className="overflow-x-auto rounded-md border border-surface-sunken">
          <table className="w-full text-sm">
            <thead className="bg-surface-sunken/40 text-start text-xs text-ink-muted">
              <tr>
                <th className="px-3 py-2 text-start font-medium">{t('vouchers.profile')}</th>
                <th className="px-3 py-2 text-end font-medium">{t('reports.margin.revenue')}</th>
                <th className="px-3 py-2 text-end font-medium">{t('reports.margin.wholesale')}</th>
                {showOwnerColumns ? (
                  <th className="px-3 py-2 text-end font-medium">{t('reports.margin.cost')}</th>
                ) : null}
                <th className="px-3 py-2 text-end font-medium">
                  {t('reports.margin.resellerMargin')}
                </th>
                {showOwnerColumns ? (
                  <th className="px-3 py-2 text-end font-medium">
                    {t('reports.margin.ownerMargin')}
                  </th>
                ) : null}
              </tr>
            </thead>
            <tbody>
              {(data?.rows ?? []).map((row) => (
                <tr
                  key={`${row.profile_id}-${row.currency}`}
                  className="border-t border-surface-sunken/60"
                >
                  <td className="px-3 py-2">{row.profile_name}</td>
                  <td className="px-3 py-2 text-end">
                    <IQDAmount amount={row.revenue} currency={row.currency} />
                  </td>
                  <td className="px-3 py-2 text-end">
                    <IQDAmount amount={row.wholesale} currency={row.currency} />
                  </td>
                  {showOwnerColumns ? (
                    <td className="px-3 py-2 text-end">
                      {row.cost !== undefined ? (
                        <IQDAmount amount={row.cost} currency={row.currency} />
                      ) : (
                        <span className="text-ink-muted">{t('reports.margin.unknown')}</span>
                      )}
                    </td>
                  ) : null}
                  <td className="px-3 py-2 text-end font-medium">
                    <IQDAmount amount={row.reseller_margin} currency={row.currency} />
                  </td>
                  {showOwnerColumns ? (
                    <td className="px-3 py-2 text-end font-medium">
                      {row.owner_margin !== undefined ? (
                        <IQDAmount amount={row.owner_margin} currency={row.currency} />
                      ) : (
                        <span className="text-ink-muted">{t('reports.margin.unknown')}</span>
                      )}
                    </td>
                  ) : null}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {(sites?.rows.length ?? 0) > 0 ? (
        <div className="mt-6">
          <h2 className="mb-2 text-sm font-semibold">{t('reports.margin.sites.title')}</h2>
          <div className="overflow-x-auto rounded-md border border-surface-sunken">
            <table className="w-full text-sm">
              <thead className="bg-surface-sunken/40 text-start text-xs text-ink-muted">
                <tr>
                  <th className="px-3 py-2 text-start font-medium">
                    {t('reports.margin.sites.site')}
                  </th>
                  <th className="px-3 py-2 text-end font-medium">{t('reports.margin.revenue')}</th>
                  <th className="px-3 py-2 text-end font-medium">
                    {t('reports.margin.sites.siteOverheads')}
                  </th>
                  <th className="px-3 py-2 text-end font-medium">
                    {t('reports.margin.sites.netMargin')}
                  </th>
                  <th className="px-3 py-2 text-end font-medium">
                    {t('reports.margin.sites.globalOverheads')}
                  </th>
                </tr>
              </thead>
              <tbody>
                {(sites?.rows ?? []).map((row) => (
                  <tr
                    key={`${row.nas_id}-${row.currency}`}
                    className="border-t border-surface-sunken/60"
                  >
                    <td className="px-3 py-2">{row.nas_name}</td>
                    <td className="px-3 py-2 text-end">
                      <IQDAmount amount={row.revenue} currency={row.currency} />
                    </td>
                    <td className="px-3 py-2 text-end">
                      <IQDAmount amount={row.site_overheads} currency={row.currency} />
                    </td>
                    <td className="px-3 py-2 text-end font-medium">
                      <IQDAmount amount={row.net_margin} currency={row.currency} />
                    </td>
                    <td className="px-3 py-2 text-end text-ink-muted">
                      <IQDAmount amount={row.global_overheads} currency={row.currency} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <p className="mt-2 text-xs text-ink-muted">{t('reports.margin.sites.globalNote')}</p>
        </div>
      ) : null}
    </section>
  )
}
