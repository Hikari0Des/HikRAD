import { Link } from 'react-router-dom'

import { useT } from '@hikrad/shared'

import { useAuth } from '../auth/AuthContext'
import { Card } from '@hikrad/shared'
import { PageHeader } from '../components/PageHeader'
import { PERM_MANAGERS_VIEW, PERM_SETTINGS_VIEW } from '../auth/permissions'

/**
 * v3-1 FR-99.1: the profile-centric "My account" area. One place that gathers
 * the signed-in person's own screens — security, preferences, payment methods —
 * plus quick links into the administration settings that concern them (admins
 * only). The individual routes are unchanged and keep working; this is a landing
 * that gathers them, reachable in ≤ 2 clicks from the user menu.
 */
interface Entry {
  to: string
  title: string
  desc: string
  perm?: string
}

export function MyAccountPage() {
  const t = useT()
  const { manager, can } = useAuth()

  const mine: Entry[] = [
    { to: '/account', title: t('account.security'), desc: t('account.securityDesc') },
    { to: '/preferences', title: t('account.preferences'), desc: t('account.preferencesDesc') },
    {
      to: '/my-payment-methods',
      title: t('account.paymentMethods'),
      desc: t('account.paymentMethodsDesc'),
    },
  ]

  const admin: Entry[] = [
    {
      to: '/settings',
      title: t('account.settings'),
      desc: t('account.settingsDesc'),
      perm: PERM_SETTINGS_VIEW,
    },
    {
      to: '/roles',
      title: t('account.roles'),
      desc: t('account.rolesDesc'),
      perm: PERM_MANAGERS_VIEW,
    },
  ].filter((e) => !e.perm || can(e.perm))

  return (
    <section>
      <PageHeader
        title={t('account.title')}
        subtitle={t('account.subtitle', { username: manager?.username ?? '' })}
      />

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {mine.map((e) => (
          <AccountLink key={e.to} entry={e} />
        ))}
      </div>

      {admin.length > 0 && (
        <>
          <h2 className="mb-2 mt-6 text-xs font-semibold uppercase tracking-wide text-ink-muted">
            {t('account.adminSection')}
          </h2>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {admin.map((e) => (
              <AccountLink key={e.to} entry={e} />
            ))}
          </div>
        </>
      )}
    </section>
  )
}

function AccountLink({ entry }: { entry: Entry }) {
  return (
    <Link
      to={entry.to}
      className="block rounded-lg outline-none ring-brand transition-shadow focus-visible:ring-2"
    >
      <Card className="h-full transition-shadow hover:shadow-md">
        <h3 className="font-semibold">{entry.title}</h3>
        <p className="mt-1 text-sm text-ink-muted">{entry.desc}</p>
      </Card>
    </Link>
  )
}
