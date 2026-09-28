import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'

import { ErrorState, LoadingState, Ltr, useFormatters, useT, type TFunction } from '@hikrad/shared'

import { listDeviceMonitor, refreshDevice } from '../../api/nasDevice'
import type { DeviceMonitorRow } from '../../api/types'
import { Button } from '../../components/Button'
import { PageHeader } from '../../components/PageHeader'
import { Pill } from '../../components/Pill'
import { useAsync } from '../../hooks/useAsync'

/** Reachability dot (FR-139). undefined = never sampled — a distinct third
 * state from up/down, since "no API credentials yet" is not a failure. */
function ReachDot({ reachable }: { reachable: boolean | undefined }) {
  const cls = reachable === undefined ? 'bg-ink-muted/40' : reachable ? 'bg-ok' : 'bg-danger'
  return (
    <span aria-hidden="true" className={`inline-block h-2.5 w-2.5 shrink-0 rounded-full ${cls}`} />
  )
}

function formatUptime(secs: number | undefined, t: TFunction): string {
  if (secs === undefined) return '—'
  const days = Math.floor(secs / 86400)
  const hours = Math.floor((secs % 86400) / 3600)
  if (days > 0) return t('nas.device.uptimeDays', { d: days, h: hours })
  const mins = Math.floor((secs % 3600) / 60)
  return t('nas.device.uptimeHours', { h: hours, m: mins })
}

/** FR-139 monitor grid: reachability, RouterOS version, CPU load/cores,
 * memory, session count, uptime, NAS-vs-monitor tag. Auto-refreshes on the
 * device worker's own cadence (60s) with an explicit per-row "refresh now" —
 * both read from the worker's samples, never a synchronous router call. */
export function DeviceMonitorPage() {
  const t = useT()
  const { formatNumber } = useFormatters()
  const { data, error, loading, reload } = useAsync(() => listDeviceMonitor(), [])
  const [refreshing, setRefreshing] = useState<string | null>(null)

  const pollRef = useRef(reload)
  pollRef.current = reload
  useEffect(() => {
    const id = setInterval(() => pollRef.current(), 60_000)
    return () => clearInterval(id)
  }, [])

  async function runRefresh(row: DeviceMonitorRow) {
    setRefreshing(row.id)
    try {
      await refreshDevice(row.id)
      // The worker tick runs in the background; give it a moment before polling.
      setTimeout(reload, 3000)
    } finally {
      setRefreshing(null)
    }
  }

  return (
    <section>
      <PageHeader
        title={t('nas.device.monitor.title')}
        actions={
          <Button size="sm" variant="secondary" onClick={reload}>
            {t('nas.device.refreshNow')}
          </Button>
        }
      />

      {error ? (
        <ErrorState onRetry={reload} />
      ) : loading || !data ? (
        <LoadingState />
      ) : data.items.length === 0 ? (
        <p className="text-sm text-ink-muted">{t('nas.device.monitor.empty')}</p>
      ) : (
        <div className="overflow-x-auto rounded-md border border-border">
          <table className="w-full min-w-[64rem] text-sm">
            <thead className="bg-surface-sunken/40 text-xs text-ink-muted">
              <tr>
                <Th>{t('nas.device.monitor.col.device')}</Th>
                <Th>{t('nas.device.monitor.col.kind')}</Th>
                <Th>{t('nas.device.monitor.col.rosVersion')}</Th>
                <Th className="text-end">{t('nas.device.monitor.col.cpu')}</Th>
                <Th className="text-end">{t('nas.device.monitor.col.mem')}</Th>
                <Th className="text-end">{t('nas.device.monitor.col.sessions')}</Th>
                <Th>{t('nas.device.monitor.col.uptime')}</Th>
                <Th />
              </tr>
            </thead>
            <tbody>
              {data.items.map((row) => (
                <tr key={row.id} className="border-t border-border/60">
                  <td className="px-3 py-2">
                    <div className="flex items-center gap-2">
                      <ReachDot reachable={row.reachable} />
                      {row.kind === 'nas' ? (
                        <Link
                          to={`/nas/${row.id}/device`}
                          className="min-w-0 truncate font-medium text-brand-strong hover:underline"
                        >
                          {row.name}
                        </Link>
                      ) : (
                        <span className="min-w-0 truncate font-medium">{row.name}</span>
                      )}
                    </div>
                    <p className="text-xs text-ink-muted">
                      <Ltr>{row.ip}</Ltr>
                    </p>
                  </td>
                  <td className="px-3 py-2">
                    <Pill tone={row.kind === 'nas' ? 'brand' : 'muted'}>
                      {row.kind === 'nas'
                        ? t('nas.device.monitor.tagNas')
                        : t('nas.device.monitor.tagMonitor')}
                    </Pill>
                  </td>
                  <td className="px-3 py-2">
                    <Ltr>{row.ros_version || '—'}</Ltr>
                  </td>
                  <td className="px-3 py-2 text-end">
                    {row.cpu_percent === undefined
                      ? '—'
                      : t('nas.device.monitor.cpuValue', {
                          pct: formatNumber(row.cpu_percent, { maximumFractionDigits: 0 }),
                          cores: row.cpu_cores === undefined ? '?' : formatNumber(row.cpu_cores),
                        })}
                  </td>
                  <td className="px-3 py-2 text-end">
                    {row.mem_percent === undefined
                      ? '—'
                      : `${formatNumber(row.mem_percent, { maximumFractionDigits: 0 })}%`}
                  </td>
                  <td className="px-3 py-2 text-end">
                    {row.session_count === undefined ? '—' : formatNumber(row.session_count)}
                  </td>
                  <td className="px-3 py-2">{formatUptime(row.uptime_secs, t)}</td>
                  <td className="px-3 py-2 text-end">
                    {row.kind === 'nas' && row.has_api_credentials ? (
                      <Button
                        size="sm"
                        variant="ghost"
                        disabled={refreshing === row.id}
                        onClick={() => runRefresh(row)}
                      >
                        {refreshing === row.id ? t('ui.working') : t('nas.device.refreshNow')}
                      </Button>
                    ) : row.kind === 'nas' ? (
                      <span className="text-xs text-ink-muted">{t('nas.device.noCredsShort')}</span>
                    ) : null}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}

function Th({ children, className = '' }: { children?: ReactNode; className?: string }) {
  return <th className={`px-3 py-2 text-start font-medium ${className}`}>{children}</th>
}
