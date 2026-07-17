import { NavLink, Route, Routes, Navigate } from 'react-router-dom'

import { useT } from '@hikrad/shared'

import { PageHeader } from '../../components/PageHeader'
import { BackupsSettings } from './BackupsSettings'
import { BillingSettings } from './BillingSettings'
import { BrandingSettings } from './BrandingSettings'
import { DataRetentionSettings } from './DataRetentionSettings'
import { LocaleSettings } from './LocaleSettings'
import { NotificationsSettings } from './NotificationsSettings'
import { RemoteAccessSettings } from './RemoteAccessSettings'
import { SystemSettings } from './SystemSettings'

const TABS = [
  { to: 'locale', key: 'settings.tab.locale' },
  { to: 'branding', key: 'settings.tab.branding' },
  { to: 'notifications', key: 'settings.tab.notifications' },
  { to: 'billing', key: 'settings.tab.billing' },
  { to: 'backups', key: 'settings.tab.backups' },
  { to: 'data-retention', key: 'settings.tab.dataRetention' },
  { to: 'remote-access', key: 'settings.tab.remoteAccess' },
  { to: 'system', key: 'settings.tab.system' },
]

/** Settings (FR-53): grouped screens, admin-permission-gated. */
export function SettingsPage() {
  const t = useT()
  return (
    <section>
      <PageHeader title={t('settings.title')} />
      <div className="flex flex-col gap-6 md:flex-row">
        <nav className="flex shrink-0 gap-1 overflow-x-auto md:w-48 md:flex-col md:overflow-visible">
          {TABS.map((tab) => (
            <NavLink
              key={tab.to}
              to={tab.to}
              className={({ isActive }) =>
                `shrink-0 rounded-md px-3 py-2 text-sm whitespace-nowrap ${
                  isActive
                    ? 'bg-brand-soft font-medium text-brand-strong'
                    : 'text-ink hover:bg-surface-sunken'
                }`
              }
            >
              {t(tab.key)}
            </NavLink>
          ))}
        </nav>
        <div className="min-w-0 flex-1">
          <Routes>
            <Route index element={<Navigate to="locale" replace />} />
            <Route path="locale" element={<LocaleSettings />} />
            <Route path="branding" element={<BrandingSettings />} />
            <Route path="notifications" element={<NotificationsSettings />} />
            <Route path="billing" element={<BillingSettings />} />
            <Route path="backups" element={<BackupsSettings />} />
            <Route path="data-retention" element={<DataRetentionSettings />} />
            <Route path="remote-access" element={<RemoteAccessSettings />} />
            <Route path="system" element={<SystemSettings />} />
          </Routes>
        </div>
      </div>
    </section>
  )
}
