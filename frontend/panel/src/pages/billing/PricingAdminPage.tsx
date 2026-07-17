import { useState } from 'react'

import { ErrorState, IQDAmount, LoadingState, useFormatters, useT } from '@hikrad/shared'

import {
  createOverhead,
  createResellerPrice,
  listCurrencies,
  listOverheads,
  listResellerPrices,
  type Overhead,
  type ResellerPrice,
} from '../../api/billing'
import { listManagers } from '../../api/managers'
import { listNas } from '../../api/nas'
import { listProfiles } from '../../api/profiles'
import { Button } from '../../components/Button'
import { Field, Select, TextInput } from '../../components/form'
import { PageHeader } from '../../components/PageHeader'
import { useToast } from '../../components/Toast'
import { useAsync } from '../../hooks/useAsync'

/**
 * Overheads (FR-73) + reseller wholesale pricing (FR-74) — both admin-only,
 * append-only-versioned. Combined on one screen (implementer's call, same
 * reasoning as v2-4's CurrencyRatesPage): both are "an admin enters a money
 * fact that a renewal later resolves against," and an operator setting one
 * up is very likely to be here for the other in the same sitting.
 */
export function PricingAdminPage() {
  const t = useT()
  return (
    <section>
      <PageHeader title={t('pricingAdmin.title')} subtitle={t('pricingAdmin.subtitle')} />
      <OverheadsSection />
      <div className="mt-8">
        <ResellerPricesSection />
      </div>
    </section>
  )
}

