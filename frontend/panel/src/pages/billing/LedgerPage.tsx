import { useMemo, useState } from 'react'

import { ErrorState, IQDAmount, LoadingState, useFormatters, useT } from '@hikrad/shared'

import { downloadAuthorized } from '../../api/security'
import {
  ledgerExportUrl,
  listCurrencies,
  listLedger,
  refundRenewal,
  type LedgerFilters,
  type LedgerItem,
  type LedgerType,
} from '../../api/billing'
import { useAuth } from '../../auth/AuthContext'
import { PERM_EXPORT } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { Field, Select, TextInput, Textarea } from '../../components/form'
import { Modal } from '../../components/Modal'
import { PageHeader } from '../../components/PageHeader'
import { useToast } from '../../components/Toast'
import { useAsync } from '../../hooks/useAsync'
import { usePaginated } from '../../hooks/usePaginated'
import { indexReversals, runningBalances } from '../../lib/ledgerPairing'

const TYPES: LedgerType[] = [
  'renewal',
  'topup',
  'manual_payment',
  'voucher_redeem',
  'refund',
  'adjustment',
  'discount',
]

/** Ledger view (FR-24) + refund flow (FR-25). */
export function LedgerPage() {
  const t = useT()
  const { can } = useAuth()
  const [filters, setFilters] = useState<LedgerFilters>({})
  const [refundRow, setRefundRow] = useState<LedgerItem | null>(null)
  const { data: currencies } = useAsync(() => listCurrencies(), [])

  const key = JSON.stringify(filters)
  const page = usePaginated<LedgerItem>((cursor) => listLedger({ cursor }, filters), key)

  const reversals = useMemo(() => indexReversals(page.items), [page.items])
  // Running balance only makes sense filtered to a single manager AND a
  // single currency (v2 phase 4) — a mixed-currency feed has no single sum.
  const balances = useMemo(
    () => (filters.manager_id && filters.currency ? runningBalances(page.items) : null),
    [filters.manager_id, filters.currency, page.items],
  )

  function setField(k: keyof LedgerFilters, v: string) {
    setFilters((prev) => ({ ...prev, [k]: v || undefined }))
  }

  async function exportCsv() {
    try {
      await downloadAuthorized(ledgerExportUrl(filters), 'ledger.csv')
    } catch {
      /* toast handled below via row-level actions; keep export best-effort */
    }
  }

  return (
    <section>
      <PageHeader
        title={t('ledger.title')}
        actions={
          can(PERM_EXPORT) ? (
            <Button size="sm" variant="secondary" onClick={() => void exportCsv()}>
              {t('ledger.export')}
            </Button>
          ) : null
        }
      />

      {/* Filter chips */}
      <div className="mb-4 grid grid-cols-2 gap-2 sm:grid-cols-4">
        <label className="text-xs">
          <span className="mb-1 block text-ink-muted">{t('ledger.filter.type')}</span>
          <Select value={filters.type ?? ''} onChange={(e) => setField('type', e.target.value)}>
            <option value="">{t('ledger.filter.allTypes')}</option>
            {TYPES.map((ty) => (
              <option key={ty} value={ty}>
                {t(`ledger.type.${ty}`)}
              </option>
            ))}
          </Select>
        </label>
        <label className="text-xs">
          <span className="mb-1 block text-ink-muted">{t('ledger.filter.manager')}</span>
          <TextInput
            value={filters.manager_id ?? ''}
            onChange={(e) => setField('manager_id', e.target.value)}
            placeholder={t('ledger.filter.managerId')}
            dir="ltr"
          />
        </label>
        <label className="text-xs">
          <span className="mb-1 block text-ink-muted">{t('ledger.filter.currency')}</span>
          <Select
            value={filters.currency ?? ''}
            onChange={(e) => setField('currency', e.target.value)}
          >
            <option value="">{t('ledger.filter.allCurrencies')}</option>
            {(currencies?.items ?? []).map((c) => (
              <option key={c.code} value={c.code}>
                {c.code}
              </option>
            ))}
          </Select>
        </label>
        <label className="text-xs">
          <span className="mb-1 block text-ink-muted">{t('ledger.filter.from')}</span>
          <TextInput
            type="date"
            value={filters.from ?? ''}
            onChange={(e) => setField('from', e.target.value)}
          />
        </label>
        <label className="text-xs">
          <span className="mb-1 block text-ink-muted">{t('ledger.filter.to')}</span>
          <TextInput
            type="date"
            value={filters.to ?? ''}
            onChange={(e) => setField('to', e.target.value)}
          />
        </label>
      </div>

      {page.error ? (
        <ErrorState onRetry={page.reset} />
      ) : (
        <div className="overflow-x-auto rounded-md border border-surface-sunken">
          <table className="w-full min-w-[40rem] text-sm">
            <thead className="bg-surface-sunken/40 text-start text-xs text-ink-muted">
              <tr>
                <Th>{t('ledger.col.at')}</Th>
                <Th>{t('ledger.col.type')}</Th>
                <Th className="text-end">{t('ledger.col.amount')}</Th>
                {balances ? <Th className="text-end">{t('ledger.col.balance')}</Th> : null}
                <Th>{t('ledger.col.note')}</Th>
                <Th />
              </tr>
            </thead>
            <tbody>
              {page.items.map((row) => (
                <LedgerRow
                  key={row.id}
                  row={row}
                  showBalance={balances !== null}
                  balance={balances?.get(row.id)}
                  isReversal={reversals.isReversal.has(row.id)}
                  reversedBy={reversals.reversedBy.get(row.id)}
                  canRefund={can('refund') && !!row.subscriber_id}
                  onRefund={() => setRefundRow(row)}
                />
              ))}
            </tbody>
          </table>
          {page.loading ? <LoadingState /> : null}
          {page.items.length === 0 && !page.loading ? (
            <p className="p-6 text-center text-sm text-ink-muted">{t('ledger.empty')}</p>
          ) : null}
          {page.hasMore && !page.loading ? (
            <div className="p-3 text-center">
              <Button size="sm" variant="ghost" onClick={page.loadMore}>
                {t('ui.loadMore')}
              </Button>
            </div>
          ) : null}
        </div>
      )}

      <RefundDialog row={refundRow} onClose={() => setRefundRow(null)} onDone={page.reset} />
    </section>
  )
}

