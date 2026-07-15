import { NavLink, Outlet } from 'react-router-dom'

import { useT } from '@hikrad/shared'

import { useAuth } from '../auth/AuthContext'
import { brandInitial, useBranding } from '../branding'
import { LanguageSwitcher } from '../components/LanguageSwitcher'

const TABS = [
  { to: '/home', key: 'portal.nav.home' },
  { to: '/usage', key: 'portal.nav.usage' },
  { to: '/renew', key: 'portal.nav.renew' },
  { to: '/settings', key: 'portal.nav.settings' },
] as const

/**
 * Mobile-first single-column shell: brand header, routed content, bottom tab
 * navigation. All layout logical — mirrors under RTL.
 */
export function PortalLayout() {
  const t = useT()
  const branding = useBranding()
  const { logout } = useAuth()

  return (
    <div className="mx-auto flex min-h-screen w-full max-w-md flex-col">
      <header className="flex items-center justify-between gap-2 px-4 py-3">
        <span className="flex items-center gap-2 font-semibold">
          {branding.logo_url ? (
            <img
              src={branding.logo_url}
              alt=""
              aria-hidden="true"
              className="h-7 w-7 rounded-md object-contain"
            />
          ) : (
            <span
              aria-hidden="true"
              className="flex h-7 w-7 items-center justify-center rounded-md bg-brand text-sm text-ink-inverse"
            >
              {brandInitial(branding.name)}
            </span>
          )}
          {branding.name}
        </span>
        <div className="flex items-center gap-2">
          <LanguageSwitcher />
          <button
            type="button"
            onClick={logout}
            aria-label={t('portal.nav.logout')}
            className="rounded-md p-2 text-ink-muted hover:bg-surface-sunken"
          >
            <svg
              aria-hidden="true"
              width="16"
              height="16"
              viewBox="0 0 16 16"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.5"
            >
              <path d="M6 2H3.5A1.5 1.5 0 0 0 2 3.5v9A1.5 1.5 0 0 0 3.5 14H6M10.5 11l3-3-3-3M13.25 8H6" />
            </svg>
          </button>
        </div>
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
