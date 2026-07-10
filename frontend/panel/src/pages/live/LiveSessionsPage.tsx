import { useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'

import { Ltr, useFormatters, useT } from '@hikrad/shared'

import { disconnectSession, openLiveStream, type LiveFilter } from '../../api/live'
import { listNas } from '../../api/nas'
import { listProfiles } from '../../api/profiles'
import { listManagers, type ManagerView } from '../../api/managers'
import type { LiveSession, Nas, Profile } from '../../api/types'
import { useAuth } from '../../auth/AuthContext'
import { PERM_DISCONNECT } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { PageHeader } from '../../components/PageHeader'
import { Select, TextInput } from '../../components/form'
import { VirtualList } from '../../components/VirtualList'
import { useToast } from '../../components/Toast'
import { useAsync } from '../../hooks/useAsync'
import { useDebouncedValue } from '../../hooks/useDebouncedValue'
import { applyLiveEvent, type LiveMap } from '../../lib/liveReducer'
import { formatBps, formatBytes } from '../../lib/units'

const ROW_HEIGHT = 52

/**
 * Live Sessions (FR-31): an SSE table that renders a snapshot immediately then
 * applies upsert/remove deltas with no page refresh. Filters (NAS/profile/
 * manager/search) re-open the stream so the server replays a fresh snapshot and
 * the reducer re-syncs (no ghost rows on reconnect). Rows are virtualized so 2k
 * sessions scroll smoothly. Stale rows are dimmed; each row can Disconnect
 * (permission-aware, CoA result surfaced) or open the subscriber.
 */
export function LiveSessionsPage() {
  const t = useT()
  const { formatNumber } = useFormatters()
  const { can } = useAuth()
  const { toast } = useToast()

  const nasQ = useAsync(() => listNas(), [])
  const profilesQ = useAsync(() => listProfiles(true), [])
  const managersQ = useAsync(() => listManagers().catch(() => ({ items: [] as ManagerView[] })), [])
  const nasList: Nas[] = useMemo(() => nasQ.data?.items ?? [], [nasQ.data])
  const profiles: Profile[] = profilesQ.data?.items ?? []
  const managers: ManagerView[] = managersQ.data?.items ?? []

  const [nasId, setNasId] = useState('')
  const [profileId, setProfileId] = useState('')
  const [managerId, setManagerId] = useState('')
  const [q, setQ] = useState('')
  const debouncedQ = useDebouncedValue(q.trim(), 300)

  const [sessions, setSessions] = useState<LiveSession[]>([])
  const [status, setStatus] = useState<'connecting' | 'live' | 'reconnecting'>('connecting')
  const [confirm, setConfirm] = useState<LiveSession | null>(null)
  const mapRef = useRef<LiveMap>(new Map())

  const filterKey = `${nasId}|${profileId}|${managerId}|${debouncedQ}`

  useEffect(() => {
    const filter: LiveFilter = {}
    if (nasId) filter.nas_id = nasId
    if (profileId) filter.profile_id = profileId
    if (managerId) filter.manager_id = managerId
    if (debouncedQ) filter.q = debouncedQ

    mapRef.current = new Map()
    setSessions([])
    setStatus('connecting')

    const handle = openLiveStream(filter, {
      onEvent: (evt) => {
        // A snapshot fully replaces state (reconnect re-sync); deltas mutate it.
        mapRef.current = applyLiveEvent(mapRef.current, evt)
        setSessions([...mapRef.current.values()])
      },
      onConnect: () => setStatus('live'),
      onDisconnect: () => setStatus('reconnecting'),
    })
    return () => handle.close()
  }, [filterKey, nasId, profileId, managerId, debouncedQ])

  const nasName = useMemo(() => {
    const m = new Map(nasList.map((n) => [n.id, n.name]))
    return (id: string) => m.get(id) ?? id
  }, [nasList])

  const sorted = useMemo(
    () => [...sessions].sort((a, b) => a.username.localeCompare(b.username)),
    [sessions],
  )

  async function doDisconnect(s: LiveSession) {
    try {
      const res = await disconnectSession(s.nas_id, s.acct_session_id)
      if (res.outcome === 'ack') toast(t('live.disconnectAck'), 'ok')
      else toast(t('live.disconnectFail', { outcome: res.outcome }), 'danger')
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    }
  }

  const canDisconnect = can(PERM_DISCONNECT)

  return (
    <section>
      <PageHeader
        title={t('nav.sessions')}
        actions={<ConnectionBadge status={status} count={sorted.length} />}
      />

      {/* Filters */}
      <div className="mb-3 flex flex-wrap gap-2 rounded-md border border-surface-sunken bg-surface-raised p-3">
        <TextInput
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder={t('live.searchPlaceholder')}
          aria-label={t('live.search')}
          className="max-w-xs"
        />
        <Select value={nasId} onChange={(e) => setNasId(e.target.value)} className="max-w-[10rem]">
          <option value="">{t('live.allNas')}</option>
          {nasList.map((n) => (
            <option key={n.id} value={n.id}>
              {n.name}
            </option>
          ))}
        </Select>
        <Select
          value={profileId}
          onChange={(e) => setProfileId(e.target.value)}
          className="max-w-[10rem]"
        >
          <option value="">{t('live.allProfiles')}</option>
          {profiles.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
            </option>
          ))}
        </Select>
        {managers.length > 0 && (
          <Select
            value={managerId}
            onChange={(e) => setManagerId(e.target.value)}
            className="max-w-[10rem]"
          >
            <option value="">{t('live.allManagers')}</option>
            {managers.map((m) => (
              <option key={m.id} value={m.id}>
                {m.username}
              </option>
            ))}
          </Select>
        )}
      </div>

      {/* Header row */}
      <div className="overflow-x-auto rounded-md border border-surface-sunken">
        <div className="min-w-[900px]">
          <div className="grid grid-cols-[1.4fr_1.2fr_1.4fr_1.2fr_1fr_1.4fr_0.8fr_auto] gap-2 border-b border-surface-sunken bg-surface-raised px-3 py-2 text-xs uppercase tracking-wide text-ink-muted">
            <span>{t('live.col.user')}</span>
            <span>{t('live.col.ip')}</span>
            <span>{t('live.col.mac')}</span>
            <span>{t('live.col.nas')}</span>
            <span>{t('live.col.uptime')}</span>
            <span>{t('live.col.usage')}</span>
            <span>{t('live.col.service')}</span>
            <span className="text-end">{t('live.col.actions')}</span>
          </div>

          {sorted.length === 0 ? (
            <p className="p-10 text-center text-sm text-ink-muted">
              {status === 'live' ? t('live.emptyWaiting') : t('common.loading')}
            </p>
          ) : (
            <VirtualList
              items={sorted}
              rowHeight={ROW_HEIGHT}
              height={Math.min(sorted.length * ROW_HEIGHT, 640)}
              getKey={(s) => `${s.nas_id}:${s.acct_session_id}`}
              renderRow={(s) => (
                <Row
                  s={s}
                  nasName={nasName(s.nas_id)}
                  formatNumber={formatNumber}
                  canDisconnect={canDisconnect}
                  onDisconnect={() => setConfirm(s)}
                />
              )}
            />
          )}
        </div>
      </div>

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
    </section>
  )
}

