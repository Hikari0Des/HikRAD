import { Ltr, useT } from '@hikrad/shared'

import { getBridgeTab } from '../../../api/nasDevice'
import { OwnershipBadge } from '../../../components/OwnershipBadge'
import { useAsync } from '../../../hooks/useAsync'
import { DeviceTabShell } from './DeviceTabShell'

/** FR-143 Bridge tab: bridges and their member ports, read-only. Every
 * bridge badges "router" — v4-6 never creates one (that's v4-7's wizard). */
export function BridgeTab({ nasId }: { nasId: string }) {
  const t = useT()
  const { data, error, loading, reload } = useAsync(() => getBridgeTab(nasId), [nasId])

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
          <p className="text-sm text-ink-muted">{t('nas.device.bridge.empty')}</p>
        ) : (
          <div className="space-y-3">
            {list.map((entry) => (
              <div
                key={entry.item.name}
                className="rounded-md border border-border bg-surface-raised p-4"
              >
                <div className="flex items-center justify-between gap-2">
                  <h3 className="font-medium">
                    <Ltr>{entry.item.name}</Ltr>
                  </h3>
                  <OwnershipBadge ownership={entry.ownership} />
                </div>
                {entry.item.comment ? (
                  <p className="mt-1 text-xs text-ink-muted">{entry.item.comment}</p>
                ) : null}
                {entry.item.ports && entry.item.ports.length > 0 ? (
                  <ul className="mt-3 space-y-1 text-sm">
                    {entry.item.ports.map((p) => (
                      <li
                        key={p.interface}
                        className="flex items-center justify-between rounded bg-surface-sunken/40 px-2 py-1"
                      >
                        <Ltr>{p.interface}</Ltr>
                        <span className="text-xs text-ink-muted">
                          {t('nas.device.bridge.pvid', { pvid: p.pvid })}
                        </span>
                      </li>
                    ))}
                  </ul>
                ) : (
                  <p className="mt-2 text-xs text-ink-muted">{t('nas.device.bridge.noPorts')}</p>
                )}
              </div>
            ))}
          </div>
        )
      ) : null}
    </DeviceTabShell>
  )
}
