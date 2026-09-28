import { Ltr, StatCard, useFormatters, useT } from '@hikrad/shared'

import { getOverviewTab } from '../../../api/nasDevice'
import { useAsync } from '../../../hooks/useAsync'
import { DeviceHistoryChart } from './DeviceHistoryChart'
import { DeviceTabShell } from './DeviceTabShell'

function formatUptime(secs: number, formatNumber: (v: number) => string): string {
  const days = Math.floor(secs / 86400)
  const hours = Math.floor((secs % 86400) / 3600)
  const mins = Math.floor((secs % 3600) / 60)
  if (days > 0) return `${formatNumber(days)}d ${formatNumber(hours)}h`
  if (hours > 0) return `${formatNumber(hours)}h ${formatNumber(mins)}m`
  return `${formatNumber(mins)}m`
}

/** FR-141 Overview tab: identity, model, RouterOS version, serial, uptime,
 * and CPU/mem/session history charts sourced entirely from device_metrics. */
export function OverviewTab({ nasId }: { nasId: string }) {
  const t = useT()
  const { formatNumber } = useFormatters()
  const { data, error, loading, reload } = useAsync(() => getOverviewTab(nasId), [nasId])

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
            <StatCard label={t('nas.device.overview.identity')} value={d.identity || '—'} />
            <StatCard label={t('nas.device.overview.model')} value={d.board_name || '—'} />
            <StatCard label={t('nas.device.overview.rosVersion')} value={d.ros_version || '—'} />
            <StatCard
              label={t('nas.device.overview.serial')}
              value={<Ltr>{d.serial_number || '—'}</Ltr>}
            />
            <StatCard
              label={t('nas.device.overview.cpu')}
              value={`${formatNumber(d.cpu_percent, { maximumFractionDigits: 0 })}% (${formatNumber(d.cpu_cores)})`}
            />
            <StatCard
              label={t('nas.device.overview.mem')}
              value={`${formatNumber(d.mem_percent, { maximumFractionDigits: 0 })}%`}
            />
            <StatCard
              label={t('nas.device.overview.sessions')}
              value={formatNumber(d.session_count)}
            />
            <StatCard
              label={t('nas.device.overview.uptime')}
              value={formatUptime(d.uptime_secs, formatNumber)}
            />
          </div>
          <div className="rounded-md border border-border bg-surface-raised p-4">
            <h2 className="mb-2 text-sm font-semibold">{t('nas.device.overview.historyTitle')}</h2>
            <DeviceHistoryChart points={d.history} />
          </div>
        </div>
      ) : null}
    </DeviceTabShell>
  )
}
