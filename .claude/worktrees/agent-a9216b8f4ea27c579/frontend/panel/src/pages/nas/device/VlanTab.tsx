import { Ltr, useFormatters, useT } from '@hikrad/shared'

import { getVlanTab } from '../../../api/nasDevice'
import { OwnershipBadge } from '../../../components/OwnershipBadge'
import { useAsync } from '../../../hooks/useAsync'
import { DeviceTabShell } from './DeviceTabShell'

/** FR-143 VLAN tab: VLAN interfaces, read-only. */
export function VlanTab({ nasId }: { nasId: string }) {
  const t = useT()
  const { formatNumber } = useFormatters()
  const { data, error, loading, reload } = useAsync(() => getVlanTab(nasId), [nasId])

  const list = data?.data ?? null

  return (
    <DeviceTabShell
      nasId={nasId}
      loading={loading}
      error={error}
      reload={reload}
      hasData={list !== null}
      readAt={data?.read_at ?? null}
      stale={data?.stale ?? true}
      ownership={data?.ownership ?? 'router'}
      noApiCredentials={data?.no_api_credentials ?? false}
    >
      {list ? (
        list.length === 0 ? (
          <p className="text-sm text-ink-muted">{t('nas.device.vlan.empty')}</p>
        ) : (
          <div className="overflow-x-auto rounded-md border border-border">
            <table className="w-full min-w-[36rem] text-sm">
              <thead className="bg-surface-sunken/40 text-xs text-ink-muted">
                <tr>
                  <th className="px-3 py-2 text-start font-medium">
                    {t('nas.device.vlan.col.name')}
                  </th>
                  <th className="px-3 py-2 text-end font-medium">{t('nas.device.vlan.col.id')}</th>
                  <th className="px-3 py-2 text-start font-medium">
                    {t('nas.device.vlan.col.interface')}
                  </th>
                  <th className="px-3 py-2 text-start font-medium">
                    {t('nas.device.vlan.col.comment')}
                  </th>
                  <th className="px-3 py-2 text-end font-medium">
                    {t('nas.device.vlan.col.ownership')}
                  </th>
                </tr>
              </thead>
              <tbody>
                {list.map((entry) => (
                  <tr key={entry.item.name} className="border-t border-border/60">
                    <td className="px-3 py-2 font-medium">
                      <Ltr>{entry.item.name}</Ltr>
                    </td>
                    <td className="px-3 py-2 text-end">
                      <Ltr>{formatNumber(entry.item.vlan_id)}</Ltr>
                    </td>
                    <td className="px-3 py-2">
                      <Ltr>{entry.item.interface}</Ltr>
                    </td>
                    <td className="px-3 py-2 text-ink-muted">{entry.item.comment || '—'}</td>
                    <td className="px-3 py-2 text-end">
                      <OwnershipBadge ownership={entry.ownership} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )
      ) : null}
    </DeviceTabShell>
  )
}
