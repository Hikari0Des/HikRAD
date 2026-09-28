import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'

import { useT } from '@hikrad/shared'

import { useAuth } from './AuthContext'

/**
 * Friendly no-access state for a screen the manager's role does not include.
 * Rendered by RequirePerm on direct navigation (the sidebar already hides
 * gated entries), so it must explain itself instead of looking like a crash.
 */
export function NoAccessPage() {
  const t = useT()
  return (
    <section className="flex flex-col items-center gap-3 py-16 text-center">
      <p className="text-lg font-semibold">{t('noAccess.title')}</p>
      <p className="max-w-md text-sm text-ink-muted">{t('noAccess.body')}</p>
      <Link to="/" className="mt-2 font-medium text-brand-strong hover:underline">
        {t('noAccess.backHome')}
      </Link>
    </section>
  )
}

/**
 * Route-level permission gate. The server re-checks every request — this only
 * keeps managers from landing on a screen whose every fetch would 403 (which
 * used to surface as a generic "Something went wrong").
 */
export function RequirePerm({ perm, children }: { perm: string; children: ReactNode }) {
  const { can } = useAuth()
  if (!can(perm)) return <NoAccessPage />
  return <>{children}</>
}
