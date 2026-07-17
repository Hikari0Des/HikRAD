import { Link } from 'react-router-dom'

import { IQDAmount, useT } from '@hikrad/shared'

import { getRevenueReport, getSubscriberReport } from '../../api/reports'
import { PageHeader } from '../../components/PageHeader'
import { useAsync } from '../../hooks/useAsync'
import { presetRange, toApiInstant } from './reportRange'

const month = presetRange('month')
const range = { from: toApiInstant(month.from), to: toApiInstant(month.to) }

/** Reports landing (task 1): headline numbers first, then links to each report. */
export function ReportsIndexPage() {
  const t = useT()
  const revenue = useAsync(() => getRevenueReport(range, 'day'), [])
  const expiring = useAsync(() => getSubscriberReport('expiring', range, 7), [])

  const cards: { to: string; titleKey: string; descKey: string }[] = [
    { to: '/reports/revenue', titleKey: 'reports.revenue.title', descKey: 'reports.revenue.desc' },
    {
      to: '/reports/settlement',
      titleKey: 'reports.settlement.title',
      descKey: 'reports.settlement.desc',
    },
    {
      to: '/reports/subscribers',
      titleKey: 'reports.subscribers.title',
      descKey: 'reports.subscribers.desc',
    },
    { to: '/reports/usage', titleKey: 'reports.usage.title', descKey: 'reports.usage.desc' },
    { to: '/reports/margin', titleKey: 'reports.margin.title', descKey: 'reports.margin.desc' },
  ]

  return (
    <section>
      <PageHeader title={t('reports.title')} subtitle={t('reports.subtitle')} />
      <div className="mb-6 grid gap-3 sm:grid-cols-2">
        <div className="rounded-md border border-surface-sunken bg-surface-raised p-4">
          <p className="text-xs text-ink-muted">{t('reports.index.revenueThisMonth')}</p>
          <p className="text-2xl font-semibold">
            {/* IQD-scoped headline (like the dashboard tile and digest) — the
                full per-currency breakdown lives on the revenue report itself. */}
            {revenue.loading ? '…' : <IQDAmount amount={revenue.data?.totals.IQD ?? 0} />}
          </p>
        </div>
        <div className="rounded-md border border-surface-sunken bg-surface-raised p-4">
          <p className="text-xs text-ink-muted">{t('reports.index.expiringSoon')}</p>
          <p className="text-2xl font-semibold">
            {expiring.loading ? '…' : (expiring.data?.total ?? 0)}
          </p>
        </div>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        {cards.map((c) => (
          <Link
            key={c.to}
            to={c.to}
            className="rounded-md border border-surface-sunken bg-surface-raised p-4 hover:border-brand"
          >
            <p className="font-medium">{t(c.titleKey)}</p>
            <p className="mt-1 text-sm text-ink-muted">{t(c.descKey)}</p>
          </Link>
        ))}
      </div>
    </section>
  )
}
