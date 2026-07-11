import { useState } from 'react'

import { ErrorState, LoadingState, useFormatters, useT } from '@hikrad/shared'

import {
  createAlertRule,
  listAlertEvents,
  listAlertRules,
  updateAlertRule,
  type AlertChannel,
  type AlertEvent,
  type AlertRule,
  type AlertRuleType,
  type AlertRuleWrite,
} from '../../api/monitoring'
import { ApiError } from '../../api/client'
import { useAuth } from '../../auth/AuthContext'
import { PERM_MONITORING_EDIT } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { Checkbox, Field, Select, TextInput } from '../../components/form'
import { Modal } from '../../components/Modal'
import { PageHeader } from '../../components/PageHeader'
import { useToast } from '../../components/Toast'
import { useAsync } from '../../hooks/useAsync'

const RULE_TYPES: AlertRuleType[] = [
  'nas_down',
  'nas_up',
  'device_down',
  'device_up',
  'radius_reject_spike',
  'acct_backlog',
  'disk_low',
  'expiring_digest',
  'agent_balance_low',
]
const CHANNELS: AlertChannel[] = ['inapp', 'telegram', 'email', 'whatsapp']
// Types that carry a numeric threshold value.
const THRESHOLD_TYPES = new Set<AlertRuleType>([
  'radius_reject_spike',
  'acct_backlog',
  'disk_low',
  'agent_balance_low',
])

/** Alerts (FR-36): rules CRUD + events with per-channel delivery results. */
export function AlertsPage() {
  const t = useT()
  const { can } = useAuth()
  const rules = useAsync(() => listAlertRules(), [])
  const [tab, setTab] = useState<'rules' | 'events'>('rules')
  const [editing, setEditing] = useState<AlertRule | null | 'new'>(null)
  const canEdit = can(PERM_MONITORING_EDIT)

  return (
    <section>
      <PageHeader
        title={t('alerts.title')}
        actions={
          canEdit && tab === 'rules' ? (
            <Button size="sm" onClick={() => setEditing('new')}>
              {t('alerts.create')}
            </Button>
          ) : null
        }
      />

      <div className="mb-4 flex gap-1 border-b border-surface-sunken">
        <TabBtn active={tab === 'rules'} onClick={() => setTab('rules')}>
          {t('alerts.tabRules')}
        </TabBtn>
        <TabBtn active={tab === 'events'} onClick={() => setTab('events')}>
          {t('alerts.tabEvents')}
        </TabBtn>
      </div>

      {tab === 'rules' ? (
        rules.error ? (
          <ErrorState onRetry={rules.reload} />
        ) : rules.loading || !rules.data ? (
          <LoadingState />
        ) : rules.data.items.length === 0 ? (
          <p className="rounded-md border border-dashed border-surface-sunken p-8 text-center text-sm text-ink-muted">
            {t('alerts.empty')}
          </p>
        ) : (
          <ul className="space-y-2">
            {rules.data.items.map((r) => (
              <li
                key={r.id}
                className="flex items-center justify-between rounded-md border border-surface-sunken bg-surface-raised p-3 text-sm"
              >
                <div>
                  <p className="font-medium">{r.name}</p>
                  <p className="text-xs text-ink-muted">
                    {t(`alerts.type.${r.type}`)} ·{' '}
                    {r.channels.map((c) => t(`alerts.channel.${c}`)).join(', ')}
                  </p>
                </div>
                <div className="flex items-center gap-2">
                  {!r.enabled ? (
                    <span className="rounded bg-surface-sunken px-1.5 py-0.5 text-xs text-ink-muted">
                      {t('alerts.disabled')}
                    </span>
                  ) : null}
                  {canEdit ? (
                    <Button size="sm" variant="ghost" onClick={() => setEditing(r)}>
                      {t('ui.edit')}
                    </Button>
                  ) : null}
                </div>
              </li>
            ))}
          </ul>
        )
      ) : (
        <EventsList />
      )}

      {editing !== null ? (
        <RuleFormModal
          existing={editing === 'new' ? null : editing}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            rules.reload()
          }}
        />
      ) : null}
    </section>
  )
}

