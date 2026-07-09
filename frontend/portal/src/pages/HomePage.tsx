import { EmptyState, QuotaBar, StatusBadge, useFormatters, useT } from '@hikrad/shared'

/** Home stub (Phase 4 fills with live data — FR-41). Sample values only. */
export function HomePage() {
  const t = useT()
  const { formatDate, formatNumber } = useFormatters()

  return (
    <section className="flex flex-col gap-4">
      <h1 className="text-lg font-semibold">{t('portal.home.title')}</h1>

      <div className="flex flex-col gap-3 rounded-xl bg-surface-raised p-4 shadow-sm">
        <div className="flex items-center justify-between gap-2 text-sm">
          <span className="text-ink-muted">{t('portal.home.status')}</span>
          <StatusBadge status="active" />
        </div>
        <div className="flex items-center justify-between gap-2 text-sm">
          <span className="text-ink-muted">{t('portal.home.expires')}</span>
          <span>{formatDate('2026-08-01T00:00:00Z')}</span>
        </div>
      </div>

      <div className="flex flex-col gap-2 rounded-xl bg-surface-raised p-4 shadow-sm">
        <h2 className="text-sm font-semibold">{t('portal.home.quotaTitle')}</h2>
        <QuotaBar
          used={42}
          total={100}
          usedLabel={formatNumber(42)}
          totalLabel={formatNumber(100)}
        />
      </div>

      <EmptyState body={t('portal.home.placeholder')} />
    </section>
  )
}
