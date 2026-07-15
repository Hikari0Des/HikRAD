import { useState } from 'react'

import { ErrorState, IQDAmount, LoadingState, useFormatters, useT } from '@hikrad/shared'

import { getPayments, getUsage } from '../api/usage'
import { UsageChart } from '../components/UsageChart'
import { useAsync } from '../hooks/useAsync'

const PAYMENT_TYPE_KEYS: Record<string, string> = {
  renewal: 'portal.usage.paymentType.renewal',
  voucher_redeem: 'portal.usage.paymentType.voucher',
  'portal-mock': 'portal.usage.paymentType.gateway',
  'portal-zaincash': 'portal.usage.paymentType.gateway',
  'card-trial': 'portal.usage.paymentType.card',
  refund: 'portal.usage.paymentType.refund',
}

/** Usage & payments (FR-41.3): daily/monthly charts, payment history. All
 * server-scoped to the signed-in subscriber only (IDOR rule, contract C2). */
export function UsagePage() {
  const t = useT()
  const { formatDate } = useFormatters()
  const [granularity, setGranularity] = useState<'daily' | 'monthly'>('daily')
  const usage = useAsync(() => getUsage(granularity), [granularity])
  const payments = useAsync(() => getPayments({ limit: 20 }), [])

  return (
    <section className="flex flex-col gap-4">
      <h1 className="text-lg font-semibold">{t('portal.usage.title')}</h1>

      <div className="flex flex-col gap-2 rounded-xl bg-surface-raised p-4 shadow-sm">
        <div className="flex items-center justify-between">
          <h2 className="text-sm font-semibold">{t('portal.usage.chartTitle')}</h2>
          <div role="group" className="inline-flex gap-1">
            {(['daily', 'monthly'] as const).map((g) => (
              <button
                key={g}
                type="button"
                aria-pressed={granularity === g}
                onClick={() => setGranularity(g)}
                className={`rounded-md px-2 py-1 text-xs ${
                  granularity === g
                    ? 'bg-brand text-ink-inverse'
                    : 'bg-surface-sunken text-ink-muted'
                }`}
              >
                {t(`portal.usage.granularity.${g}`)}
              </button>
            ))}
          </div>
        </div>
        {usage.loading ? (
          <LoadingState />
        ) : usage.error || !usage.data ? (
          <ErrorState body={t('portal.usage.loadError')} onRetry={usage.reload} />
        ) : (
          <UsageChart points={usage.data.items} granularity={granularity} />
        )}
      </div>

      <div className="flex flex-col gap-2 rounded-xl bg-surface-raised p-4 shadow-sm">
        <h2 className="text-sm font-semibold">{t('portal.usage.paymentsTitle')}</h2>
        {payments.loading ? (
          <LoadingState />
        ) : payments.error || !payments.data ? (
          <ErrorState body={t('portal.usage.loadError')} onRetry={payments.reload} />
        ) : payments.data.items.length === 0 ? (
          <p className="py-4 text-center text-sm text-ink-muted">
            {t('portal.usage.paymentsEmpty')}
          </p>
        ) : (
          <ul className="flex flex-col divide-y divide-surface-sunken">
            {payments.data.items.map((p) => (
              <li key={p.id} className="flex items-center justify-between gap-2 py-2 text-sm">
                <div>
                  <p className="font-medium">
                    {t(PAYMENT_TYPE_KEYS[p.type] ?? 'portal.usage.paymentType.other')}
                  </p>
                  <p className="text-xs text-ink-muted">{formatDate(p.at)}</p>
                </div>
                <IQDAmount amount={p.amount_iqd} className="font-semibold" />
              </li>
            ))}
          </ul>
        )}
      </div>
    </section>
  )
}
