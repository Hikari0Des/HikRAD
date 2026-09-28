import { useState } from 'react'

import { ErrorState, IQDAmount, LoadingState, StatCard, useT } from '@hikrad/shared'

import {
  getAgingPayables,
  getAgingReceivables,
  getBalanceSheet,
  getCashFlow,
  getGeneralLedger,
  getProfitAndLoss,
  getTrialBalance,
  listJournal,
  type AgingBucket,
} from '../../api/accounting'
import { PageHeader } from '../../components/PageHeader'
import { TextInput } from '../../components/form'
import { useAsync } from '../../hooks/useAsync'
import { usePaginated } from '../../hooks/usePaginated'

type Tab = 'pl' | 'balanceSheet' | 'cashFlow' | 'aging' | 'advanced'

/** FR-156: P&L, Balance Sheet, Cash Flow, Aging, and Advanced (GL/TB/Journal). */
export function StatementsPage() {
  const t = useT()
  const [tab, setTab] = useState<Tab>('pl')
  const tabs: { key: Tab; label: string }[] = [
    { key: 'pl', label: t('statements.tab.pl') },
    { key: 'balanceSheet', label: t('statements.tab.balanceSheet') },
    { key: 'cashFlow', label: t('statements.tab.cashFlow') },
    { key: 'aging', label: t('statements.tab.aging') },
    { key: 'advanced', label: t('statements.tab.advanced') },
  ]

  return (
    <section>
      <PageHeader title={t('statements.title')} />
      <div className="mb-4 flex flex-wrap gap-1 border-b border-border">
        {tabs.map((tb) => (
          <button
            key={tb.key}
            type="button"
            onClick={() => setTab(tb.key)}
            className={`rounded-t-md px-3 py-2 text-sm font-medium ${
              tab === tb.key
                ? 'border-b-2 border-brand text-brand-strong'
                : 'text-ink-muted hover:text-ink'
            }`}
          >
            {tb.label}
          </button>
        ))}
      </div>
      {tab === 'pl' ? <ProfitAndLossTab /> : null}
      {tab === 'balanceSheet' ? <BalanceSheetTab /> : null}
      {tab === 'cashFlow' ? <CashFlowTab /> : null}
      {tab === 'aging' ? <AgingTab /> : null}
      {tab === 'advanced' ? <AdvancedTab /> : null}
    </section>
  )
}

function DateRangeFilter({
  from,
  to,
  onFrom,
  onTo,
}: {
  from: string
  to: string
  onFrom: (v: string) => void
  onTo: (v: string) => void
}) {
  const t = useT()
  return (
    <div className="mb-4 flex flex-wrap items-end gap-3">
      <label className="text-xs">
        <span className="mb-1 block text-ink-muted">{t('statements.from')}</span>
        <TextInput type="date" value={from} onChange={(e) => onFrom(e.target.value)} />
      </label>
      <label className="text-xs">
        <span className="mb-1 block text-ink-muted">{t('statements.to')}</span>
        <TextInput type="date" value={to} onChange={(e) => onTo(e.target.value)} />
      </label>
    </div>
  )
}

function ProfitAndLossTab() {
  const t = useT()
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const q = useAsync(() => getProfitAndLoss(from || undefined, to || undefined), [from, to])

  if (q.error) return <ErrorState onRetry={q.reload} />
  if (q.loading || !q.data) return <LoadingState />
  const pl = q.data
  return (
    <div>
      <DateRangeFilter from={from} to={to} onFrom={setFrom} onTo={setTo} />
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
        <StatCard label={t('statements.pl.revenue')} value={<IQDAmount amount={pl.revenue} />} />
        {pl.cost_of_service !== undefined ? (
          <StatCard
            label={t('statements.pl.costOfService')}
            value={<IQDAmount amount={pl.cost_of_service} />}
          />
        ) : null}
        {pl.gross_margin !== undefined ? (
          <StatCard
            label={t('statements.pl.grossMargin')}
            value={<IQDAmount amount={pl.gross_margin} />}
          />
        ) : null}
        <StatCard
          label={t('statements.pl.commissionExpense')}
          value={<IQDAmount amount={pl.commission_expense} />}
        />
        <StatCard
          label={t('statements.pl.operatingExpense')}
          value={<IQDAmount amount={pl.operating_expense} />}
        />
        <StatCard
          label={t('statements.pl.depreciationExpense')}
          value={<IQDAmount amount={pl.depreciation_expense} />}
        />
        <StatCard
          label={t('statements.pl.netIncome')}
          value={
            <span className={pl.net_income < 0 ? 'text-danger' : ''}>
              <IQDAmount amount={pl.net_income} />
            </span>
          }
        />
      </div>
    </div>
  )
}