function EventsList() {
  const t = useT()
  const { formatDate } = useFormatters()
  const q = useAsync<{ items: AlertEvent[]; next_cursor: string | null }>(
    () => listAlertEvents(),
    [],
  )
  if (q.error) return <ErrorState onRetry={q.reload} />
  if (q.loading || !q.data) return <LoadingState />
  if (q.data.items.length === 0)
    return <p className="p-6 text-center text-sm text-ink-muted">{t('alerts.noEvents')}</p>
  return (
    <ul className="space-y-2">
      {q.data.items.map((e) => (
        <li
          key={e.id}
          className="rounded-md border border-surface-sunken bg-surface-raised p-3 text-sm"
        >
          <div className="flex items-center justify-between">
            <span className="font-medium">{e.summary || t(`alerts.type.${e.type}`)}</span>
            <span className="text-xs text-ink-muted">{formatDate(e.at)}</span>
          </div>
          {e.deliveries ? (
            <div className="mt-1 flex flex-wrap gap-1">
              {Object.entries(e.deliveries).map(([channel, result]) => (
                <span key={channel} className="rounded bg-surface-sunken px-1.5 py-0.5 text-xs">
                  {t(`alerts.channel.${channel}`)}: {String(result)}
                </span>
              ))}
            </div>
          ) : null}
        </li>
      ))}
    </ul>
  )
}

