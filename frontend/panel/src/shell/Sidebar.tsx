import { useEffect, useState } from 'react'
import { NavLink } from 'react-router-dom'

import { useT } from '@hikrad/shared'

import { listTickets } from '../api/paymentTickets'
import { useAuth } from '../auth/AuthContext'
import {
  PERM_AUDIT_VIEW,
  PERM_PAYMENT_TICKETS_VERIFY,
  PERM_PAYMENT_PROVIDERS_MANAGE,
  PERM_LIVE_VIEW,
  PERM_MANAGERS_VIEW,
  PERM_MONITORING_VIEW,
  PERM_NAS_VIEW,
  PERM_OVERHEADS_MANAGE,
  PERM_POOLS_VIEW,
  PERM_PROFILES_VIEW,
  PERM_REPORTS_VIEW,
  PERM_SETTINGS_VIEW,
  PERM_SUBSCRIBERS_CREATE,
  PERM_SUBSCRIBERS_VIEW,
  PERM_TOPUP,
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
      { key: 'nav.sessions', to: '/sessions', perm: PERM_LIVE_VIEW },
      { key: 'nav.import', to: '/import', perm: PERM_SUBSCRIBERS_CREATE },
    ],
  },
  {
    titleKey: 'nav.group.billing',
    items: [
      { key: 'nav.ledger', to: '/ledger', perm: PERM_REPORTS_VIEW },
      { key: 'nav.vouchers', to: '/vouchers', perm: PERM_VOUCHERS_VIEW },
      { key: 'nav.paymentTickets', to: '/payment-tickets', perm: PERM_PAYMENT_TICKETS_VERIFY },
      {
        key: 'nav.paymentProviders',
        to: '/payment-providers',
        perm: PERM_PAYMENT_PROVIDERS_MANAGE,
      },
      { key: 'nav.myPaymentMethods', to: '/my-payment-methods' },
      { key: 'nav.reports', to: '/reports', perm: PERM_REPORTS_VIEW },
      { key: 'nav.currencyRates', to: '/currency-rates', perm: PERM_TOPUP },
      { key: 'nav.pricingAdmin', to: '/pricing-admin', perm: PERM_OVERHEADS_MANAGE },
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
      { key: 'nav.settings', to: '/settings', perm: PERM_SETTINGS_VIEW },
      { key: 'nav.license', to: '/license' },
      { key: 'nav.account', to: '/account' },
    ],
  },
]

/** Polls the pending payment-ticket count for the nav badge (task 2c,
 * generalized in v2-2 from card-payments to every method). */
function usePendingTicketCount(enabled: boolean): number {
  const [count, setCount] = useState(0)
  useEffect(() => {
    if (!enabled) return
    let cancelled = false
    function load() {
      listTickets({ scope: 'mine', state: 'pending', limit: 100 })
        .then((res) => {
          if (!cancelled) setCount(res.items.length)
        })
        .catch(() => {
          /* badge is best-effort */
        })
    }
    load()
    const id = setInterval(load, 30_000)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [enabled])
  return count
}

export function SidebarContent({ onNavigate }: { onNavigate?: () => void }) {
  const t = useT()
  const { can } = useAuth()
  const pendingTickets = usePendingTicketCount(can(PERM_PAYMENT_TICKETS_VERIFY))

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
                        `flex items-center justify-between gap-2 rounded-md px-3 py-2 text-sm ${
                          isActive
                            ? 'bg-brand-soft font-medium text-brand-strong'
                            : 'text-ink hover:bg-surface-sunken'
                        }`
                      }
                    >
                      <span>{t(item.key)}</span>
                      {item.to === '/payment-tickets' && pendingTickets > 0 ? (
                        <span className="rounded-full bg-danger px-1.5 py-0.5 text-xs font-medium text-ink-inverse">
                          {pendingTickets}
                        </span>
                      ) : null}
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
