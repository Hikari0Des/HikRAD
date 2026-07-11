import { NavLink } from 'react-router-dom'

import { useT } from '@hikrad/shared'

import { useAuth } from '../auth/AuthContext'
import {
  PERM_AUDIT_VIEW,
  PERM_MANAGERS_VIEW,
  PERM_MONITORING_VIEW,
  PERM_NAS_VIEW,
  PERM_POOLS_VIEW,
  PERM_PROFILES_VIEW,
  PERM_REPORTS_VIEW,
  PERM_SUBSCRIBERS_VIEW,
  PERM_VOUCHERS_VIEW,
} from '../auth/permissions'

interface NavItem {
  key: string
  to: string
  /** Permission required to see the item; undefined = always visible. */
  perm?: string
}

interface NavGroup {
  titleKey?: string
  items: NavItem[]
}

/** Navigation, grouped and permission-gated (the server re-checks every route). */
const NAV_GROUPS: readonly NavGroup[] = [
  {
    items: [
      { key: 'nav.dashboard', to: '/' },
      { key: 'nav.subscribers', to: '/subscribers', perm: PERM_SUBSCRIBERS_VIEW },
      { key: 'nav.profiles', to: '/profiles', perm: PERM_PROFILES_VIEW },
      { key: 'nav.sessions', to: '/sessions' },
    ],
  },
  {
    titleKey: 'nav.group.billing',
    items: [
      { key: 'nav.ledger', to: '/ledger', perm: PERM_REPORTS_VIEW },
      { key: 'nav.vouchers', to: '/vouchers', perm: PERM_VOUCHERS_VIEW },
    ],
  },
  {
    titleKey: 'nav.group.network',
    items: [
      { key: 'nav.nas', to: '/nas', perm: PERM_NAS_VIEW },
      { key: 'nav.pools', to: '/pools', perm: PERM_POOLS_VIEW },
      { key: 'nav.devices', to: '/devices', perm: PERM_MONITORING_VIEW },
      { key: 'nav.alerts', to: '/alerts', perm: PERM_MONITORING_VIEW },
      { key: 'nav.health', to: '/health', perm: PERM_MONITORING_VIEW },
      { key: 'nav.debug', to: '/debug', perm: PERM_NAS_VIEW },
    ],
  },
  {
    titleKey: 'nav.group.admin',
    items: [
      { key: 'nav.managers', to: '/managers', perm: PERM_MANAGERS_VIEW },
      { key: 'nav.roles', to: '/roles', perm: PERM_MANAGERS_VIEW },
      { key: 'nav.auditLog', to: '/audit-log', perm: PERM_AUDIT_VIEW },
      { key: 'nav.account', to: '/account' },
    ],
  },
]

export function SidebarContent({ onNavigate }: { onNavigate?: () => void }) {
  const t = useT()
  const { can } = useAuth()

  return (
    <div className="flex h-full flex-col">
      <div className="border-b border-surface-sunken px-4 py-4">
        <span className="text-xl font-bold text-brand">{t('common.productName')}</span>
      </div>
      <nav className="flex-1 overflow-y-auto p-2">
        {NAV_GROUPS.map((group, gi) => {
          const visible = group.items.filter((item) => !item.perm || can(item.perm))
          if (visible.length === 0) return null
          return (
            <div key={gi} className={gi > 0 ? 'mt-4' : ''}>
              {group.titleKey ? (
                <p className="px-3 pb-1 text-xs font-semibold uppercase tracking-wide text-ink-muted/70">
                  {t(group.titleKey)}
                </p>
              ) : null}
              <ul className="space-y-1">
                {visible.map((item) => (
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
                ))}
              </ul>
            </div>
          )
        })}
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
