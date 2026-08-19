import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'

import {
  EmptyState,
  ErrorState,
  LoadingState,
  StatCard,
  useFormatters,
  useT,
  type TFunction,
} from '@hikrad/shared'

import {
  broadcast,
  cancelOutbox,
  getOverview,
  getTemplateHistory,
  listOutbox,
  listRules,
  listTemplates,
  putRule,
  putTemplate,
  retryOutbox,
  type NotifyChannel,
  type NotificationRule,
  type NotificationTemplate,
  type OutboxItem,
} from '../../api/notifications'
import { Button } from '../../components/Button'
import { Checkbox, Field, Select, Switch, TextInput, Textarea } from '../../components/form'
import { Modal } from '../../components/Modal'
import { PageHeader } from '../../components/PageHeader'
import { Pill } from '../../components/Pill'
import { useToast } from '../../components/Toast'
import { useAsync } from '../../hooks/useAsync'
import { usePaginated } from '../../hooks/usePaginated'

type Tab = 'overview' | 'channels' | 'messages' | 'rules' | 'broadcast' | 'outbox'
const CHANNELS: NotifyChannel[] = ['inapp', 'telegram', 'email', 'whatsapp', 'sms']
const LOCALES = ['en', 'ar', 'ku'] as const

/** FR-160/FR-163: the Notification Center's six tabs, all backed by the
 * one throttled queue (internal/notify) — broadcast, transactional and
 * staff-alert messages all appear in the same Outbox tab. */
export function NotificationCenterPage() {
  const t = useT()
  const [tab, setTab] = useState<Tab>('overview')
  const tabs: { key: Tab; label: string }[] = [
    { key: 'overview', label: t('notifications.tab.overview') },
    { key: 'channels', label: t('notifications.tab.channels') },
    { key: 'messages', label: t('notifications.tab.messages') },
    { key: 'rules', label: t('notifications.tab.rules') },
    { key: 'broadcast', label: t('notifications.tab.broadcast') },
    { key: 'outbox', label: t('notifications.tab.outbox') },
  ]

  return (
    <section>
      <PageHeader title={t('notifications.title')} />
      <div className="mb-4 flex flex-wrap gap-1 border-b border-border">
        {tabs.map((tb) => (
          <button
            key={tb.key}
            type="button"
            onClick={() => setTab(tb.key)}
            className={`rounded-t-md px-3 py-2 text-sm font-medium ${
              tab === tb.key
                ? 'border-b-2 border-brand text-brand-strong'
                : 'text-ink-muted hover:text-ink'
            }`}
          >
            {tb.label}
          </button>
        ))}
      </div>
      {tab === 'overview' ? <OverviewTab /> : null}
      {tab === 'channels' ? <ChannelsTab /> : null}
      {tab === 'messages' ? <MessagesTab /> : null}
      {tab === 'rules' ? <RulesTab /> : null}
      {tab === 'broadcast' ? <BroadcastTab /> : null}
      {tab === 'outbox' ? <OutboxTab /> : null}
    </section>
  )
}

function OverviewTab() {
  const t = useT()
  const q = useAsync(() => getOverview(), [])
  if (q.error) return <ErrorState onRetry={q.reload} />
  if (q.loading || !q.data) return <LoadingState />
  const { counters, channels } = q.data
  return (
    <div>
      <div className="mb-6 grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
        <StatCard label={t('notifications.overview.queued')} value={counters.queued} />
        <StatCard label={t('notifications.overview.sending')} value={counters.sending} />
        <StatCard label={t('notifications.overview.sentToday')} value={counters.sent_today} />
        <StatCard label={t('notifications.overview.failed')} value={counters.failed} />
        <StatCard label={t('notifications.overview.skipped')} value={counters.skipped} />
        <StatCard label={t('notifications.overview.total')} value={counters.total} />
      </div>
      <p className="mb-2 text-sm font-semibold">{t('notifications.overview.channelState')}</p>
      <div className="flex flex-wrap gap-2">
        {channels.map((c) => (
          <Pill key={c.channel} tone={c.configured ? 'ok' : 'muted'}>
            {t(`notifications.channel.${c.channel}`)} —{' '}
            {c.configured
              ? t('notifications.channel.configured')
              : t('notifications.channel.notConfigured')}
          </Pill>
        ))}
      </div>
    </div>
  )
}

