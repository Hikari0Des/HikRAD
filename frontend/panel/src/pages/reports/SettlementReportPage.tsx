import { useSearchParams } from 'react-router-dom'

import { ErrorState, IQDAmount, LoadingState, useT } from '@hikrad/shared'

import { downloadAuthorized } from '../../api/security'
import { getSettlementReport, settlementExportUrl } from '../../api/reports'
import { useAuth } from '../../auth/AuthContext'
import { PERM_EXPORT } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { TextInput } from '../../components/form'
import { PageHeader } from '../../components/PageHeader'
import { useAsync } from '../../hooks/useAsync'
import { DateRangeFilter } from './DateRangeFilter'
import { PrintHeader } from './PrintHeader'
import { useReportRange } from './useReportRange'

/**
 * Agent settlement report (FR-45.2) — Hassan's sign-off document: closing ≡
 * live balance when `to=now`, so this doubles as the printable collections
 * hand-off sheet.
 */
export function SettlementReportPage() {
  const t = useT()
  const { can, manager } = useAuth()
  const range = useReportRange()
  const [params, setParams] = useSearchParams()
  const managerId = params.get('manager_id') ?? manager?.id ?? ''

  const { data, error, loading, reload } = useAsync(
    () => getSettlementReport({ from: range.apiFrom, to: range.apiTo }, managerId || undefined),
    [range.apiFrom, range.apiTo, managerId],
  )

  function setManagerId(v: string) {
    const next = new URLSearchParams(params)
    if (v) next.set('manager_id', v)
    else next.delete('manager_id')
    setParams(next, { replace: true })
  }

  async function exportCsv() {
    await downloadAuthorized(
      settlementExportUrl({ from: range.apiFrom, to: range.apiTo }, managerId || undefined),
      'settlement.csv',
    )
  }

  return (
    <section className="print-report">
      <PrintHeader reportTitle={t('reports.settlement.title')} />
      <PageHeader
        title={t('reports.settlement.title')}
        subtitle={t('reports.settlement.subtitle')}
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
          <span className="mb-1 block text-ink-muted">{t('reports.settlement.manager')}</span>
          <TextInput
            dir="ltr"
            value={managerId}
            onChange={(e) => setManagerId(e.target.value)}
            placeholder={t('ledger.filter.managerId')}
          />
        </label>
      </div>

      {error ? (
        <ErrorState onRetry={reload} />
      ) : loading ? (
        <LoadingState />
      ) : !data ? null : (
        <div className="grid gap-3 sm:grid-cols-2">
          <Tile label={t('reports.settlement.opening')} amount={data.opening_iqd} />
          <Tile label={t('reports.settlement.topups')} amount={data.topups_iqd} positive />
          <Tile
            label={t('reports.settlement.renewals', { count: data.renewals.count })}
            amount={data.renewals.amount_iqd}
          />
          <Tile label={t('reports.settlement.refunds')} amount={data.refunds_iqd} />
          <div className="rounded-md border-2 border-brand bg-brand-soft p-4 sm:col-span-2">
            <p className="text-xs text-ink-muted">{t('reports.settlement.closing')}</p>
            <p className="text-3xl font-bold">
              <IQDAmount amount={data.closing_iqd} />
            </p>
          </div>
        </div>
      )}
    </section>
  )
}

function Tile({ label, amount, positive }: { label: string; amount: number; positive?: boolean }) {
  return (
    <div className="rounded-md border border-surface-sunken bg-surface-raised p-4">
      <p className="text-xs text-ink-muted">{label}</p>
      <p className={`text-xl font-semibold ${positive ? 'text-ok' : ''}`}>
        <IQDAmount amount={amount} />
      </p>
    </div>
  )
}
