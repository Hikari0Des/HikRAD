import { useT } from '@hikrad/shared'

import { PayPanel } from '../components/renew/PayPanel'

/** Renew (v2-2, FR-42, FR-78): one unified Pay screen — every method the
 * subscriber's owning manager has enabled, voucher included, as tiles — the
 * hero flow (renew → CoA restore, key flow 2). */
export function RenewPage() {
  const t = useT()

  return (
    <section className="flex flex-col gap-4">
      <h1 className="text-lg font-semibold">{t('portal.renew.title')}</h1>
      <PayPanel />
    </section>
  )
}
