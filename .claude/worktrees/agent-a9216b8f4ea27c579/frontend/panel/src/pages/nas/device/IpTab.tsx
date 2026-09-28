import { Ltr, useT } from '@hikrad/shared'

import { getIpTab } from '../../../api/nasDevice'
import { OwnershipBadge } from '../../../components/OwnershipBadge'
import { useAsync } from '../../../hooks/useAsync'
import { DeviceTabShell } from './DeviceTabShell'

/** FR-143 IP tab: static addresses, DHCP client, DHCP servers, and the live
 * lease table — "the operator's answer to which device is that." DHCP
 * servers carry a per-row ownership badge from FR-108 pool ownership; every
 * other section is uniformly "router" (nothing else here is HikRAD-owned). */
export function IpTab({ nasId }: { nasId: string }) {
  const t = useT()
  const { data, error, loading, reload } = useAsync(() => getIpTab(nasId), [nasId])

  const d = data?.data ?? null

  return (
    <DeviceTabShell
      nasId={nasId}
      loading={loading}
      error={error}
      reload={reload}
      hasData={d !== null}
      readAt={data?.read_at ?? null}
      stale={data?.stale ?? true}
      ownership={data?.ownership ?? 'router'}
      noApiCredentials={data?.no_api_credentials ?? false}
    >
      {d ? (
        <div className="space-y-6">
          <section>
            <h2 className="mb-2 text-sm font-semibold">{t('nas.device.ip.addressesTitle')}</h2>
            {!d.snapshot.addresses || d.snapshot.addresses.length === 0 ? (
              <p className="text-sm text-ink-muted">{t('nas.device.ip.addressesEmpty')}</p>
            ) : (
              <div className="overflow-x-auto rounded-md border border-border">
                <table className="w-full min-w-[32rem] text-sm">
                  <thead className="bg-surface-sunken/40 text-xs text-ink-muted">
                    <tr>
                      <th className="px-3 py-2 text-start font-medium">
                        {t('nas.device.ip.col.address')}
                      </th>
                      <th className="px-3 py-2 text-start font-medium">
                        {t('nas.device.ip.col.interface')}
                      </th>
                      <th className="px-3 py-2 text-start font-medium">
                        {t('nas.device.ip.col.comment')}
                      </th>
                      <th className="px-3 py-2 text-end font-medium">
                        {t('nas.device.ip.col.status')}
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {d.snapshot.addresses.map((a, i) => (
                      <tr key={`${a.address}-${i}`} className="border-t border-border/60">
                        <td className="px-3 py-2 font-medium">
                          <Ltr>{a.address}</Ltr>
                        </td>
                        <td className="px-3 py-2">
                          <Ltr>{a.interface}</Ltr>
                        </td>
                        <td className="px-3 py-2 text-ink-muted">{a.comment || '—'}</td>
                        <td className="px-3 py-2 text-end">
                          {a.disabled ? (
                            <span className="text-ink-muted">
                              {t('nas.device.interfaces.disabled')}
                            </span>
                          ) : (
                            <span className="text-ok">{t('ui.enabled')}</span>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>

          <section>
            <h2 className="mb-2 text-sm font-semibold">{t('nas.device.ip.dhcpClientTitle')}</h2>
            {!d.snapshot.dhcp_clients || d.snapshot.dhcp_clients.length === 0 ? (
              <p className="text-sm text-ink-muted">{t('nas.device.ip.dhcpClientEmpty')}</p>
            ) : (
              <ul className="space-y-1 text-sm">
                {d.snapshot.dhcp_clients.map((c, i) => (
                  <li
                    key={`${c.interface}-${i}`}
                    className="flex flex-wrap items-center justify-between gap-2 rounded bg-surface-sunken/40 px-3 py-2"
                  >
                    <span>
                      <Ltr>{c.interface}</Ltr>
                    </span>
                    <span className="text-ink-muted">{c.status}</span>
                    <Ltr className="text-ink-muted">
                      {c.address}
                      {c.gateway ? ` → ${c.gateway}` : ''}
                    </Ltr>
                  </li>
                ))}
              </ul>
            )}
          </section>

          <section>
            <h2 className="mb-2 text-sm font-semibold">{t('nas.device.ip.dhcpServersTitle')}</h2>
            {!d.snapshot.dhcp_servers || d.snapshot.dhcp_servers.length === 0 ? (
              <p className="text-sm text-ink-muted">{t('nas.device.ip.dhcpServersEmpty')}</p>
            ) : (
              <ul className="space-y-1 text-sm">
                {d.snapshot.dhcp_servers.map((s) => (
                  <li
                    key={s.name}
                    className="flex flex-wrap items-center justify-between gap-2 rounded bg-surface-sunken/40 px-3 py-2"
                  >
                    <span className="font-medium">
                      <Ltr>{s.name}</Ltr>
                    </span>
                    <span className="text-ink-muted">
                      <Ltr>{s.interface}</Ltr> · {t('nas.device.ip.pool')}{' '}
                      <Ltr>{s.pool_name || '—'}</Ltr>
                    </span>
                    <OwnershipBadge ownership={d.dhcp_server_ownership?.[s.name] ?? 'router'} />
                  </li>
                ))}
              </ul>
            )}
          </section>

          <section>
            <h2 className="mb-2 text-sm font-semibold">{t('nas.device.ip.leasesTitle')}</h2>
            <p className="mb-2 text-xs text-ink-muted">{t('nas.device.ip.leasesHint')}</p>
            {!d.snapshot.leases || d.snapshot.leases.length === 0 ? (
              <p className="text-sm text-ink-muted">{t('nas.device.ip.leasesEmpty')}</p>
            ) : (
              <div className="overflow-x-auto rounded-md border border-border">
                <table className="w-full min-w-[48rem] text-sm">
                  <thead className="bg-surface-sunken/40 text-xs text-ink-muted">
                    <tr>
                      <th className="px-3 py-2 text-start font-medium">
                        {t('nas.device.ip.col.address')}
                      </th>
                      <th className="px-3 py-2 text-start font-medium">
                        {t('nas.device.ip.col.mac')}
                      </th>
                      <th className="px-3 py-2 text-start font-medium">
                        {t('nas.device.ip.col.hostname')}
                      </th>
                      <th className="px-3 py-2 text-start font-medium">
                        {t('nas.device.ip.col.server')}
                      </th>
                      <th className="px-3 py-2 text-end font-medium">
                        {t('nas.device.ip.col.status')}
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {d.snapshot.leases.map((lease, i) => (
                      <tr key={`${lease.address}-${i}`} className="border-t border-border/60">
                        <td className="px-3 py-2 font-medium">
                          <Ltr>{lease.address}</Ltr>
                        </td>
                        <td className="px-3 py-2">
                          <Ltr>{lease.mac || '—'}</Ltr>
                        </td>
                        <td className="px-3 py-2">{lease.hostname || '—'}</td>
                        <td className="px-3 py-2 text-ink-muted">
                          <Ltr>{lease.server}</Ltr>
                        </td>
                        <td className="px-3 py-2 text-end">
                          <span className={lease.status === 'bound' ? 'text-ok' : 'text-ink-muted'}>
                            {lease.status}
                          </span>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>
        </div>
      ) : null}
    </DeviceTabShell>
  )
}
