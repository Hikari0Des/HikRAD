import { Link, useSearchParams } from 'react-router-dom'

import {
  EmptyState,
  ErrorState,
  LoadingState,
  StatusBadge,
  useFormatters,
  useT,
} from '@hikrad/shared'
import type { SubscriberStatus } from '@hikrad/shared'

import { downloadAuthorized } from '../../api/security'
import {
  getByProfileReport,
  getSubscriberReport,
  subscribersExportUrl,
  type SubscriberReportView,
} from '../../api/reports'
import { useAuth } from '../../auth/AuthContext'
import { PERM_EXPORT } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { Select } from '../../components/form'
import { PageHeader } from '../../components/PageHeader'
import { useAsync } from '../../hooks/useAsync'
import { DateRangeFilter } from './DateRangeFilter'
import { PrintHeader } from './PrintHeader'
import { useReportRange } from './useReportRange'

const VIEWS: SubscriberReportView[] = ['new', 'expired', 'expiring', 'by_profile', 'inactive']

/** Subscriber lifecycle reports (FR-46): worklist rows link to the user page. */
export function SubscribersReportPage() {
  const t = useT()
  const { formatDate } = useFormatters()
  const { can } = useAuth()
  const range = useReportRange()
  const [params, setParams] = useSearchParams()
  const view = (params.get('view') as SubscriberReportView) || 'expiring'
  const isByProfile = view === 'by_profile'

  const listAsync = useAsync(
    () =>
      isByProfile
        ? Promise.resolve(null)
        : getSubscriberReport(view as Exclude<SubscriberReportView, 'by_profile'>, {
            from: range.apiFrom,
            to: range.apiTo,
          }),
    [view, range.apiFrom, range.apiTo],
  )
  const byProfileAsync = useAsync(
    () =>
      isByProfile
        ? getByProfileReport({ from: range.apiFrom, to: range.apiTo })
        : Promise.resolve(null),
    [view, range.apiFrom, range.apiTo],
  )
  const { data, error, loading, reload } = isByProfile ? byProfileAsync : listAsync

  function setView(v: string) {
    const next = new URLSearchParams(params)
    next.set('view', v)
    setParams(next, { replace: true })
  }

  async function exportCsv() {
    await downloadAuthorized(
      subscribersExportUrl(view, { from: range.apiFrom, to: range.apiTo }),
      `subscribers_${view}.csv`,
    )
  }

  const rows = !isByProfile && data ? (data as { rows: unknown[] }).rows : []
  const profRows = isByProfile && data ? (data as { rows: unknown[] }).rows : []
  const isEmpty = isByProfile ? profRows.length === 0 : rows.length === 0

  return (
    <section className="print-report">
      <PrintHeader reportTitle={t(`reports.subscribers.view.${view}`)} />
      <PageHeader
        title={t('reports.subscribers.title')}
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
        {view !== 'by_profile' ? (
          <DateRangeFilter range={range} />
        ) : (
          <div className="print:hidden" />
        )}
        <label className="mb-4 text-xs print:hidden">
          <span className="mb-1 block text-ink-muted">{t('reports.subscribers.view')}</span>
          <Select value={view} onChange={(e) => setView(e.target.value)}>
            {VIEWS.map((v) => (
              <option key={v} value={v}>
                {t(`reports.subscribers.view.${v}`)}
              </option>
            ))}
          </Select>
        </label>
      </div>

      {error ? (
        <ErrorState onRetry={reload} />
      ) : loading ? (
        <LoadingState />
      ) : isEmpty ? (
        <EmptyState title={t('reports.empty.title')} body={t('reports.empty.body')} />
      ) : isByProfile ? (
        <div className="overflow-x-auto rounded-md border border-surface-sunken">
          <table className="w-full text-sm">
            <thead className="bg-surface-sunken/40 text-start text-xs text-ink-muted">
              <tr>
                <th className="px-3 py-2 text-start font-medium">{t('reports.col.profile')}</th>
                <th className="px-3 py-2 text-end font-medium">{t('reports.col.count')}</th>
              </tr>
            </thead>
            <tbody>
              {(profRows as { profile_id: string; profile_name: string; count: number }[]).map(
                (r) => (
                  <tr key={r.profile_id || 'none'} className="border-t border-surface-sunken/60">
                    <td className="px-3 py-2">{r.profile_name}</td>
                    <td className="px-3 py-2 text-end">{r.count}</td>
                  </tr>
                ),
              )}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="overflow-x-auto rounded-md border border-surface-sunken">
          <table className="w-full min-w-[36rem] text-sm">
            <thead className="bg-surface-sunken/40 text-start text-xs text-ink-muted">
              <tr>
                <th className="px-3 py-2 text-start font-medium">{t('reports.col.username')}</th>
                <th className="px-3 py-2 text-start font-medium">{t('reports.col.name')}</th>
                <th className="px-3 py-2 text-start font-medium">{t('reports.col.status')}</th>
                <th className="px-3 py-2 text-start font-medium">{t('reports.col.expiresAt')}</th>
              </tr>
            </thead>
            <tbody>
              {(
                rows as {
                  id: string
                  username: string
                  name: string
                  status: string
                  expires_at: string | null
                }[]
              ).map((r) => (
                <tr key={r.id} className="border-t border-surface-sunken/60">
                  <td className="px-3 py-2">
                    <Link
                      to={`/subscribers/${r.id}`}
                      className="text-brand hover:underline print:no-underline print:text-ink"
                    >
                      {r.username}
                    </Link>
                  </td>
                  <td className="px-3 py-2">{r.name || '—'}</td>
                  <td className="px-3 py-2">
                    <StatusBadge status={r.status as SubscriberStatus} />
                  </td>
                  <td className="px-3 py-2">{r.expires_at ? formatDate(r.expires_at) : '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}
