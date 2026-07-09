import { EmptyState, IQDAmount, useT } from '@hikrad/shared'

/**
 * Renew stub — the hero flow (renew → CoA, key flow 2) arrives in Phase 4;
 * this proves localized IQD amounts and the disabled action shell.
 */
export function RenewPage() {
  const t = useT()

  return (
    <section className="flex flex-col gap-4">
      <h1 className="text-lg font-semibold">{t('portal.renew.title')}</h1>

      <div className="flex flex-col gap-3 rounded-xl bg-surface-raised p-4 shadow-sm">
        <div className="flex items-center justify-between gap-2 text-sm">
          <span className="text-ink-muted">{t('portal.renew.price')}</span>
          <IQDAmount amount={25000} className="font-semibold" />
        </div>
        <button
          type="button"
          disabled
          className="rounded-md bg-brand py-2 font-semibold text-ink-inverse opacity-50"
        >
          {t('portal.renew.action')}
        </button>
      </div>

      <EmptyState body={t('portal.renew.placeholder')} />
    </section>
  )
}
