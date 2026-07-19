import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'

import { ErrorState, IQDAmount, LoadingState, useFormatters, useT } from '@hikrad/shared'

import { getDashboard, type Dashboard } from '../api/monitoring'
import { getPreferences, putPreferences, type Preferences } from '../api/preferences'
import { useAuth } from '../auth/AuthContext'
import { Button } from '../components/Button'
import { Sparkline } from '../components/Sparkline'
import { DASHBOARD_WIDGET_CATALOG, widgetDef, type DashboardWidgetId } from '../dashboard/widgets'
import { resolveLayout, type EffectiveWidget } from '../dashboard/layout'
import { useAsync } from '../hooks/useAsync'
import { formatBytes } from '../lib/units'

const REFRESH_MS = 15000

/**
 * Omar's dashboard (FR-32), made customizable per manager (v2-10, FR-89/90):
 * a widget-registry renderer over the permission-gated catalog, with an edit
 * mode (add/remove/reorder/resize/reset) that round-trips through
 * GET/PUT /me/preferences. Phone-first single column always (FR-90.3) — the
 * grid only ever gains columns at sm/lg breakpoints.
 */
export function DashboardPage() {
  const t = useT()
  const { can } = useAuth()
  const prefsQ = useAsync(getPreferences, [])
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState<EffectiveWidget[] | null>(null)
  const [saving, setSaving] = useState(false)

  const savedLayout = useMemo(
    () => (prefsQ.data ? resolveLayout(prefsQ.data.dashboard_layout, can) : null),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [prefsQ.data],
  )
  const widgets = editing && draft ? draft : savedLayout
  const ids = useMemo(() => widgets?.map((w) => w.id) ?? [], [widgets])

  const idsKey = ids.join(',')
  const dataQ = useAsync<Dashboard>(
    () => (ids.length ? getDashboard(ids) : Promise.resolve({})),
    [idsKey],
  )

  // Auto-refresh on the C5 cadence; pause when the tab is hidden or the
  // manager is mid-edit (a refresh mid-drag would be surprising).
  useEffect(() => {
    const id = setInterval(() => {
      if (!document.hidden && !editing) dataQ.reload()
    }, REFRESH_MS)
    return () => clearInterval(id)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dataQ.reload, editing])

  if (prefsQ.error || dataQ.error) {
    return (
      <ErrorState
        onRetry={() => {
          prefsQ.reload()
          dataQ.reload()
        }}
      />
    )
  }
  if (prefsQ.loading || !widgets) return <LoadingState />

  function startEditing() {
    setDraft(widgets)
    setEditing(true)
  }
  function cancelEditing() {
    setDraft(null)
    setEditing(false)
  }
  async function persist(next: Preferences) {
    setSaving(true)
    try {
      await putPreferences(next)
      setDraft(null)
      setEditing(false)
      prefsQ.reload()
    } finally {
      setSaving(false)
    }
  }
  function saveLayout() {
    if (!draft) return
    void persist({
      ...(prefsQ.data ?? {}),
      dashboard_layout: { widgets: draft.map((w) => ({ id: w.id, size: w.size })) },
    })
  }
  function resetToDefault() {
    const next: Preferences = { ...(prefsQ.data ?? {}) }
    delete next.dashboard_layout
    void persist(next)
  }
  function removeWidget(id: DashboardWidgetId) {
    setDraft((d) => (d ? d.filter((w) => w.id !== id) : d))
  }
  function addWidget(id: DashboardWidgetId) {
    const def = widgetDef(id)
    if (!def) return
    setDraft((d) => (d ? [...d, { id, size: def.defaultSize }] : d))
  }
  function toggleSize(id: DashboardWidgetId) {
    setDraft((d) =>
      d ? d.map((w) => (w.id === id ? { ...w, size: w.size === '2x' ? '1x' : '2x' } : w)) : d,
    )
  }
  function moveWidget(index: number, dir: -1 | 1) {
    setDraft((d) => {
      if (!d) return d
      const target = index + dir
      if (target < 0 || target >= d.length) return d
      const next = [...d]
      ;[next[index], next[target]] = [next[target], next[index]]
      return next
    })
  }

  const availableToAdd = DASHBOARD_WIDGET_CATALOG.filter(
    (w) => (w.permission === '' || can(w.permission)) && !(draft ?? []).some((d) => d.id === w.id),
  )

  return (
    <section className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h1 className="text-xl font-semibold">{t('dashboard.title')}</h1>
        {!editing ? (
          <Button variant="secondary" size="sm" onClick={startEditing}>
            {t('dashboard.edit')}
          </Button>
        ) : (
          <div className="flex flex-wrap gap-2">
            <Button variant="ghost" size="sm" onClick={resetToDefault} disabled={saving}>
              {t('dashboard.resetDefault')}
            </Button>
            <Button variant="ghost" size="sm" onClick={cancelEditing} disabled={saving}>
              {t('ui.cancel')}
            </Button>
            <Button size="sm" onClick={saveLayout} disabled={saving}>
              {saving ? t('ui.working') : t('ui.save')}
            </Button>
          </div>
        )}
      </div>

      {editing && availableToAdd.length > 0 && (
        <div className="flex flex-wrap items-center gap-2 rounded-md border border-dashed border-surface-sunken p-3">
          <span className="text-xs text-ink-muted">{t('dashboard.addWidget')}</span>
          {availableToAdd.map((w) => (
            <button
              key={w.id}
              type="button"
              onClick={() => addWidget(w.id)}
              className="rounded-full bg-brand-soft px-2.5 py-1 text-xs text-brand-strong hover:opacity-80"
            >
              + {t(`dashboard.widget.${w.id}`)}
            </button>
          ))}
        </div>
      )}

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {(draft ?? widgets).map((w, i) => (
          <div key={w.id} className={w.size === '2x' ? 'sm:col-span-2' : undefined}>
            <WidgetFrame
              widget={w}
              editing={editing}
              canMoveLeft={i > 0}
              canMoveRight={!!draft && i < draft.length - 1}
              onRemove={() => removeWidget(w.id)}
              onToggleSize={() => toggleSize(w.id)}
              onMoveLeft={() => moveWidget(i, -1)}
              onMoveRight={() => moveWidget(i, 1)}
            >
              <WidgetBody id={w.id} data={dataQ.data} />
            </WidgetFrame>
          </div>
        ))}
      </div>
    </section>
  )
}