function ChannelsTab() {
  const t = useT()
  const q = useAsync(() => getOverview(), [])
  if (q.error) return <ErrorState onRetry={q.reload} />
  if (q.loading || !q.data) return <LoadingState />
  return (
    <div>
      <p className="mb-4 text-sm text-ink-muted">
        {t('notifications.channels.hint')}{' '}
        <Link to="/settings/notifications" className="text-brand hover:underline">
          {t('notifications.channels.settingsLink')}
        </Link>
      </p>
      <div className="overflow-x-auto rounded-md border border-border">
        <table className="w-full max-w-xl text-sm">
          <thead className="bg-surface-sunken/40 text-start text-xs text-ink-muted">
            <tr>
              <th className="px-3 py-2 text-start">{t('notifications.channels.col.channel')}</th>
              <th className="px-3 py-2 text-start">{t('notifications.channels.col.status')}</th>
            </tr>
          </thead>
          <tbody>
            {q.data.channels.map((c) => (
              <tr key={c.channel} className="border-t border-border/60">
                <td className="px-3 py-2">{t(`notifications.channel.${c.channel}`)}</td>
                <td className="px-3 py-2">
                  <Pill tone={c.configured ? 'ok' : 'muted'}>
                    {c.configured
                      ? t('notifications.channel.configured')
                      : t('notifications.channel.notConfigured')}
                  </Pill>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

const NOTIFY_EVENT_KEYS = new Set([
  'renewal_reminder',
  'expiry_notice',
  'quota_warning',
  'payment_receipt',
  'plan_renewed',
  'payment_ticket_state',
  'staff_alert',
  'broadcast',
])

/** Outbox rows sourced from a staff alert carry an alert_rules `type`
 * (nas_down, disk_low, …), not one of this phase's own event keys — those
 * already have labels under alerts.type.* (the Alerts screen), reused here
 * rather than duplicated. */
function eventLabel(event: string, t: TFunction): string {
  return t(NOTIFY_EVENT_KEYS.has(event) ? `notifications.event.${event}` : `alerts.type.${event}`)
}

function MessagesTab() {
  const t = useT()
  const q = useAsync(() => listTemplates(), [])
  const [editing, setEditing] = useState<{ event: string; locale: string } | null>(null)

  if (q.error) return <ErrorState onRetry={q.reload} />
  if (q.loading || !q.data) return <LoadingState />

  const byEvent = new Map<string, NotificationTemplate[]>()
  for (const tpl of q.data.items) {
    const arr = byEvent.get(tpl.EventKey) ?? []
    arr.push(tpl)
    byEvent.set(tpl.EventKey, arr)
  }

  return (
    <div>
      <div className="overflow-x-auto rounded-md border border-border">
        <table className="w-full min-w-[36rem] text-sm">
          <thead className="bg-surface-sunken/40 text-start text-xs text-ink-muted">
            <tr>
              <th className="px-3 py-2 text-start">{t('notifications.messages.col.event')}</th>
              {LOCALES.map((l) => (
                <th key={l} className="px-3 py-2 text-start">
                  {t(`languages.${l}`)}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {[...byEvent.entries()].map(([event, tpls]) => (
              <tr key={event} className="border-t border-border/60">
                <td className="px-3 py-2 font-medium">{eventLabel(event, t)}</td>
                {LOCALES.map((l) => {
                  const tpl = tpls.find((x) => x.Locale === l)
                  return (
                    <td key={l} className="px-3 py-2">
                      <button
                        type="button"
                        className="text-brand hover:underline"
                        onClick={() => setEditing({ event, locale: l })}
                      >
                        {tpl
                          ? t('notifications.messages.editVersion', { version: tpl.Version })
                          : t('notifications.messages.addTranslation')}
                      </button>
                    </td>
                  )
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <TemplateEditModal
        target={editing}
        onClose={() => setEditing(null)}
        onDone={() => {
          setEditing(null)
          q.reload()
        }}
      />
    </div>
  )
}

function TemplateEditModal({
  target,
  onClose,
  onDone,
}: {
  target: { event: string; locale: string } | null
  onClose: () => void
  onDone: () => void
}) {
  const t = useT()
  const { toast } = useToast()
  const { data, loading } = useAsync(
    () => (target ? getTemplateHistory(target.event, target.locale) : Promise.resolve(null)),
    [target],
  )
  const [subject, setSubject] = useState('')
  const [body, setBody] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    const current = data?.items[0]
    setSubject(current?.Subject ?? '')
    setBody(current?.Body ?? '')
  }, [data])

  async function save() {
    if (!target) return
    setBusy(true)
    try {
      await putTemplate(target.event, target.locale, { subject, body })
      toast(t('notifications.messages.saved'), 'ok')
      onDone()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open={target !== null}
      onOpenChange={(o) => !o && onClose()}
      title={target ? `${eventLabel(target.event, t)} — ${t(`languages.${target.locale}`)}` : ''}
      size="lg"
    >
      {loading ? (
        <LoadingState />
      ) : (
        <div className="space-y-4">
          <Field label={t('notifications.messages.subject')} htmlFor="tpl-subject">
            <TextInput
              id="tpl-subject"
              value={subject}
              onChange={(e) => setSubject(e.target.value)}
            />
          </Field>
          <Field
            label={t('notifications.messages.body')}
            htmlFor="tpl-body"
            hint={t('notifications.messages.placeholderHint')}
          >
            <Textarea
              id="tpl-body"
              rows={5}
              value={body}
              onChange={(e) => setBody(e.target.value)}
            />
          </Field>
          <div className="flex justify-end gap-2 border-t border-border pt-4">
            <Button variant="ghost" disabled={busy} onClick={onClose}>
              {t('ui.cancel')}
            </Button>
            <Button disabled={busy || !body.trim()} onClick={() => void save()}>
              {busy ? t('ui.working') : t('ui.save')}
            </Button>
          </div>
        </div>
      )}
    </Modal>
  )
}

function RulesTab() {
  const t = useT()
  const q = useAsync(() => listRules(), [])
  const [editing, setEditing] = useState<NotificationRule | null>(null)

  if (q.error) return <ErrorState onRetry={q.reload} />
  if (q.loading || !q.data) return <LoadingState />

  return (
    <div>
      <div className="overflow-x-auto rounded-md border border-border">
        <table className="w-full min-w-[48rem] text-sm">
          <thead className="bg-surface-sunken/40 text-start text-xs text-ink-muted">
            <tr>
              <th className="px-3 py-2 text-start">{t('notifications.rules.col.event')}</th>
              <th className="px-3 py-2 text-start">{t('notifications.rules.col.enabled')}</th>
              <th className="px-3 py-2 text-start">{t('notifications.rules.col.channels')}</th>
              <th className="px-3 py-2 text-start">{t('notifications.rules.col.daysBefore')}</th>
              <th className="px-3 py-2 text-start">{t('notifications.rules.col.ignoreOptOut')}</th>
              <th className="px-3 py-2" />
            </tr>
          </thead>
          <tbody>
            {q.data.items.map((r) => (
              <tr key={r.EventKey} className="border-t border-border/60">
                <td className="px-3 py-2 font-medium">{eventLabel(r.EventKey, t)}</td>
                <td className="px-3 py-2">
                  <Pill tone={r.Enabled ? 'ok' : 'muted'}>
                    {r.Enabled ? t('ui.enabled') : t('ui.disabled')}
                  </Pill>
                </td>
                <td className="px-3 py-2 text-ink-muted">
                  {r.Channels.map((c) => t(`notifications.channel.${c}`)).join(', ')}
                </td>
                <td className="px-3 py-2">{r.DaysBefore ?? '—'}</td>
                <td className="px-3 py-2">
                  {r.IgnoreOptOut ? (
                    <Pill tone="warn">{t('notifications.rules.ignoreOptOutOn')}</Pill>
                  ) : (
                    <span className="text-ink-muted">
                      {t('notifications.rules.optOutHonoured')}
                    </span>
                  )}
                </td>
                <td className="px-3 py-2 text-end">
                  <Button size="sm" variant="secondary" onClick={() => setEditing(r)}>
                    {t('ui.edit')}
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <RuleEditModal
        rule={editing}
        onClose={() => setEditing(null)}
        onDone={() => {
          setEditing(null)
          q.reload()
        }}
      />
    </div>
  )
}

function RuleEditModal({
  rule,
  onClose,
  onDone,
}: {
  rule: NotificationRule | null
  onClose: () => void
  onDone: () => void
}) {
  const t = useT()
  const { toast } = useToast()
  const [enabled, setEnabled] = useState(true)
  const [channels, setChannels] = useState<NotifyChannel[]>([])
  const [daysBefore, setDaysBefore] = useState('')
  const [ignoreOptOut, setIgnoreOptOut] = useState(false)
  const [batchSize, setBatchSize] = useState(50)
  const [tickInterval, setTickInterval] = useState(5)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!rule) return
    setEnabled(rule.Enabled)
    setChannels(rule.Channels)
    setDaysBefore(rule.DaysBefore === null ? '' : String(rule.DaysBefore))
    setIgnoreOptOut(rule.IgnoreOptOut)
    setBatchSize(rule.BatchSize)
    setTickInterval(rule.TickIntervalSeconds)
  }, [rule])

  function toggleChannel(c: NotifyChannel) {
    setChannels((prev) => (prev.includes(c) ? prev.filter((x) => x !== c) : [...prev, c]))
  }

  async function save() {
    if (!rule) return
    setBusy(true)
    try {
      await putRule(rule.EventKey, {
        enabled,
        channels,
        days_before: daysBefore === '' ? null : Number(daysBefore),
        ignore_opt_out: ignoreOptOut,
        batch_size: batchSize,
        tick_interval_seconds: tickInterval,
      })
      toast(t('notifications.rules.saved'), 'ok')
      onDone()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open={rule !== null}
      onOpenChange={(o) => !o && onClose()}
      title={rule ? eventLabel(rule.EventKey, t) : ''}
    >
      <div className="space-y-4">
        <Switch
          label={t('notifications.rules.col.enabled')}
          checked={enabled}
          onChange={(e) => setEnabled(e.target.checked)}
        />

        <div>
          <p className="mb-2 text-sm font-medium">{t('notifications.rules.col.channels')}</p>
          <div className="flex flex-wrap gap-3">
            {CHANNELS.map((c) => (
              <Checkbox
                key={c}
                label={t(`notifications.channel.${c}`)}
                checked={channels.includes(c)}
                onChange={() => toggleChannel(c)}
              />
            ))}
          </div>
        </div>

        <Field
          label={t('notifications.rules.col.daysBefore')}
          htmlFor="rule-days-before"
          hint={t('notifications.rules.daysBeforeHint')}
        >
          <TextInput
            id="rule-days-before"
            type="number"
            dir="ltr"
            value={daysBefore}
            onChange={(e) => setDaysBefore(e.target.value)}
          />
        </Field>

        {/* Frozen contract: this flag must always be a visible, explicit
            toggle in the rule UI — never a hidden default. */}
        <div className="rounded-md border border-warn/40 bg-warn/10 p-3">
          <Switch
            label={t('notifications.rules.ignoreOptOut')}
            description={t('notifications.rules.ignoreOptOutDescription')}
            checked={ignoreOptOut}
            onChange={(e) => setIgnoreOptOut(e.target.checked)}
          />
        </div>

        <div className="grid grid-cols-2 gap-3">
          <Field label={t('notifications.rules.batchSize')} htmlFor="rule-batch-size">
            <TextInput
              id="rule-batch-size"
              type="number"
              dir="ltr"
              value={batchSize}
              onChange={(e) => setBatchSize(Number(e.target.value))}
            />
          </Field>
          <Field label={t('notifications.rules.tickInterval')} htmlFor="rule-tick-interval">
            <TextInput
              id="rule-tick-interval"
              type="number"
              dir="ltr"
              value={tickInterval}
              onChange={(e) => setTickInterval(Number(e.target.value))}
            />
          </Field>
        </div>

        <div className="flex justify-end gap-2 border-t border-border pt-4">
          <Button variant="ghost" disabled={busy} onClick={onClose}>
            {t('ui.cancel')}
          </Button>
          <Button disabled={busy} onClick={() => void save()}>
            {busy ? t('ui.working') : t('ui.save')}
          </Button>
        </div>
      </div>
    </Modal>
  )
}

function BroadcastTab() {
  const t = useT()
  const { toast } = useToast()
  const [subscriberIds, setSubscriberIds] = useState('')
  const [channel, setChannel] = useState<NotifyChannel>('sms')
  const [subject, setSubject] = useState('')
  const [body, setBody] = useState('')
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<number | null>(null)

  async function send() {
    const ids = subscriberIds
      .split(/[\s,]+/)
      .map((s) => s.trim())
      .filter(Boolean)
    if (ids.length === 0 || !body.trim()) return
    setBusy(true)
    setResult(null)
    try {
      const res = await broadcast({ subscriber_ids: ids, channel, subject, body: body.trim() })
      setResult(res.queued)
      toast(t('notifications.broadcast.done', { count: res.queued }), 'ok')
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="max-w-xl space-y-4">
      <p className="text-sm text-ink-muted">{t('notifications.broadcast.hint')}</p>
      <Field
        label={t('notifications.broadcast.subscriberIds')}
        htmlFor="broadcast-subscribers"
        hint={t('notifications.broadcast.subscriberIdsHint')}
      >
        <Textarea
          id="broadcast-subscribers"
          dir="ltr"
          rows={3}
          value={subscriberIds}
          onChange={(e) => setSubscriberIds(e.target.value)}
        />
      </Field>
      <Field label={t('notifications.broadcast.channel')} htmlFor="broadcast-channel">
        <Select
          id="broadcast-channel"
          value={channel}
          onChange={(e) => setChannel(e.target.value as NotifyChannel)}
        >
          {CHANNELS.filter((c) => c !== 'inapp').map((c) => (
            <option key={c} value={c}>
              {t(`notifications.channel.${c}`)}
            </option>
          ))}
        </Select>
      </Field>
      <Field label={t('notifications.broadcast.subject')} htmlFor="broadcast-subject">
        <TextInput
          id="broadcast-subject"
          value={subject}
          onChange={(e) => setSubject(e.target.value)}
        />
      </Field>
      <Field label={t('notifications.broadcast.body')} htmlFor="broadcast-body">
        <Textarea
          id="broadcast-body"
          rows={4}
          value={body}
          onChange={(e) => setBody(e.target.value)}
        />
      </Field>
      <Button disabled={busy || !subscriberIds.trim() || !body.trim()} onClick={() => void send()}>
        {busy ? t('ui.working') : t('notifications.broadcast.send')}
      </Button>
      {result !== null ? (
        <p className="text-sm text-ink-muted">
          {t('notifications.broadcast.done', { count: result })}
        </p>
      ) : null}
    </div>
  )
}

function outboxStateTone(s: OutboxItem['state']): 'ok' | 'danger' | 'warn' | 'muted' {
  if (s === 'sent') return 'ok'
  if (s === 'failed') return 'danger'
  if (s === 'skipped') return 'muted'
  return 'warn'
}

function OutboxTab() {
  const t = useT()
  const { formatDate } = useFormatters()
  const { toast } = useToast()
  const [state, setState] = useState('')
  const [channel, setChannel] = useState('')
  const [search, setSearch] = useState('')
  const page = usePaginated(
    (cursor) =>
      listOutbox({
        cursor,
        state: state || undefined,
        channel: channel || undefined,
        search: search || undefined,
      }),
    `${state}|${channel}|${search}`,
  )
  const [inspecting, setInspecting] = useState<OutboxItem | null>(null)

  async function doRetry(id: string) {
    try {
      await retryOutbox(id)
      toast(t('notifications.outbox.retried'), 'ok')
      page.reset()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    }
  }

  async function doCancel(id: string) {
    try {
      await cancelOutbox(id)
      toast(t('notifications.outbox.cancelled'), 'ok')
      page.reset()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    }
  }

  return (
    <div>
      <div className="mb-4 flex flex-wrap items-end gap-3">
        <label className="block text-xs">
          <span className="mb-1 block text-ink-muted">
            {t('notifications.outbox.filter.state')}
          </span>
          <Select value={state} onChange={(e) => setState(e.target.value)}>
            <option value="">{t('support.filter.all')}</option>
            {(['queued', 'sending', 'sent', 'failed', 'skipped'] as const).map((s) => (
              <option key={s} value={s}>
                {t(`notifications.outbox.state.${s}`)}
              </option>
            ))}
          </Select>
        </label>
        <label className="block text-xs">
          <span className="mb-1 block text-ink-muted">
            {t('notifications.outbox.filter.channel')}
          </span>
          <Select value={channel} onChange={(e) => setChannel(e.target.value)}>
            <option value="">{t('support.filter.all')}</option>
            {CHANNELS.map((c) => (
              <option key={c} value={c}>
                {t(`notifications.channel.${c}`)}
              </option>
            ))}
          </Select>
        </label>
        <label className="block flex-1 text-xs">
          <span className="mb-1 block text-ink-muted">{t('support.filter.search')}</span>
          <TextInput value={search} onChange={(e) => setSearch(e.target.value)} />
        </label>
      </div>

      {page.error ? (
        <ErrorState onRetry={page.reset} />
      ) : page.items.length === 0 && !page.loading ? (
        <EmptyState
          title={t('notifications.outbox.empty.title')}
          body={t('notifications.outbox.empty.body')}
        />
      ) : (
        <div className="overflow-x-auto rounded-md border border-border">
          <table className="w-full min-w-[54rem] text-sm">
            <thead className="bg-surface-sunken/40 text-start text-xs text-ink-muted">
              <tr>
                <th className="px-3 py-2 text-start">{t('notifications.outbox.col.event')}</th>
                <th className="px-3 py-2 text-start">{t('notifications.outbox.col.channel')}</th>
                <th className="px-3 py-2 text-start">{t('notifications.outbox.col.address')}</th>
                <th className="px-3 py-2 text-start">{t('notifications.outbox.col.state')}</th>
                <th className="px-3 py-2 text-start">{t('notifications.outbox.col.reason')}</th>
                <th className="px-3 py-2 text-start">{t('notifications.outbox.col.created')}</th>
                <th className="px-3 py-2" />
              </tr>
            </thead>
            <tbody>
              {page.items.map((row) => (
                <tr key={row.id} className="border-t border-border/60">
                  <td className="px-3 py-2">{eventLabel(row.event, t)}</td>
                  <td className="px-3 py-2">{t(`notifications.channel.${row.channel}`)}</td>
                  <td className="px-3 py-2 text-ink-muted" dir="ltr">
                    {row.address || '—'}
                  </td>
                  <td className="px-3 py-2">
                    <Pill tone={outboxStateTone(row.state)}>
                      {t(`notifications.outbox.state.${row.state}`)}
                    </Pill>
                  </td>
                  <td
                    className="max-w-[16rem] truncate px-3 py-2 text-ink-muted"
                    title={row.last_error || row.skip_reason || ''}
                  >
                    {row.last_error || row.skip_reason || '—'}
                  </td>
                  <td className="px-3 py-2 whitespace-nowrap text-ink-muted">
                    {formatDate(row.created_at)}
                  </td>
                  <td className="px-3 py-2 text-end">
                    <div className="flex justify-end gap-1">
                      <Button size="sm" variant="ghost" onClick={() => setInspecting(row)}>
                        {t('notifications.outbox.inspect')}
                      </Button>
                      {row.state === 'failed' ? (
                        <Button size="sm" variant="secondary" onClick={() => void doRetry(row.id)}>
                          {t('notifications.outbox.retry')}
                        </Button>
                      ) : null}
                      {row.state === 'queued' ? (
                        <Button size="sm" variant="ghost" onClick={() => void doCancel(row.id)}>
                          {t('notifications.outbox.cancel')}
                        </Button>
                      ) : null}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {page.loading ? <LoadingState /> : null}
          {page.hasMore && !page.loading ? (
            <div className="p-3 text-center">
              <Button variant="secondary" onClick={page.loadMore}>
                {t('ui.loadMore')}
              </Button>
            </div>
          ) : null}
        </div>
      )}

      <Modal
        open={inspecting !== null}
        onOpenChange={(o) => !o && setInspecting(null)}
        title={t('notifications.outbox.inspect')}
      >
        {inspecting ? (
          <div className="space-y-2 text-sm">
            <p>
              <span className="text-ink-muted">{t('notifications.outbox.col.event')}: </span>
              {eventLabel(inspecting.event, t)}
            </p>
            <p>
              <span className="text-ink-muted">{t('notifications.outbox.col.channel')}: </span>
              {t(`notifications.channel.${inspecting.channel}`)}
            </p>
            <p dir="ltr">
              <span className="text-ink-muted">{t('notifications.outbox.col.address')}: </span>
              {inspecting.address || '—'}
            </p>
            <p>
              <span className="text-ink-muted">{t('notifications.outbox.col.state')}: </span>
              <Pill tone={outboxStateTone(inspecting.state)}>
                {t(`notifications.outbox.state.${inspecting.state}`)}
              </Pill>
            </p>
            <p>
              <span className="text-ink-muted">{t('notifications.outbox.attempts')}: </span>
              {inspecting.attempt_count}
            </p>
            {inspecting.template_version ? (
              <p>
                <span className="text-ink-muted">
                  {t('notifications.outbox.templateVersion')}:{' '}
                </span>
                {inspecting.template_version}
              </p>
            ) : null}
            {inspecting.last_error || inspecting.skip_reason ? (
              <p className="rounded-md bg-danger/10 p-2 text-danger">
                {inspecting.last_error || inspecting.skip_reason}
              </p>
            ) : null}
            <div className="rounded-md border border-border p-3 whitespace-pre-wrap">
              {inspecting.body}
            </div>
          </div>
        ) : null}
      </Modal>
    </div>
  )
}
