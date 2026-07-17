import { useState } from 'react'

import { ErrorState, IQDAmount, LoadingState, useFormatters, useT } from '@hikrad/shared'

import {
  createCurrencyRate,
  exchangeManagerBalance,
  listCurrencies,
  listCurrencyRates,
  listManagerBalances,
  type CurrencyRate,
} from '../../api/billing'
import { ApiError } from '../../api/client'
import { useAuth } from '../../auth/AuthContext'
import { PERM_CURRENCY_RATES_MANAGE } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { Field, Select, TextInput } from '../../components/form'
import { PageHeader } from '../../components/PageHeader'
import { useToast } from '../../components/Toast'
import { useAsync } from '../../hooks/useAsync'

/**
 * Currency rates + exchange (v2 phase 4, contracts C4/C5). Rates are entered
 * by hand — FR-68.4/AC-68b, there is no online rate feed anywhere in this
 * codebase — and every submission is a brand-new append-only row, never an
 * update, so a `currency_rate_id` stamped on a past ledger entry always means
 * the exact rate actually used at that moment (FR-68.1).
 */
export function CurrencyRatesPage() {
  const t = useT()
  const { can, manager } = useAuth()
  const {
    data: currencies,
    loading: currenciesLoading,
    error: currenciesError,
  } = useAsync(() => listCurrencies(), [])
  const {
    data: rates,
    loading: ratesLoading,
    error: ratesError,
    reload: reloadRates,
  } = useAsync(() => listCurrencyRates(), [])

  return (
    <section>
      <PageHeader title={t('currencyRates.title')} subtitle={t('currencyRates.subtitle')} />

      {manager ? (
        <ExchangeCard
          managerId={manager.id}
          currencies={currencies?.items ?? []}
          rates={rates?.items ?? []}
        />
      ) : null}

      <div className="mt-6 rounded-md border border-surface-sunken">
        <div className="flex items-center justify-between border-b border-surface-sunken p-3">
          <h2 className="text-sm font-semibold">{t('currencyRates.table.title')}</h2>
        </div>

        {can(PERM_CURRENCY_RATES_MANAGE) ? (
          <CreateRateForm currencies={currencies?.items ?? []} onCreated={reloadRates} />
        ) : null}

        {currenciesError || ratesError ? (
          <ErrorState onRetry={reloadRates} />
        ) : currenciesLoading || ratesLoading ? (
          <LoadingState />
        ) : (rates?.items.length ?? 0) === 0 ? (
          <p className="p-6 text-center text-sm text-ink-muted">{t('currencyRates.table.empty')}</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="bg-surface-sunken/40 text-start text-xs text-ink-muted">
                <tr>
                  <Th>{t('currencyRates.table.pair')}</Th>
                  <Th className="text-end">{t('currencyRates.table.rate')}</Th>
                  <Th>{t('currencyRates.table.effectiveFrom')}</Th>
                </tr>
              </thead>
              <tbody>
                {(rates?.items ?? []).map((r) => (
                  <RateRow key={r.id} rate={r} />
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </section>
  )
}

function RateRow({ rate }: { rate: CurrencyRate }) {
  const { formatDate, formatNumber } = useFormatters()
  return (
    <tr className="border-t border-surface-sunken/60">
      <td className="px-3 py-2" dir="ltr">
        {rate.from_currency} → {rate.to_currency}
      </td>
      <td className="px-3 py-2 text-end" dir="ltr">
        {formatNumber(rate.rate, { maximumFractionDigits: 8 })}
      </td>
      <td className="px-3 py-2 text-ink-muted">{formatDate(rate.effective_from)}</td>
    </tr>
  )
}

function CreateRateForm({
  currencies,
  onCreated,
}: {
  currencies: { code: string }[]
  onCreated: () => void
}) {
  const t = useT()
  const { toast } = useToast()
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const [rate, setRate] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit() {
    setBusy(true)
    try {
      await createCurrencyRate({ from_currency: from, to_currency: to, rate: Number(rate) })
      toast(t('currencyRates.rateCreated'), 'ok')
      setRate('')
      onCreated()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    } finally {
      setBusy(false)
    }
  }

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        void submit()
      }}
      className="grid gap-3 border-b border-surface-sunken p-3 sm:grid-cols-4 sm:items-end"
    >
      <Field label={t('currencyRates.from')} htmlFor="rate-from">
        <Select id="rate-from" value={from} onChange={(e) => setFrom(e.target.value)} required>
          <option value="">{t('ui.choose')}</option>
          {currencies.map((c) => (
            <option key={c.code} value={c.code}>
              {c.code}
            </option>
          ))}
        </Select>
      </Field>
      <Field label={t('currencyRates.to')} htmlFor="rate-to">
        <Select id="rate-to" value={to} onChange={(e) => setTo(e.target.value)} required>
          <option value="">{t('ui.choose')}</option>
          {currencies.map((c) => (
            <option key={c.code} value={c.code}>
              {c.code}
            </option>
          ))}
        </Select>
      </Field>
      <Field
        label={t('currencyRates.rate')}
        htmlFor="rate-value"
        hint={t('currencyRates.rateHint')}
      >
        <TextInput
          id="rate-value"
          type="number"
          step="any"
          min={0}
          dir="ltr"
          value={rate}
          onChange={(e) => setRate(e.target.value)}
          required
        />
      </Field>
      <Button type="submit" disabled={busy || !from || !to || !rate}>
        {busy ? t('ui.working') : t('currencyRates.addRate')}
      </Button>
    </form>
  )
}

/** The signed-in manager's own exchange (C4) — converts part of their balance. */
function ExchangeCard({
  managerId,
  currencies,
  rates,
}: {
  managerId: string
  currencies: { code: string }[]
  rates: CurrencyRate[]
}) {
  const t = useT()
  const { toast } = useToast()
  const {
    data: balances,
    loading: balancesLoading,
    reload: reloadBalances,
  } = useAsync(() => listManagerBalances(managerId), [managerId])

  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const [amount, setAmount] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const applicableRate = rates.find((r) => r.from_currency === from && r.to_currency === to)

  async function submit() {
    if (!applicableRate) return
    setBusy(true)
    setError(null)
    try {
      await exchangeManagerBalance(managerId, {
        from_currency: from,
        to_currency: to,
        amount: Number(amount),
        currency_rate_id: applicableRate.id,
      })
      toast(t('currencyRates.exchangeDone'), 'ok')
      setAmount('')
      reloadBalances()
    } catch (err) {
      if (err instanceof ApiError) setError(err.message)
      else setError(t('common.error.body'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="rounded-md border border-surface-sunken p-4">
      <h2 className="mb-3 text-sm font-semibold">{t('currencyRates.exchange.title')}</h2>

      {balancesLoading ? (
        <LoadingState />
      ) : (
        <div className="mb-4 flex flex-wrap gap-3">
          {(balances?.balances ?? []).map((b) => (
            <div key={b.currency} className="rounded-md bg-surface-sunken px-3 py-1.5 text-sm">
              <span className="text-ink-muted">{b.currency}</span>{' '}
              <IQDAmount amount={b.balance} currency={b.currency} />
            </div>
          ))}
        </div>
      )}

      <form
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
        className="grid gap-3 sm:grid-cols-4 sm:items-end"
      >
        <Field label={t('currencyRates.from')} htmlFor="exch-from">
          <Select id="exch-from" value={from} onChange={(e) => setFrom(e.target.value)} required>
            <option value="">{t('ui.choose')}</option>
            {currencies.map((c) => (
              <option key={c.code} value={c.code}>
                {c.code}
              </option>
            ))}
          </Select>
        </Field>
        <Field label={t('currencyRates.to')} htmlFor="exch-to">
          <Select id="exch-to" value={to} onChange={(e) => setTo(e.target.value)} required>
            <option value="">{t('ui.choose')}</option>
            {currencies.map((c) => (
              <option key={c.code} value={c.code}>
                {c.code}
              </option>
            ))}
          </Select>
        </Field>
        <Field label={t('currencyRates.exchange.amount')} htmlFor="exch-amount">
          <TextInput
            id="exch-amount"
            type="number"
            dir="ltr"
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            required
          />
        </Field>
        <Button type="submit" disabled={busy || !from || !to || !amount || !applicableRate}>
          {busy ? t('ui.working') : t('currencyRates.exchange.submit')}
        </Button>
      </form>

      {from && to && !applicableRate ? (
        <p className="mt-2 text-xs text-warn">{t('currencyRates.exchange.noRate')}</p>
      ) : null}
      {error ? <p className="mt-2 text-sm text-danger">{error}</p> : null}
    </div>
  )
}

function Th({ children, className = '' }: { children?: React.ReactNode; className?: string }) {
  return <th className={`px-3 py-2 text-start font-medium ${className}`}>{children}</th>
}