function WidgetFrame({
  widget,
  editing,
  canMoveLeft,
  canMoveRight,
  onRemove,
  onToggleSize,
  onMoveLeft,
  onMoveRight,
  children,
}: {
  widget: EffectiveWidget
  editing: boolean
  canMoveLeft: boolean
  canMoveRight: boolean
  onRemove: () => void
  onToggleSize: () => void
  onMoveLeft: () => void
  onMoveRight: () => void
  children: React.ReactNode
}) {
  const t = useT()
  return (
    <div className="h-full rounded-lg border border-surface-sunken bg-surface-raised p-4">
      <div className="mb-2 flex items-center justify-between gap-2">
        <p className="text-xs text-ink-muted">{t(`dashboard.widget.${widget.id}`)}</p>
        {editing && (
          <div className="flex items-center gap-1">
            <button
              type="button"
              onClick={onMoveLeft}
              disabled={!canMoveLeft}
              aria-label={t('dashboard.moveEarlier')}
              className="rounded px-1 text-xs text-ink-muted hover:bg-surface-sunken disabled:opacity-30"
            >
              ‹
            </button>
            <button
              type="button"
              onClick={onMoveRight}
              disabled={!canMoveRight}
              aria-label={t('dashboard.moveLater')}
              className="rounded px-1 text-xs text-ink-muted hover:bg-surface-sunken disabled:opacity-30"
            >
              ›
            </button>
            <button
              type="button"
              onClick={onToggleSize}
              aria-label={t('dashboard.toggleSize')}
              className="rounded px-1.5 text-xs text-ink-muted hover:bg-surface-sunken"
            >
              {widget.size === '2x' ? '1×' : '2×'}
            </button>
            <button
              type="button"
              onClick={onRemove}
              aria-label={t('dashboard.removeWidget')}
              className="rounded px-1.5 text-xs text-danger hover:bg-surface-sunken"
            >
              ×
            </button>
          </div>
        )}
      </div>
      {children}
    </div>
  )
}