function OverheadsSection() {
  const t = useT()
  const { toast } = useToast()
  const { formatDate } = useFormatters()
  const { data: currencies } = useAsync(() => listCurrencies(), [])
  const { data: nasList } = useAsync(() => listNas(), [])
  const { data, error, loading, reload } = useAsync(() => listOverheads(), [])

  const [name, setName] = useState('')
  const [amount, setAmount] = useState('')
  const [currency, setCurrency] = useState('IQD')
  const [nasId, setNasId] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit() {
    setBusy(true)
    try {
      await createOverhead({
        name,
        amount: Number(amount),
        currency,
        nas_id: nasId || null,
        period_start: new Date().toISOString(),
      })
      toast(t('pricingAdmin.overheadSaved'), 'ok')
      setName('')
      setAmount('')
      reload()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="rounded-md border border-surface-sunken">
      <div className="border-b border-surface-sunken p-3">
        <h2 className="text-sm font-semibold">{t('pricingAdmin.overheads.title')}</h2>
      </div>
      <form
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
        className="grid gap-3 border-b border-surface-sunken p-3 sm:grid-cols-5 sm:items-end"
      >
        <Field label={t('pricingAdmin.overheads.name')} htmlFor="oh-name">
          <TextInput id="oh-name" value={name} onChange={(e) => setName(e.target.value)} required />
        </Field>
        <Field label={t('pricingAdmin.overheads.amount')} htmlFor="oh-amount">
          <TextInput
            id="oh-amount"
            type="number"
            dir="ltr"
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            required
          />
        </Field>
        <Field label={t('profiles.currency')} htmlFor="oh-currency">
          <Select id="oh-currency" value={currency} onChange={(e) => setCurrency(e.target.value)}>
            {(currencies?.items ?? [{ code: 'IQD' }]).map((c) => (
              <option key={c.code} value={c.code}>
                {c.code}
              </option>
            ))}
          </Select>
        </Field>
        <Field label={t('pricingAdmin.overheads.site')} htmlFor="oh-nas">
          <Select id="oh-nas" value={nasId} onChange={(e) => setNasId(e.target.value)}>
            <option value="">{t('pricingAdmin.overheads.global')}</option>
            {(nasList?.items ?? []).map((n) => (
              <option key={n.id} value={n.id}>
                {n.name}
              </option>
            ))}
          </Select>
        </Field>
        <Button type="submit" disabled={busy || !name || !amount}>
          {busy ? t('ui.working') : t('pricingAdmin.overheads.add')}
        </Button>
      </form>

      {error ? (
        <ErrorState onRetry={reload} />
      ) : loading ? (
        <LoadingState />
      ) : (data?.items.length ?? 0) === 0 ? (
        <p className="p-6 text-center text-sm text-ink-muted">
          {t('pricingAdmin.overheads.empty')}
        </p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="bg-surface-sunken/40 text-start text-xs text-ink-muted">
              <tr>
                <Th>{t('pricingAdmin.overheads.name')}</Th>
                <Th className="text-end">{t('pricingAdmin.overheads.amount')}</Th>
                <Th>{t('pricingAdmin.overheads.site')}</Th>
                <Th>{t('currencyRates.table.effectiveFrom')}</Th>
              </tr>
            </thead>
            <tbody>
              {(data?.items ?? []).map((o: Overhead) => (
                <tr key={o.id} className="border-t border-surface-sunken/60">
                  <td className="px-3 py-2">{o.name}</td>
                  <td className="px-3 py-2 text-end">
                    <IQDAmount amount={o.amount} currency={o.currency} />
                  </td>
                  <td className="px-3 py-2 text-ink-muted">
                    {o.nas_id
                      ? (nasList?.items.find((n) => n.id === o.nas_id)?.name ?? o.nas_id)
                      : t('pricingAdmin.overheads.global')}
                  </td>
                  <td className="px-3 py-2 text-ink-muted">{formatDate(o.period_start)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

function ResellerPricesSection() {
  const t = useT()
  const { toast } = useToast()
  const { formatDate } = useFormatters()
  const { data: currencies } = useAsync(() => listCurrencies(), [])
  const { data: managers } = useAsync(() => listManagers(), [])
  const { data: profiles } = useAsync(() => listProfiles(), [])
  const { data, error, loading, reload } = useAsync(() => listResellerPrices({}), [])

  const resellers = (managers?.items ?? []).filter((m) => m.scoped)

  const [managerId, setManagerId] = useState('')
  const [profileId, setProfileId] = useState('')
  const [subscriberId, setSubscriberId] = useState('')
  const [price, setPrice] = useState('')
  const [currency, setCurrency] = useState('IQD')
  const [busy, setBusy] = useState(false)

  async function submit() {
    setBusy(true)
    try {
      await createResellerPrice({
        manager_id: managerId,
        profile_id: profileId,
        subscriber_id: subscriberId || null,
        price: Number(price),
        currency,
      })
      toast(t('pricingAdmin.resellerPrices.saved'), 'ok')
      setSubscriberId('')
      setPrice('')
      reload()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="rounded-md border border-surface-sunken">
      <div className="border-b border-surface-sunken p-3">
        <h2 className="text-sm font-semibold">{t('pricingAdmin.resellerPrices.title')}</h2>
        <p className="mt-1 text-xs text-ink-muted">{t('pricingAdmin.resellerPrices.hint')}</p>
      </div>
      <form
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
        className="grid gap-3 border-b border-surface-sunken p-3 sm:grid-cols-6 sm:items-end"
      >
        <Field label={t('pricingAdmin.resellerPrices.reseller')} htmlFor="rp-manager">
          <Select
            id="rp-manager"
            value={managerId}
            onChange={(e) => setManagerId(e.target.value)}
            required
          >
            <option value="">{t('ui.choose')}</option>
            {resellers.map((m) => (
              <option key={m.id} value={m.id}>
                {m.username}
              </option>
            ))}
          </Select>
        </Field>
        <Field label={t('vouchers.profile')} htmlFor="rp-profile">
          <Select
            id="rp-profile"
            value={profileId}
            onChange={(e) => setProfileId(e.target.value)}
            required
          >
            <option value="">{t('vouchers.pickProfile')}</option>
            {(profiles?.items ?? []).map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field
          label={t('pricingAdmin.resellerPrices.subscriberOverride')}
          htmlFor="rp-subscriber"
          hint={t('pricingAdmin.resellerPrices.subscriberOverrideHint')}
        >
          <TextInput
            id="rp-subscriber"
            dir="ltr"
            value={subscriberId}
            onChange={(e) => setSubscriberId(e.target.value)}
          />
        </Field>
        <Field label={t('pricingAdmin.resellerPrices.price')} htmlFor="rp-price">
          <TextInput
            id="rp-price"
            type="number"
            dir="ltr"
            value={price}
            onChange={(e) => setPrice(e.target.value)}
            required
          />
        </Field>
        <Field label={t('profiles.currency')} htmlFor="rp-currency">
          <Select id="rp-currency" value={currency} onChange={(e) => setCurrency(e.target.value)}>
            {(currencies?.items ?? [{ code: 'IQD' }]).map((c) => (
              <option key={c.code} value={c.code}>
                {c.code}
              </option>
            ))}
          </Select>
        </Field>
        <Button type="submit" disabled={busy || !managerId || !profileId || !price}>
          {busy ? t('ui.working') : t('pricingAdmin.resellerPrices.add')}
        </Button>
      </form>

      {error ? (
        <ErrorState onRetry={reload} />
      ) : loading ? (
        <LoadingState />
      ) : (data?.items.length ?? 0) === 0 ? (
        <p className="p-6 text-center text-sm text-ink-muted">
          {t('pricingAdmin.resellerPrices.empty')}
        </p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="bg-surface-sunken/40 text-start text-xs text-ink-muted">
              <tr>
                <Th>{t('pricingAdmin.resellerPrices.reseller')}</Th>
                <Th>{t('vouchers.profile')}</Th>
                <Th>{t('pricingAdmin.resellerPrices.subscriberOverride')}</Th>
                <Th className="text-end">{t('pricingAdmin.resellerPrices.price')}</Th>
                <Th>{t('currencyRates.table.effectiveFrom')}</Th>
              </tr>
            </thead>
            <tbody>
              {(data?.items ?? []).map((rp: ResellerPrice) => (
                <tr key={rp.id} className="border-t border-surface-sunken/60">
                  <td className="px-3 py-2">
                    {resellers.find((m) => m.id === rp.manager_id)?.username ?? rp.manager_id}
                  </td>
                  <td className="px-3 py-2">
                    {profiles?.items.find((p) => p.id === rp.profile_id)?.name ?? rp.profile_id}
                  </td>
                  <td className="px-3 py-2 text-ink-muted" dir="ltr">
                    {rp.subscriber_id ?? t('pricingAdmin.resellerPrices.planWide')}
                  </td>
                  <td className="px-3 py-2 text-end">
                    <IQDAmount amount={rp.price} currency={rp.currency} />
                  </td>
                  <td className="px-3 py-2 text-ink-muted">{formatDate(rp.effective_from)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

function Th({ children, className = '' }: { children?: React.ReactNode; className?: string }) {
  return <th className={`px-3 py-2 text-start font-medium ${className}`}>{children}</th>
}
