import { useSearchParams } from 'react-router-dom'

import { ErrorState, IQDAmount, LoadingState, useT } from '@hikrad/shared'

import { listCurrencies } from '../../api/billing'
import { downloadAuthorized } from '../../api/security'
import { getSettlementReport, settlementExportUrl } from '../../api/reports'
import { useAuth } from '../../auth/AuthContext'
import { PERM_EXPORT } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { Select, TextInput } from '../../components/form'
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
  const currency = params.get('currency') || 'IQD'

  const { data: currencies } = useAsync(() => listCurrencies(), [])

  const { data, error, loading, reload } = useAsync(
    () =>
      getSettlementReport(
        { from: range.apiFrom, to: range.apiTo },
        managerId || undefined,
        currency,
      ),
    [range.apiFrom, range.apiTo, managerId, currency],
  )

  function setManagerId(v: string) {
    const next = new URLSearchParams(params)
    if (v) next.set('manager_id', v)
    else next.delete('manager_id')
    setParams(next, { replace: true })
  }

  function setCurrency(v: string) {
    const next = new URLSearchParams(params)
    next.set('currency', v)
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
        <div className="flex flex-wrap items-end gap-3">
          <label className="mb-4 text-xs print:hidden">
            <span className="mb-1 block text-ink-muted">{t('reports.settlement.currency')}</span>
            <Select value={currency} onChange={(e) => setCurrency(e.target.value)}>
              {(currencies?.items ?? [{ code: 'IQD' }]).map((c) => (
                <option key={c.code} value={c.code}>
                  {c.code}
                </option>
              ))}
            </Select>
          </label>
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
      </div>

      {error ? (
        <ErrorState onRetry={reload} />
      ) : loading ? (
        <LoadingState />
      ) : !data ? null : (
        <div className="grid gap-3 sm:grid-cols-2">
          <Tile
            label={t('reports.settlement.opening')}
            amount={data.opening}
            currency={data.currency}
          />
          <Tile
            label={t('reports.settlement.topups')}
            amount={data.topups}
            currency={data.currency}
            positive
          />
          <Tile
            label={t('reports.settlement.renewals', { count: data.renewals.count })}
            amount={data.renewals.amount}
            currency={data.currency}
          />
          <Tile
            label={t('reports.settlement.refunds')}
            amount={data.refunds}
            currency={data.currency}
          />
          <div className="rounded-md border-2 border-brand bg-brand-soft p-4 sm:col-span-2">
            <p className="text-xs text-ink-muted">{t('reports.settlement.closing')}</p>
            <p className="text-3xl font-bold">
              <IQDAmount amount={data.closing} currency={data.currency} />
            </p>
          </div>
        </div>
      )}
    </section>
  )
}

function Tile({
  label,
  amount,
  currency,
  positive,
}: {
  label: string
  amount: number
  currency: string
  positive?: boolean
}) {
  return (
    <div className="rounded-md border border-surface-sunken bg-surface-raised p-4">
      <p className="text-xs text-ink-muted">{label}</p>
      <p className={`text-xl font-semibold ${positive ? 'text-ok' : ''}`}>
        <IQDAmount amount={amount} currency={currency} />
      </p>
    </div>
  )
}