function RuleFormModal({
  existing,
  onClose,
  onSaved,
}: {
  existing: AlertRule | null
  onClose: () => void
  onSaved: () => void
}) {
  const t = useT()
  const { toast } = useToast()
  const [name, setName] = useState(existing?.name ?? '')
  const [type, setType] = useState<AlertRuleType>(existing?.type ?? 'nas_down')
  const [channels, setChannels] = useState<Set<AlertChannel>>(
    new Set(existing?.channels ?? ['inapp']),
  )
  const [thresholdValue, setThresholdValue] = useState<number>(
    Number((existing?.threshold as { value?: number } | null)?.value ?? 0),
  )
  const [whatsapp, setWhatsapp] = useState(
    ((existing?.recipients as { whatsapp?: string[] } | null)?.whatsapp ?? []).join(', '),
  )
  const [telegram, setTelegram] = useState(
    ((existing?.recipients as { telegram?: string[] } | null)?.telegram ?? []).join(', '),
  )
  const [email, setEmail] = useState(
    ((existing?.recipients as { email?: string[] } | null)?.email ?? []).join(', '),
  )
  const quiet = (existing?.quiet_hours as { start?: string; end?: string } | null) ?? {}
  const [quietStart, setQuietStart] = useState(quiet.start ?? '')
  const [quietEnd, setQuietEnd] = useState(quiet.end ?? '')
  const [enabled, setEnabled] = useState(existing?.enabled ?? true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  function toggleChannel(c: AlertChannel, on: boolean) {
    setChannels((prev) => {
      const next = new Set(prev)
      if (on) next.add(c)
      else next.delete(c)
      return next
    })
  }

  function splitList(s: string): string[] {
    return s
      .split(',')
      .map((x) => x.trim())
      .filter(Boolean)
  }

  async function submit() {
    setBusy(true)
    setError(null)
    try {
      const body: AlertRuleWrite = {
        name: name.trim(),
        type,
        channels: [...channels],
        threshold: THRESHOLD_TYPES.has(type) ? { value: thresholdValue } : null,
        recipients: {
          whatsapp: splitList(whatsapp),
          telegram: splitList(telegram),
          email: splitList(email),
        },
        quiet_hours: quietStart && quietEnd ? { start: quietStart, end: quietEnd } : null,
        enabled,
      }
      if (existing) await updateAlertRule(existing.id, body)
      else await createAlertRule(body)
      toast(t('alerts.saved'), 'ok')
      onSaved()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t('common.error.body'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open
      onOpenChange={busy ? () => {} : (o) => !o && onClose()}
      title={existing ? t('alerts.editTitle') : t('alerts.createTitle')}
      size="lg"
    >
      <form
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
        className="space-y-4"
      >
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label={t('alerts.name')} htmlFor="ar-name">
            <TextInput
              id="ar-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
            />
          </Field>
          <Field label={t('alerts.ruleType')} htmlFor="ar-type">
            <Select
              id="ar-type"
              value={type}
              onChange={(e) => setType(e.target.value as AlertRuleType)}
            >
              {RULE_TYPES.map((ty) => (
                <option key={ty} value={ty}>
                  {t(`alerts.type.${ty}`)}
                </option>
              ))}
            </Select>
          </Field>
        </div>

        {THRESHOLD_TYPES.has(type) ? (
          <Field
            label={t('alerts.threshold')}
            hint={t(`alerts.thresholdHint.${type}`)}
            htmlFor="ar-threshold"
          >
            <TextInput
              id="ar-threshold"
              type="number"
              value={thresholdValue}
              onChange={(e) => setThresholdValue(Number(e.target.value))}
              dir="ltr"
            />
          </Field>
        ) : null}

        <div>
          <p className="mb-1 text-sm font-medium">{t('alerts.channels')}</p>
          <div className="flex flex-wrap gap-3">
            {CHANNELS.map((c) => (
              <Checkbox
                key={c}
                label={t(`alerts.channel.${c}`)}
                checked={channels.has(c)}
                onChange={(e) => toggleChannel(c, e.target.checked)}
              />
            ))}
          </div>
        </div>

        {channels.has('whatsapp') ? (
          <Field
            label={t('alerts.whatsappRecipients')}
            hint={t('alerts.recipientsHint')}
            htmlFor="ar-wa"
          >
            <TextInput
              id="ar-wa"
              value={whatsapp}
              onChange={(e) => setWhatsapp(e.target.value)}
              dir="ltr"
            />
          </Field>
        ) : null}
        {channels.has('telegram') ? (
          <Field
            label={t('alerts.telegramRecipients')}
            hint={t('alerts.recipientsHint')}
            htmlFor="ar-tg"
          >
            <TextInput
              id="ar-tg"
              value={telegram}
              onChange={(e) => setTelegram(e.target.value)}
              dir="ltr"
            />
          </Field>
        ) : null}
        {channels.has('email') ? (
          <Field
            label={t('alerts.emailRecipients')}
            hint={t('alerts.recipientsHint')}
            htmlFor="ar-em"
          >
            <TextInput
              id="ar-em"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              dir="ltr"
            />
          </Field>
        ) : null}

        <div className="grid gap-3 sm:grid-cols-2">
          <Field label={t('alerts.quietStart')} htmlFor="ar-qs">
            <TextInput
              id="ar-qs"
              type="time"
              value={quietStart}
              onChange={(e) => setQuietStart(e.target.value)}
              dir="ltr"
            />
          </Field>
          <Field label={t('alerts.quietEnd')} htmlFor="ar-qe">
            <TextInput
              id="ar-qe"
              type="time"
              value={quietEnd}
              onChange={(e) => setQuietEnd(e.target.value)}
              dir="ltr"
            />
          </Field>
        </div>

        <Checkbox
          label={t('alerts.enabledLabel')}
          checked={enabled}
          onChange={(e) => setEnabled(e.target.checked)}
        />
        {error ? <p className="text-sm text-danger">{error}</p> : null}
        <div className="flex justify-end gap-2 border-t border-surface-sunken pt-3">
          <Button variant="ghost" disabled={busy} onClick={onClose}>
            {t('ui.cancel')}
          </Button>
          <Button type="submit" disabled={busy || channels.size === 0}>
            {busy ? t('ui.working') : t('ui.save')}
          </Button>
        </div>
      </form>
    </Modal>
  )
}

function TabBtn({
  active,
  onClick,
  children,
}: {
  active: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`-mb-px border-b-2 px-3 py-2 text-sm ${
        active
          ? 'border-brand font-medium text-brand-strong'
          : 'border-transparent text-ink-muted hover:text-ink'
      }`}
    >
      {children}
    </button>
  )
}
