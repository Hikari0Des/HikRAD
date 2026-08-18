import { Navigate, NavLink, Route, Routes, useParams } from 'react-router-dom'

import { useT } from '@hikrad/shared'

import { getNas } from '../../../api/nas'
import { PageHeader } from '../../../components/PageHeader'
import { useAsync } from '../../../hooks/useAsync'
import { BridgeTab } from './BridgeTab'
import { DnsTab } from './DnsTab'
import { FirewallTab } from './FirewallTab'
import { GeneralTab } from './GeneralTab'
import { InterfacesTab } from './InterfacesTab'
import { InternetTab } from './InternetTab'
import { IpTab } from './IpTab'
import { OverviewTab } from './OverviewTab'
import { UpdateTab } from './UpdateTab'
import { VlanTab } from './VlanTab'

const TAB_GROUPS = [
  {
    titleKey: 'nas.device.group.monitoring',
    tabs: [
      { to: 'overview', key: 'nas.device.tab.overview' },
      { to: 'interfaces', key: 'nas.device.tab.interfaces' },
    ],
  },
  {
    titleKey: 'nas.device.group.manager',
    tabs: [
      { to: 'general', key: 'nas.device.tab.general' },
      { to: 'update', key: 'nas.device.tab.update' },
    ],
  },
  {
    titleKey: 'nas.device.group.network',
    tabs: [
      { to: 'bridge', key: 'nas.device.tab.bridge' },
      { to: 'vlan', key: 'nas.device.tab.vlan' },
      { to: 'ip', key: 'nas.device.tab.ip' },
      { to: 'dns', key: 'nas.device.tab.dns' },
      { to: 'internet', key: 'nas.device.tab.internet' },
      { to: 'firewall', key: 'nas.device.tab.firewall' },
    ],
  },
] as const

/**
 * FR-140 device page shell: tab groups (Monitoring / Manager / Network), all
 * read through the read-only cache — no tab in this phase ever writes to the
 * router (reboot/ping in General excepted).
 */
export function DevicePage() {
  const t = useT()
  const { id = '' } = useParams()
  // Resolve the NAS purely for its name/ip in the header — never the raw id.
  const nas = useAsync(() => getNas(id), [id])

  return (
    <section>
      <PageHeader title={nas.data?.name ?? ''} subtitle={nas.data?.ip} />
      <div className="flex flex-col gap-6 md:flex-row">
        <nav className="flex shrink-0 flex-col gap-4 md:w-48">
          {TAB_GROUPS.map((group) => (
            <div key={group.titleKey}>
              <p className="mb-1 px-3 text-xs font-semibold uppercase tracking-wide text-ink-muted/70">
                {t(group.titleKey)}
              </p>
              <ul className="flex gap-1 overflow-x-auto md:flex-col md:overflow-visible">
                {group.tabs.map((tab) => (
                  <li key={tab.to}>
                    <NavLink
                      to={tab.to}
                      className={({ isActive }) =>
                        `block shrink-0 rounded-md px-3 py-2 text-sm whitespace-nowrap ${
                          isActive
                            ? 'bg-brand-soft font-medium text-brand-strong'
                            : 'text-ink hover:bg-surface-sunken'
                        }`
                      }
                    >
                      {t(tab.key)}
                    </NavLink>
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </nav>
        <div className="min-w-0 flex-1">
          <Routes>
            <Route index element={<Navigate to="overview" replace />} />
            <Route path="overview" element={<OverviewTab nasId={id} />} />
            <Route path="interfaces" element={<InterfacesTab nasId={id} />} />
            <Route path="general" element={<GeneralTab nasId={id} />} />
            <Route path="bridge" element={<BridgeTab nasId={id} />} />
            <Route path="vlan" element={<VlanTab nasId={id} />} />
            <Route path="ip" element={<IpTab nasId={id} />} />
            <Route path="dns" element={<DnsTab nasId={id} />} />
            <Route path="update" element={<UpdateTab nasId={id} />} />
            <Route path="internet" element={<InternetTab nasId={id} />} />
            <Route path="firewall" element={<FirewallTab nasId={id} />} />
          </Routes>
        </div>
      </div>
    </section>
  )
}
