import { useEffect, useState } from 'react'

import { Ltr, useFormatters, useT } from '@hikrad/shared'

import { disconnectSession, openLiveStream } from '../../api/live'
import type { LiveSession } from '../../api/types'
import { useAuth } from '../../auth/AuthContext'
import { PERM_DISCONNECT } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { useToast } from '../../components/Toast'
import { applyLiveEvent, type LiveMap } from '../../lib/liveReducer'
import { formatBps, formatBytes } from '../../lib/units'

/**
 * Live-session widget for one subscriber (FR-3), SSE-driven. The feed is not
 * filterable by subscriber, so we subscribe to the stream and keep only this
 * subscriber's rows in the reducer; a reconnect snapshot re-syncs them.
 */
export function LiveWidget({ subscriberId }: { subscriberId: string }) {
  const t = useT()
  const { formatNumber, formatDate } = useFormatters()
  const { can } = useAuth()
  const { toast } = useToast()

  const [sessions, setSessions] = useState<LiveSession[]>([])
  const [confirm, setConfirm] = useState<LiveSession | null>(null)

  useEffect(() => {
    let map: LiveMap = new Map()
    const sync = () => {
      const mine: LiveSession[] = []
      for (const s of map.values()) if (s.subscriber_id === subscriberId) mine.push(s)
      setSessions(mine)
    }
    const handle = openLiveStream(
      {},
      {
        onEvent: (evt) => {
          map = applyLiveEvent(map, evt)
          sync()
        },
      },
    )
    return () => handle.close()
  }, [subscriberId])

  async function doDisconnect(s: LiveSession) {
    try {
      const res = await disconnectSession(s.nas_id, s.acct_session_id)
      if (res.outcome === 'ack') toast(t('live.disconnectAck'), 'ok')
      else toast(t('live.disconnectFail', { outcome: res.outcome }), 'danger')
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    }
  }

  if (sessions.length === 0) {
    return <p className="text-sm text-ink-muted">{t('live.noneForUser')}</p>
  }

  return (
    <div className="space-y-2">
      {sessions.map((s) => (
        <div
          key={`${s.nas_id}:${s.acct_session_id}`}
          className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-surface-sunken p-3 text-sm"
        >
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <span className="inline-block h-2 w-2 rounded-full bg-ok" aria-hidden="true" />
              <Ltr className="font-medium">{s.ip || '—'}</Ltr>
              <span className="rounded bg-surface-sunken px-1.5 py-0.5 text-xs">
                {t(`live.service.${s.service}`)}
              </span>
              {s.stale && (
                <span className="text-xs text-danger" title={t('live.staleHint')}>
                  {t('live.stale')}
                </span>
              )}
            </div>
            <div className="mt-1 text-xs text-ink-muted">
              <Ltr>{s.mac || '—'}</Ltr> · {t('live.since')}{' '}
              {formatDate(s.started_at, { dateStyle: 'short' })}
            </div>
            <div className="mt-0.5 text-xs text-ink-muted">
              ↓ {formatBytes(s.bytes_in, formatNumber)} · ↑ {formatBytes(s.bytes_out, formatNumber)}{' '}
              · {formatBps(s.rate_down_bps, formatNumber)} /{' '}
              {formatBps(s.rate_up_bps, formatNumber)}
            </div>
          </div>
          {can(PERM_DISCONNECT) && (
            <Button size="sm" variant="danger" onClick={() => setConfirm(s)}>
              {t('live.disconnect')}
            </Button>
          )}
        </div>
      ))}

      <ConfirmDialog
        open={confirm !== null}
        onOpenChange={(o) => !o && setConfirm(null)}
        title={t('live.disconnectTitle')}
        body={t('live.disconnectBody')}
        confirmLabel={t('live.disconnect')}
        destructive
        onConfirm={async () => {
          if (confirm) await doDisconnect(confirm)
        }}
      />
    </div>
  )
}
