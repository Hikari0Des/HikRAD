import { useState } from 'react'

import { Ltr, useFormatters, useT } from '@hikrad/shared'

import { getInterfacesTab } from '../../../api/nasDevice'
import { formatBps } from '../../../lib/units'
import { useAsync } from '../../../hooks/useAsync'
import { DeviceTrafficChart } from './DeviceTrafficChart'
import { DeviceTabShell } from './DeviceTabShell'

/** FR-141 Interfaces tab: live per-interface state plus, once an interface is
 * selected, its rx/tx traffic history. */
export function InterfacesTab({ nasId }: { nasId: string }) {
  const t = useT()
  const { formatNumber } = useFormatters()
  const [selected, setSelected] = useState<string | undefined>(undefined)
  const { data, error, loading, reload } = useAsync(
    () => getInterfacesTab(nasId, selected),
    [nasId, selected],
  )

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
        <div className="space-y-4">
          <div className="overflow-x-auto rounded-md border border-border">
            <table className="w-full min-w-[48rem] text-sm">
              <thead className="bg-surface-sunken/40 text-xs text-ink-muted">
                <tr>
                  <th className="px-3 py-2 text-start font-medium">
                    {t('nas.device.interfaces.col.name')}
                  </th>
                  <th className="px-3 py-2 text-start font-medium">
                    {t('nas.device.interfaces.col.type')}
                  </th>
                  <th className="px-3 py-2 text-start font-medium">
                    {t('nas.device.interfaces.col.state')}
                  </th>
                  <th className="px-3 py-2 text-start font-medium">
                    {t('nas.device.interfaces.col.mac')}
                  </th>
                  <th className="px-3 py-2 text-end font-medium">
                    {t('nas.device.interfaces.col.mtu')}
                  </th>
                  <th className="px-3 py-2 text-end font-medium">
                    {t('nas.device.interfaces.col.rx')}
                  </th>
                  <th className="px-3 py-2 text-end font-medium">
                    {t('nas.device.interfaces.col.tx')}
                  </th>
                </tr>
              </thead>
              <tbody>
                {d.interfaces.map((ifc) => (
                  <tr
                    key={ifc.name}
                    className={`cursor-pointer border-t border-border/60 hover:bg-surface-sunken/40 ${
                      selected === ifc.name ? 'bg-brand-soft/40' : ''
                    }`}
                    onClick={() => setSelected(ifc.name)}
                  >
                    <td className="px-3 py-2 font-medium">
                      <Ltr>{ifc.name}</Ltr>
                    </td>
                    <td className="px-3 py-2 text-ink-muted">{ifc.type || '—'}</td>
                    <td className="px-3 py-2">
                      {ifc.disabled ? (
                        <span className="text-ink-muted">
                          {t('nas.device.interfaces.disabled')}
                        </span>
                      ) : ifc.running ? (
                        <span className="text-ok">{t('nas.device.interfaces.running')}</span>
                      ) : (
                        <span className="text-danger">{t('nas.device.interfaces.notRunning')}</span>
                      )}
                    </td>
                    <td className="px-3 py-2 text-ink-muted">
                      <Ltr>{ifc.mac_address || '—'}</Ltr>
                    </td>
                    <td className="px-3 py-2 text-end">{ifc.mtu || '—'}</td>
                    <td className="px-3 py-2 text-end">
                      <Ltr>{formatBps(ifc.rx_bps, formatNumber)}</Ltr>
                    </td>
                    <td className="px-3 py-2 text-end">
                      <Ltr>{formatBps(ifc.tx_bps, formatNumber)}</Ltr>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className="rounded-md border border-border bg-surface-raised p-4">
            <h2 className="mb-2 text-sm font-semibold">
              {d.selected_iface
                ? t('nas.device.interfaces.historyFor', { iface: d.selected_iface })
                : t('nas.device.interfaces.historyHint')}
            </h2>
            {d.traffic_history ? (
              <DeviceTrafficChart points={d.traffic_history} />
            ) : (
              <p className="text-sm text-ink-muted">{t('nas.device.interfaces.historyHint')}</p>
            )}
          </div>
        </div>
      ) : null}
    </DeviceTabShell>
  )
}