function WidgetBody({ id, data }: { id: DashboardWidgetId; data: Dashboard | undefined }) {
  const t = useT()
  const { formatDate, formatNumber } = useFormatters()
  if (!data) return <SkeletonValue />

  switch (id) {
    case 'online-now':
      return (
        <div className="flex items-end justify-between gap-2">
          <span className="text-3xl font-bold">{data.online_now ?? 0}</span>
          <Sparkline
            values={(data.online_24h_sparkline ?? []).map((p) => p.online)}
            label={t('dashboard.onlineTrend')}
          />
        </div>
      )
    case 'revenue-today':
      return (
        <Link to="/ledger" className="block text-3xl font-bold hover:opacity-90">
          <IQDAmount amount={data.revenue_today_iqd ?? 0} />
        </Link>
      )
    case 'radius-rps':
      return (
        <>
          <span className="text-3xl font-bold">{data.radius_rps ?? 0}</span>
          <span className="ms-1 text-sm text-ink-muted">{t('dashboard.rps')}</span>
        </>
      )
    case 'subs-active':
      return (
        <Link to="/subscribers?status=active" className="block hover:opacity-90">
          <span className="text-3xl font-bold text-ok">{data.subs?.active ?? 0}</span>
        </Link>
      )
    case 'subs-expired':
      return (
        <Link to="/subscribers?status=expired" className="block hover:opacity-90">
          <span className="text-3xl font-bold text-danger">{data.subs?.expired ?? 0}</span>
        </Link>
      )
    case 'subs-expiring':
      return (
        <Link to="/subscribers?expiring=7d" className="block hover:opacity-90">
          <span className="text-3xl font-bold text-warn">{data.subs?.expiring_7d ?? 0}</span>
        </Link>
      )
    case 'pipeline-health': {
      const p = data.pipeline
      return (
        <div
          className={`rounded-md border p-3 text-sm ${
            p?.invariant_ok
              ? 'border-ok/40 bg-ok/5 text-ok'
              : 'border-danger/40 bg-danger/5 text-danger'
          }`}
        >
          {p?.invariant_ok ? t('dashboard.pipelineOk') : t('dashboard.pipelineBroken')}
          <span className="ms-2 text-ink-muted">
            {t('dashboard.queueDepth', { n: p?.depth ?? 0 })}
          </span>
        </div>
      )
    }
    case 'nas-health':
      return (data.nas_cards ?? []).length === 0 ? (
        <p className="text-sm text-ink-muted">{t('dashboard.noNas')}</p>
      ) : (
        <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
          {(data.nas_cards ?? []).map((n) => (
            <Link
              key={n.id}
              to={`/nas/${n.id}/status`}
              className="flex items-center justify-between rounded-md border border-surface-sunken bg-surface p-3 text-sm hover:border-brand"
            >
              <span className="font-medium">{n.name}</span>
              <NasStatusDot status={n.status} latency={n.latency_ms} />
            </Link>
          ))}
        </div>
      )
    case 'my-balance':
      return (data.my_balance ?? []).length === 0 ? (
        <span className="text-sm text-ink-muted">{t('dashboard.noBalance')}</span>
      ) : (
        <div className="flex flex-wrap gap-2">
          {(data.my_balance ?? []).map((b) => (
            <div key={b.currency} className="rounded-md bg-surface-sunken px-3 py-1.5 text-sm">
              <span className="text-ink-muted">{b.currency}</span>{' '}
              <IQDAmount amount={b.balance} currency={b.currency} />
            </div>
          ))}
        </div>
      )
    case 'pending-payment-tickets':
      return (
        <Link to="/payment-tickets" className="block hover:opacity-90">
          <span className="text-3xl font-bold">{data.pending_payment_tickets ?? 0}</span>
        </Link>
      )
    case 'alerts-feed':
      return (data.alerts_feed ?? []).length === 0 ? (
        <p className="text-sm text-ink-muted">{t('dashboard.noAlerts')}</p>
      ) : (
        <ul className="space-y-1.5">
          {(data.alerts_feed ?? []).map((a) => (
            <li key={a.id} className="flex items-center justify-between gap-2 text-sm">
              <span className="truncate">{a.summary}</span>
              <span className="shrink-0 text-xs text-ink-muted">{formatDate(a.at)}</span>
            </li>
          ))}
        </ul>
      )
    case 'top-usage-subscribers':
      return (data.top_usage_subscribers ?? []).length === 0 ? (
        <p className="text-sm text-ink-muted">{t('dashboard.noUsage')}</p>
      ) : (
        <ul className="space-y-1.5">
          {(data.top_usage_subscribers ?? []).map((u) => (
            <li
              key={`${u.subscriber_id}:${u.service}`}
              className="flex items-center justify-between gap-2 text-sm"
            >
              <Link to={`/subscribers/${u.subscriber_id}`} className="truncate hover:underline">
                <bdi dir="ltr">{u.username || u.subscriber_id}</bdi>
              </Link>
              <span className="flex shrink-0 items-center gap-2">
                <span className="rounded bg-surface-sunken px-1.5 py-0.5 text-xs text-ink-muted">
                  {t(`dashboard.service.${u.service}`)}
                </span>
                <span dir="ltr">{formatBytes(u.bytes, formatNumber)}</span>
              </span>
            </li>
          ))}
        </ul>
      )
    case 'top-session-subscribers':
      return (data.top_session_subscribers ?? []).length === 0 ? (
        <p className="text-sm text-ink-muted">{t('dashboard.noOpenSessions')}</p>
      ) : (
        <ul className="space-y-1.5">
          {(data.top_session_subscribers ?? []).map((u) => (
            <li key={u.subscriber_id} className="flex items-center justify-between gap-2 text-sm">
              <Link to={`/subscribers/${u.subscriber_id}`} className="truncate hover:underline">
                <bdi dir="ltr">{u.username || u.subscriber_id}</bdi>
              </Link>
              <span className="shrink-0 text-ink-muted">
                {t('dashboard.sessionCount', { n: u.open_sessions })}
              </span>
            </li>
          ))}
        </ul>
      )
    default:
      return null
  }
}

function SkeletonValue() {
  return <div className="h-8 w-16 animate-pulse rounded bg-surface-sunken" />
}

function NasStatusDot({ status, latency }: { status: string; latency: number | null }) {
  const t = useT()
  const { formatNumber } = useFormatters()
  const color =
    status === 'up'
      ? 'bg-ok text-ok'
      : status === 'down'
        ? 'bg-danger text-danger'
        : 'bg-warn text-warn'
  return (
    <span className="flex items-center gap-1.5">
      <span
        className={`inline-block h-2 w-2 rounded-full ${color.split(' ')[0]}`}
        aria-hidden="true"
      />
      <span className={color.split(' ')[1]}>{t(`monitoring.status.${status}`)}</span>
      {latency != null ? (
        <span className="text-ink-muted">{t('monitoring.ms', { n: formatNumber(latency) })}</span>
      ) : null}
    </span>
  )
}
