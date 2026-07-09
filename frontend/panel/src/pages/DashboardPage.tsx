import { useT } from '@hikrad/shared'

/** Phase-1 empty dashboard shell — Omar's real dashboard arrives in Phase 3. */
export function DashboardPage() {
  const t = useT()
  return (
    <section>
      <h1 className="text-xl font-semibold">{t('dashboard.title')}</h1>
      <div className="mt-6 rounded-lg border border-dashed border-surface-sunken bg-surface-raised p-10 text-center">
        <p className="font-medium">{t('dashboard.empty.title')}</p>
        <p className="mt-1 text-sm text-ink-muted">{t('dashboard.empty.body')}</p>
      </div>
    </section>
  )
}