function LedgerRow({
  row,
  showBalance,
  balance,
  isReversal,
  reversedBy,
  canRefund,
  onRefund,
}: {
  row: LedgerItem
  showBalance: boolean
  balance: number | undefined
  isReversal: boolean
  reversedBy: string | undefined
  canRefund: boolean
  onRefund: () => void
}) {
  const t = useT()
  const { formatDate } = useFormatters()
  return (
    <tr className="border-t border-surface-sunken/60">
      <td className="px-3 py-2 whitespace-nowrap">{formatDate(row.at)}</td>
      <td className="px-3 py-2">
        <span>{t(`ledger.type.${row.type}`)}</span>
        {isReversal ? (
          <span className="ms-1.5 rounded bg-warn/10 px-1.5 py-0.5 text-xs text-warn">
            {t('ledger.reversal')}
          </span>
        ) : null}
        {reversedBy ? (
          <span className="ms-1.5 rounded bg-surface-sunken px-1.5 py-0.5 text-xs text-ink-muted">
            {t('ledger.reversed')}
          </span>
        ) : null}
      </td>
      <td className={`px-3 py-2 text-end ${row.amount < 0 ? 'text-danger' : ''}`}>
        <IQDAmount amount={row.amount} currency={row.currency} />
      </td>
      {showBalance ? (
        <td className="px-3 py-2 text-end text-ink-muted">
          {balance !== undefined ? <IQDAmount amount={balance} currency={row.currency} /> : '—'}
        </td>
      ) : null}
      <td className="px-3 py-2 text-ink-muted">{row.note || '—'}</td>
      <td className="px-3 py-2 text-end">
        {canRefund &&
        !reversedBy &&
        !isReversal &&
        (row.type === 'renewal' || row.type === 'voucher_redeem') ? (
          <Button size="sm" variant="ghost" onClick={onRefund}>
            {t('ledger.refund')}
          </Button>
        ) : null}
      </td>
    </tr>
  )
}

/** Refund flow (FR-25): reason required, consequences (expiry rollback) shown. */
function RefundDialog({
  row,
  onClose,
  onDone,
}: {
  row: LedgerItem | null
  onClose: () => void
  onDone: () => void
}) {
  const t = useT()
  const { toast } = useToast()
  const [reason, setReason] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit() {
    if (!row?.subscriber_id) return
    setBusy(true)
    try {
      await refundRenewal(row.subscriber_id, { ledger_tx_id: row.id, reason: reason.trim() })
      toast(t('ledger.refundDone'), 'ok')
      onClose()
      onDone()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open={row !== null}
      onOpenChange={busy ? () => {} : (o) => !o && onClose()}
      title={t('ledger.refundTitle')}
    >
      <div className="space-y-4">
        <div className="rounded-md bg-warn/10 p-3 text-sm text-warn">
          {t('ledger.refundConsequence')}
        </div>
        <Field label={t('ledger.refundReason')} htmlFor="refund-reason">
          <Textarea
            id="refund-reason"
            rows={3}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
          />
        </Field>
        <div className="flex justify-end gap-2">
          <Button variant="ghost" disabled={busy} onClick={onClose}>
            {t('ui.cancel')}
          </Button>
          <Button
            variant="danger"
            disabled={busy || reason.trim().length < 3}
            onClick={() => void submit()}
          >
            {busy ? t('ui.working') : t('ledger.refund')}
          </Button>
        </div>
      </div>
    </Modal>
  )
}

function Th({ children, className = '' }: { children?: React.ReactNode; className?: string }) {
  return <th className={`px-3 py-2 text-start font-medium ${className}`}>{children}</th>
}