function BalanceSheetTab() {
  const t = useT()
  const [asOf, setAsOf] = useState('')
  const q = useAsync(() => getBalanceSheet(asOf || undefined), [asOf])
  if (q.error) return <ErrorState onRetry={q.reload} />
  if (q.loading || !q.data) return <LoadingState />
  const bs = q.data
  return (
    <div>
      <label className="mb-4 block text-xs">
        <span className="mb-1 block text-ink-muted">{t('statements.asOf')}</span>
        <TextInput type="date" value={asOf} onChange={(e) => setAsOf(e.target.value)} />
      </label>
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
        <StatCard label={t('statements.bs.assets')} value={<IQDAmount amount={bs.assets} />} />
        <StatCard
          label={t('statements.bs.liabilities')}
          value={<IQDAmount amount={bs.liabilities} />}
        />
        <StatCard label={t('statements.bs.capital')} value={<IQDAmount amount={bs.capital} />} />
        <StatCard
          label={t('statements.bs.retainedEarnings')}
          value={<IQDAmount amount={bs.retained_earnings} />}
        />
        <StatCard label={t('statements.bs.equity')} value={<IQDAmount amount={bs.equity} />} />
      </div>
    </div>
  )
}

function CashFlowTab() {
  const t = useT()
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const q = useAsync(() => getCashFlow(from || undefined, to || undefined), [from, to])
  if (q.error) return <ErrorState onRetry={q.reload} />
  if (q.loading || !q.data) return <LoadingState />
  return (
    <div>
      <DateRangeFilter from={from} to={to} onFrom={setFrom} onTo={setTo} />
      <StatCard
        label={t('statements.cf.netChange')}
        value={<IQDAmount amount={q.data.net_change} />}
        className="mb-4"
      />
      <div className="overflow-x-auto rounded-md border border-border">
        <table className="w-full text-sm">
          <thead className="bg-surface-sunken/40 text-start text-xs text-ink-muted">
            <tr>
              <th className="px-3 py-2 text-start">{t('statements.cf.account')}</th>
              <th className="px-3 py-2 text-end">{t('statements.cf.netChange')}</th>
            </tr>
          </thead>
          <tbody>
            {q.data.lines.map((l) => (
              <tr key={l.account_code} className="border-t border-border/60">
                <td className="px-3 py-2">{l.account_name}</td>
                <td className="px-3 py-2 text-end">
                  <IQDAmount amount={l.net_change} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function AgingTable({ buckets }: { buckets: AgingBucket[] }) {
  const t = useT()
  return (
    <div className="overflow-x-auto rounded-md border border-border">
      <table className="w-full text-sm">
        <thead className="bg-surface-sunken/40 text-start text-xs text-ink-muted">
          <tr>
            <th className="px-3 py-2 text-start">{t('statements.aging.bucket')}</th>
            <th className="px-3 py-2 text-end">{t('statements.aging.amount')}</th>
          </tr>
        </thead>
        <tbody>
          {buckets.map((b) => (
            <tr key={b.Bucket} className="border-t border-border/60">
              <td className="px-3 py-2">{b.Bucket}</td>
              <td className="px-3 py-2 text-end">
                <IQDAmount amount={b.Amount} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function AgingTab() {
  const t = useT()
  const ar = useAsync(() => getAgingReceivables(), [])
  const ap = useAsync(() => getAgingPayables(), [])
  return (
    <div className="grid gap-6 md:grid-cols-2">
      <div>
        <h2 className="mb-2 text-sm font-semibold">{t('statements.aging.receivables')}</h2>
        {ar.error ? (
          <ErrorState onRetry={ar.reload} />
        ) : ar.loading || !ar.data ? (
          <LoadingState />
        ) : (
          <AgingTable buckets={ar.data.items} />
        )}
      </div>
      <div>
        <h2 className="mb-2 text-sm font-semibold">{t('statements.aging.payables')}</h2>
        {ap.error ? (
          <ErrorState onRetry={ap.reload} />
        ) : ap.loading || !ap.data ? (
          <LoadingState />
        ) : (
          <AgingTable buckets={ap.data.items} />
        )}
      </div>
    </div>
  )
}

type AdvancedView = 'journal' | 'trialBalance' | 'generalLedger'

function AdvancedTab() {
  const t = useT()
  const [view, setView] = useState<AdvancedView>('journal')
  return (
    <div>
      <div className="mb-4 flex gap-2">
        {(['journal', 'trialBalance', 'generalLedger'] as AdvancedView[]).map((v) => (
          <button
            key={v}
            type="button"
            onClick={() => setView(v)}
            className={`rounded-md px-2.5 py-1 text-xs font-medium ${
              view === v ? 'bg-brand-soft text-brand-strong' : 'bg-surface-sunken text-ink-muted'
            }`}
          >
            {t(`statements.advanced.${v}`)}
          </button>
        ))}
      </div>
      {view === 'journal' ? <JournalView /> : null}
      {view === 'trialBalance' ? <TrialBalanceView /> : null}
      {view === 'generalLedger' ? <GeneralLedgerView /> : null}
    </div>
  )
}

function JournalView() {
  const t = useT()
  const page = usePaginated((cursor) => listJournal({ cursor }), 'journal')
  if (page.error) return <ErrorState onRetry={page.reset} />
  return (
    <div className="overflow-x-auto rounded-md border border-border">
      <table className="w-full min-w-[36rem] text-sm">
        <thead className="bg-surface-sunken/40 text-start text-xs text-ink-muted">
          <tr>
            <th className="px-3 py-2 text-start">{t('statements.advanced.at')}</th>
            <th className="px-3 py-2 text-start">{t('statements.advanced.memo')}</th>
            <th className="px-3 py-2 text-start">{t('statements.advanced.source')}</th>
            <th className="px-3 py-2 text-start">{t('statements.advanced.reference')}</th>
          </tr>
        </thead>
        <tbody>
          {page.items.map((e) => (
            <tr key={e.id} className="border-t border-border/60">
              <td className="px-3 py-2 whitespace-nowrap">{e.at}</td>
              <td className="px-3 py-2">{e.memo}</td>
              <td className="px-3 py-2">{e.source}</td>
              <td className="px-3 py-2">{e.reference || '—'}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {page.loading ? <LoadingState /> : null}
    </div>
  )
}

function TrialBalanceView() {
  const t = useT()
  const q = useAsync(() => getTrialBalance(), [])
  if (q.error) return <ErrorState onRetry={q.reload} />
  if (q.loading || !q.data) return <LoadingState />
  return (
    <div className="overflow-x-auto rounded-md border border-border">
      <table className="w-full text-sm">
        <thead className="bg-surface-sunken/40 text-start text-xs text-ink-muted">
          <tr>
            <th className="px-3 py-2 text-start">{t('statements.advanced.account')}</th>
            <th className="px-3 py-2 text-end">{t('statements.advanced.debit')}</th>
            <th className="px-3 py-2 text-end">{t('statements.advanced.credit')}</th>
          </tr>
        </thead>
        <tbody>
          {q.data.lines.map((l) => (
            <tr key={l.code} className="border-t border-border/60">
              <td className="px-3 py-2">
                {l.code} · {l.name}
              </td>
              <td className="px-3 py-2 text-end">
                <IQDAmount amount={l.debit} />
              </td>
              <td className="px-3 py-2 text-end">
                <IQDAmount amount={l.credit} />
              </td>
            </tr>
          ))}
          <tr className="border-t border-border font-semibold">
            <td className="px-3 py-2">{t('statements.advanced.total')}</td>
            <td className="px-3 py-2 text-end">
              <IQDAmount amount={q.data.total_debit} />
            </td>
            <td className="px-3 py-2 text-end">
              <IQDAmount amount={q.data.total_credit} />
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  )
}

function GeneralLedgerView() {
  const t = useT()
  const [account, setAccount] = useState('1000')
  const q = useAsync(() => getGeneralLedger(account), [account])
  return (
    <div>
      <label className="mb-4 block text-xs">
        <span className="mb-1 block text-ink-muted">{t('statements.advanced.accountCode')}</span>
        <TextInput value={account} onChange={(e) => setAccount(e.target.value)} dir="ltr" />
      </label>
      {q.error ? (
        <ErrorState onRetry={q.reload} />
      ) : q.loading || !q.data ? (
        <LoadingState />
      ) : (
        <div className="overflow-x-auto rounded-md border border-border">
          <table className="w-full min-w-[40rem] text-sm">
            <thead className="bg-surface-sunken/40 text-start text-xs text-ink-muted">
              <tr>
                <th className="px-3 py-2 text-start">{t('statements.advanced.at')}</th>
                <th className="px-3 py-2 text-start">{t('statements.advanced.memo')}</th>
                <th className="px-3 py-2 text-end">{t('statements.advanced.debit')}</th>
                <th className="px-3 py-2 text-end">{t('statements.advanced.credit')}</th>
                <th className="px-3 py-2 text-end">{t('statements.advanced.running')}</th>
              </tr>
            </thead>
            <tbody>
              {q.data.items.map((l, i) => (
                <tr key={i} className="border-t border-border/60">
                  <td className="px-3 py-2 whitespace-nowrap">{l.at}</td>
                  <td className="px-3 py-2">{l.line_memo || l.entry_memo}</td>
                  <td className="px-3 py-2 text-end">
                    <IQDAmount amount={l.debit} />
                  </td>
                  <td className="px-3 py-2 text-end">
                    <IQDAmount amount={l.credit} />
                  </td>
                  <td className="px-3 py-2 text-end">
                    <IQDAmount amount={l.running_balance} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