function Row({
  s,
  nasName,
  formatNumber,
  canDisconnect,
  onDisconnect,
}: {
  s: LiveSession
  nasName: string
  formatNumber: (v: number, opts?: Intl.NumberFormatOptions) => string
  canDisconnect: boolean
  onDisconnect: () => void
}) {
  const t = useT()
  return (
    <div
      className={`grid h-[52px] grid-cols-[1.4fr_1.2fr_1.4fr_1.2fr_1fr_1.4fr_0.8fr_auto] items-center gap-2 border-b border-surface-sunken px-3 text-sm ${
        s.stale ? 'opacity-50' : ''
      }`}
      title={s.stale ? t('live.staleHint') : undefined}
    >
      <Link
        to={`/subscribers/${s.subscriber_id}`}
        className="truncate font-medium text-brand-strong"
      >
        <Ltr>{s.username}</Ltr>
      </Link>
      <span className="truncate">
        <Ltr>{s.ip || '—'}</Ltr>
      </span>
      <span className="truncate text-ink-muted">
        <Ltr>{s.mac || '—'}</Ltr>
      </span>
      <span className="truncate">{nasName}</span>
      <Uptime startedAt={s.started_at} />
      <span className="truncate text-xs">
        ↓ {formatBytes(s.bytes_in, formatNumber)} · ↑ {formatBytes(s.bytes_out, formatNumber)}
        <span className="block text-ink-muted">
          {formatBps(s.rate_down_bps, formatNumber)} / {formatBps(s.rate_up_bps, formatNumber)}
        </span>
      </span>
      <span>
        <span className="rounded bg-surface-sunken px-1.5 py-0.5 text-xs">
          {t(`live.service.${s.service}`)}
        </span>
      </span>
      <span className="flex justify-end gap-1">
        {canDisconnect && (
          <Button size="sm" variant="danger" onClick={onDisconnect}>
            {t('live.disconnect')}
          </Button>
        )}
      </span>
    </div>
  )
}

/**
 * Clock-skew-safe uptime: derived from the server-provided started_at against
 * the client clock, clamped so a small skew never renders a negative duration.
 * Recomputed on a coarse tick (no per-second re-render of 2k rows).
 */
function Uptime({ startedAt }: { startedAt: string }) {
  const t = useT()
  const [, force] = useState(0)
  useEffect(() => {
    const id = setInterval(() => force((n) => n + 1), 15000)
    return () => clearInterval(id)
  }, [])
  const started = new Date(startedAt).getTime()
  const secs = Math.max(0, Math.floor((Date.now() - started) / 1000))
  const h = Math.floor(secs / 3600)
  const m = Math.floor((secs % 3600) / 60)
  return (
    <span className="text-xs text-ink-muted" dir="ltr">
      {h > 0 ? t('live.uptimeHm', { h, m }) : t('live.uptimeM', { m })}
    </span>
  )
}

function ConnectionBadge({
  status,
  count,
}: {
  status: 'connecting' | 'live' | 'reconnecting'
  count: number
}) {
  const t = useT()
  const color =
    status === 'live'
      ? 'bg-ok text-ok'
      : status === 'reconnecting'
        ? 'bg-danger text-danger'
        : 'bg-ink-muted text-ink-muted'
  return (
    <span className="flex items-center gap-2 text-sm text-ink-muted">
      <span
        className={`inline-block h-2 w-2 rounded-full ${color.split(' ')[0]}`}
        aria-hidden="true"
      />
      {status === 'live'
        ? t('live.connLive', { count })
        : status === 'reconnecting'
          ? t('live.connReconnecting')
          : t('live.connConnecting')}
    </span>
  )
}
