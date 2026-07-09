import { NavLink, Outlet } from 'react-router-dom'

import { useT } from '@hikrad/shared'

import { BRANDING } from '../branding'
import { LanguageSwitcher } from '../components/LanguageSwitcher'

const TABS = [
  { to: '/home', key: 'portal.nav.home' },
  { to: '/usage', key: 'portal.nav.usage' },
  { to: '/renew', key: 'portal.nav.renew' },
] as const

/**
 * Mobile-first single-column shell: brand header, routed content, bottom tab
 * navigation (Home / Usage / Renew). All layout logical — mirrors under RTL.
 */
export function PortalLayout() {
  const t = useT()
  return (
    <div className="mx-auto flex min-h-screen w-full max-w-md flex-col">
      <header className="flex items-center justify-between gap-2 px-4 py-3">
        <span className="flex items-center gap-2 font-semibold">
          <span
            aria-hidden="true"
            className="flex h-7 w-7 items-center justify-center rounded-md bg-brand text-sm text-ink-inverse"
          >
            {BRANDING.initial}
          </span>
          {BRANDING.name}
        </span>
        <LanguageSwitcher />
      </header>

      <main className="flex-1 px-4 pb-20 pt-2">
        <Outlet />
      </main>

      <nav
        aria-label={t('portal.nav.label')}
        className="fixed bottom-0 inset-x-0 mx-auto flex w-full max-w-md border-t border-surface-sunken bg-surface-raised"
      >
        {TABS.map((tab) => (
          <NavLink
            key={tab.to}
            to={tab.to}
            className={({ isActive }) =>
              `flex-1 py-3 text-center text-sm transition-colors ${
                isActive ? 'font-semibold text-brand' : 'text-ink-muted hover:text-ink'
              }`
            }
          >
            {t(tab.key)}
          </NavLink>
        ))}
      </nav>
    </div>
  )
}
