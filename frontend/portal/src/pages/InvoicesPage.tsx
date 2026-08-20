import { ErrorState, IQDAmount, LoadingState, useFormatters, useT } from '@hikrad/shared'

import { getInvoices, openInvoicePrint } from '../api/invoices'
import { Badge } from '../components/Badge'
import { useAsync } from '../hooks/useAsync'

/** Portal invoices (FR-171). Paying an unpaid one reuses the existing
 * unified Pay screen (/renew) — this surface is read-only. */
export function InvoicesPage() {
  const t = useT()
  const { formatDate } = useFormatters()
  const invoices = useAsync(getInvoices, [])

  if (invoices.loading) return <LoadingState />
  if (invoices.error || !invoices.data) {
    return <ErrorState body={t('portal.invoices.loadError')} onRetry={invoices.reload} />
  }

  return (
    <section className="flex flex-col gap-4">
      <h1 className="text-lg font-semibold">{t('portal.invoices.title')}</h1>
      {invoices.data.items.length === 0 ? (
        <p className="py-6 text-center text-sm text-ink-muted">{t('portal.invoices.empty')}</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {invoices.data.items.map((inv) => (
            <li
              key={inv.id}
              className="flex flex-col gap-2 rounded-xl bg-surface-raised p-3 shadow-sm"
            >
              <div className="flex items-center justify-between gap-2">
                <span className="font-medium">{inv.invoice_no}</span>
                <Badge tone={inv.status}>{t(`portal.invoices.status.${inv.status}`)}</Badge>
              </div>
              <div className="flex items-center justify-between gap-2 text-sm">
                <span className="text-ink-muted">{formatDate(inv.issue_date)}</span>
                <IQDAmount amount={inv.total} currency="IQD" className="font-semibold" />
              </div>
              <button
                type="button"
                onClick={() => void openInvoicePrint(inv.id)}
                className="self-start text-xs font-semibold text-brand underline"
              >
                {t('portal.invoices.printAction')}
              </button>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
