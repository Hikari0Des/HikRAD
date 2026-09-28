import { Ltr, useFormatters, useT } from '@hikrad/shared'

import { getDnsTab } from '../../../api/nasDevice'
import { useAsync } from '../../../hooks/useAsync'
import { DeviceTabShell } from './DeviceTabShell'

/** FR-143 DNS tab: resolver servers, cache stats, static entries — read-only,
 * uniformly "router"-owned (HikRAD never manages a router's DNS config). */
export function DnsTab({ nasId }: { nasId: string }) {
  const t = useT()
  const { formatNumber } = useFormatters()
  const { data, error, loading, reload } = useAsync(() => getDnsTab(nasId), [nasId])

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
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
            <div className="rounded-md border border-border bg-surface-raised p-3">
              <p className="text-xs text-ink-muted">{t('nas.device.dns.servers')}</p>
              <p className="font-medium">
                <Ltr>{d.servers && d.servers.length > 0 ? d.servers.join(', ') : '—'}</Ltr>
              </p>
            </div>
            <div className="rounded-md border border-border bg-surface-raised p-3">
              <p className="text-xs text-ink-muted">{t('nas.device.dns.allowRemote')}</p>
              <p className="font-medium">{d.allow_remote_requests ? t('ui.yes') : t('ui.no')}</p>
            </div>
            <div className="rounded-md border border-border bg-surface-raised p-3">
              <p className="text-xs text-ink-muted">{t('nas.device.dns.cacheUsed')}</p>
              <p className="font-medium">
                <Ltr>
                  {formatNumber(d.cache_used)} / {formatNumber(d.cache_size)} KiB
                </Ltr>
              </p>
            </div>
          </div>

          <section>
            <h2 className="mb-2 text-sm font-semibold">{t('nas.device.dns.staticTitle')}</h2>
            {!d.static || d.static.length === 0 ? (
              <p className="text-sm text-ink-muted">{t('nas.device.dns.staticEmpty')}</p>
            ) : (
              <div className="overflow-x-auto rounded-md border border-border">
                <table className="w-full min-w-[32rem] text-sm">
                  <thead className="bg-surface-sunken/40 text-xs text-ink-muted">
                    <tr>
                      <th className="px-3 py-2 text-start font-medium">
                        {t('nas.device.dns.col.name')}
                      </th>
                      <th className="px-3 py-2 text-start font-medium">
                        {t('nas.device.dns.col.address')}
                      </th>
                      <th className="px-3 py-2 text-start font-medium">
                        {t('nas.device.dns.col.comment')}
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {d.static.map((entry, i) => (
                      <tr key={`${entry.name}-${i}`} className="border-t border-border/60">
                        <td className="px-3 py-2 font-medium">
                          <Ltr>{entry.name}</Ltr>
                        </td>
                        <td className="px-3 py-2">
                          <Ltr>{entry.address}</Ltr>
                        </td>
                        <td className="px-3 py-2 text-ink-muted">{entry.comment || '—'}</td>
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
