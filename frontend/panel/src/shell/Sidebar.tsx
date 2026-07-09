import { NavLink } from 'react-router-dom'

import { useT } from '@hikrad/shared'

/**
 * Navigation slots for the whole product: Dashboard is live now; the other
 * entries are the Phase-2/3 screens (Subscribers, Profiles, NAS, Live
 * Sessions) rendered as disabled placeholders so later phases only flip
 * `placeholder: false` and add a route.
 */
const NAV_ITEMS: readonly { key: string; to: string; placeholder?: boolean }[] = [
  { key: 'nav.dashboard', to: '/' },
  { key: 'nav.subscribers', to: '/subscribers', placeholder: true },
  { key: 'nav.profiles', to: '/profiles', placeholder: true },
  { key: 'nav.nas', to: '/nas', placeholder: true },
  { key: 'nav.sessions', to: '/sessions', placeholder: true },
]

export function SidebarContent({ onNavigate }: { onNavigate?: () => void }) {
  const t = useT()
  return (
    <div className="flex h-full flex-col">
      <div className="border-b border-surface-sunken px-4 py-4">
        <span className="text-xl font-bold text-brand">{t('common.productName')}</span>
      </div>
      <nav className="flex-1 overflow-y-auto p-2">
        <ul className="space-y-1">
          {NAV_ITEMS.map((item) =>
            item.placeholder ? (
              <li key={item.key}>
                <span
                  aria-disabled="true"
                  className="flex items-center justify-between rounded-md px-3 py-2 text-sm text-ink-muted/60"
                >
                  {t(item.key)}
                  <span className="rounded bg-surface-sunken px-1.5 py-0.5 text-xs">
                    {t('nav.comingSoon')}
                  </span>
                </span>
              </li>
            ) : (
              <li key={item.key}>
                <NavLink
                  to={item.to}
                  end={item.to === '/'}
                  onClick={onNavigate}
                  className={({ isActive }) =>
                    `block rounded-md px-3 py-2 text-sm ${
                      isActive
                        ? 'bg-brand-soft font-medium text-brand-strong'
                        : 'text-ink hover:bg-surface-sunken'
                    }`
                  }
                >
                  {t(item.key)}
                </NavLink>
              </li>
            ),
          )}
        </ul>
      </nav>
      {import.meta.env.DEV && (
        <div className="border-t border-surface-sunken p-2">
          <NavLink
            to="/dev/rtl-smoke"
            onClick={onNavigate}
            className="block rounded-md px-3 py-2 text-xs text-ink-muted hover:bg-surface-sunken"
          >
            {t('smoke.title')}
          </NavLink>
        </div>
      )}
    </div>
  )
}
