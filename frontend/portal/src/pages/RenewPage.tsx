import { useState } from 'react'
import { Link } from 'react-router-dom'

import { useT } from '@hikrad/shared'

import { getMyCardPayment } from '../api/cardPayments'
import { GatewayPanel } from '../components/renew/GatewayPanel'
import { ScratchCardPanel } from '../components/renew/ScratchCardPanel'
import { VoucherPanel } from '../components/renew/VoucherPanel'
import { useAsync } from '../hooks/useAsync'
import { getPendingIntent } from '../lib/pendingPayment'

type Tab = 'voucher' | 'gateway' | 'card'

/** Renew (FR-42, FR-59): voucher redeem, e-wallet gateways, scratch-card —
 * the hero flow (renew → CoA restore, key flow 2). */
export function RenewPage() {
  const t = useT()
  const [tab, setTab] = useState<Tab>('voucher')
  const cardPayment = useAsync(getMyCardPayment, [])
  const pending = getPendingIntent()

  return (
    <section className="flex flex-col gap-4">
      <h1 className="text-lg font-semibold">{t('portal.renew.title')}</h1>

      {pending ? (
        <Link
          to={`/renew/return/${pending.gateway}?intent=${pending.intentId}`}
          className="flex items-center justify-between gap-2 rounded-xl border border-brand bg-brand-soft p-3 text-sm font-semibold text-brand"
        >
          {t('portal.renew.resumePending')}
          <span aria-hidden="true">›</span>
        </Link>
      ) : null}

      <div
        role="tablist"
        aria-label={t('portal.renew.title')}
        className="flex gap-1 rounded-lg bg-surface-sunken p-1"
      >
        {(['voucher', 'gateway', 'card'] as const).map((tabId) => (
          <button
            key={tabId}
            type="button"
            role="tab"
            aria-selected={tab === tabId}
            onClick={() => setTab(tabId)}
            className={`flex-1 rounded-md py-1.5 text-sm transition-colors ${
              tab === tabId ? 'bg-surface-raised font-semibold shadow-sm' : 'text-ink-muted'
            }`}
          >
            {t(`portal.renew.tab.${tabId}`)}
          </button>
        ))}
      </div>

      {tab === 'voucher' ? <VoucherPanel /> : null}
      {tab === 'gateway' ? <GatewayPanel onGoToVoucher={() => setTab('voucher')} /> : null}
      {tab === 'card' ? <ScratchCardPanel myCardPayment={cardPayment.data} /> : null}
    </section>
  )
}
