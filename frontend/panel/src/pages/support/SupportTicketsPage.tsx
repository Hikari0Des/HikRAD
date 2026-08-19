import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'

import {
  EmptyState,
  ErrorState,
  LoadingState,
  useFormatters,
  useT,
  type TFunction,
} from '@hikrad/shared'

import {
  addSupportTicketMessage,
  createSupportTicket,
  getSupportTicket,
  listSupportTickets,
  setSupportTicketStatus,
  withdrawSupportTicket,
  type SupportTicket,
  type TicketDetail,
  type TicketPriority,
  type TicketStatus,
} from '../../api/support'
import { Button } from '../../components/Button'
import { Field, Select, Switch, TextInput, Textarea } from '../../components/form'
import { Modal } from '../../components/Modal'
import { PageHeader } from '../../components/PageHeader'
import { Pill } from '../../components/Pill'
import { useToast } from '../../components/Toast'
import { useAsync } from '../../hooks/useAsync'

const STATUSES: TicketStatus[] = ['open', 'in_progress', 'resolved', 'closed']
const PRIORITIES: TicketPriority[] = ['normal', 'moderate', 'critical']

function statusTone(s: TicketStatus): 'ok' | 'danger' | 'muted' {
  if (s === 'resolved' || s === 'closed') return 'ok'
  if (s === 'withdrawn') return 'muted'
  return 'danger'
}

function priorityTone(p: TicketPriority): 'ok' | 'danger' | 'warn' {
  if (p === 'critical') return 'danger'
  if (p === 'moderate') return 'warn'
  return 'ok'
}

/** FR-164: the support ticket queue — filters, scoping (an agent sees only
 * their own subscribers' tickets, via the reused auth.ScopeFilter), reply
 * with a data-level internal/public split, status lifecycle, withdrawal. */
export function SupportTicketsPage() {
  const t = useT()
  const { formatDate } = useFormatters()
  const [scope, setScope] = useState<'mine' | 'all'>('mine')
  const [status, setStatus] = useState<TicketStatus | ''>('')
  const [priority, setPriority] = useState<TicketPriority | ''>('')
  const [search, setSearch] = useState('')
  const { data, error, loading, reload } = useAsync(
    () =>
      listSupportTickets({
        scope,
        status: status || undefined,
        priority: priority || undefined,
        search: search || undefined,
        limit: 100,
      }),
    [scope, status, priority, search],
  )
  const [detailId, setDetailId] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)

  const rows = data?.items ?? []

  return (
    <section>
      <PageHeader
        title={t('support.title')}
        actions={<Button onClick={() => setCreating(true)}>{t('support.new')}</Button>}
      />

      <div className="mb-4 flex flex-wrap items-end gap-4">
        <label className="block text-xs">
          <span className="mb-1 block text-ink-muted">{t('support.filter.status')}</span>
          <Select value={status} onChange={(e) => setStatus(e.target.value as TicketStatus | '')}>
            <option value="">{t('support.filter.all')}</option>
            {STATUSES.map((s) => (
              <option key={s} value={s}>
                {t(`support.status.${s}`)}
              </option>
            ))}
          </Select>
        </label>
        <label className="block text-xs">
          <span className="mb-1 block text-ink-muted">{t('support.filter.priority')}</span>
          <Select
            value={priority}
            onChange={(e) => setPriority(e.target.value as TicketPriority | '')}
          >
            <option value="">{t('support.filter.all')}</option>
            {PRIORITIES.map((p) => (
              <option key={p} value={p}>
                {t(`support.priority.${p}`)}
              </option>
            ))}
          </Select>
        </label>
        <label className="block text-xs">
          <span className="mb-1 block text-ink-muted">{t('support.filter.scope')}</span>
          <Select value={scope} onChange={(e) => setScope(e.target.value as 'mine' | 'all')}>
            <option value="mine">{t('support.scope.mine')}</option>
            <option value="all">{t('support.scope.all')}</option>
          </Select>
        </label>
        <label className="block flex-1 text-xs">
          <span className="mb-1 block text-ink-muted">{t('support.filter.search')}</span>
          <TextInput value={search} onChange={(e) => setSearch(e.target.value)} />
        </label>
      </div>

      {error ? (
        <ErrorState onRetry={reload} />
      ) : loading ? (
        <LoadingState />
      ) : rows.length === 0 ? (
        <EmptyState title={t('support.empty.title')} body={t('support.empty.body')} />
      ) : (
        <div className="overflow-x-auto rounded-md border border-border">
          <table className="w-full min-w-[46rem] text-sm">
            <thead className="bg-surface-sunken/40 text-start text-xs text-ink-muted">
              <tr>
                <th className="px-3 py-2 text-start font-medium">{t('support.col.number')}</th>
                <th className="px-3 py-2 text-start font-medium">{t('support.col.subscriber')}</th>
                <th className="px-3 py-2 text-start font-medium">{t('support.col.subject')}</th>
                <th className="px-3 py-2 text-start font-medium">{t('support.col.priority')}</th>
                <th className="px-3 py-2 text-start font-medium">{t('support.col.status')}</th>
                <th className="px-3 py-2 text-start font-medium">{t('support.col.updated')}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <TicketRow
                  key={r.id}
                  row={r}
                  formatDate={formatDate}
                  onOpen={() => setDetailId(r.id)}
                  t={t}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}

      <TicketDetailModal id={detailId} onClose={() => setDetailId(null)} onDone={reload} />
      <CreateTicketModal
        open={creating}
        onClose={() => setCreating(false)}
        onDone={(id) => {
          setCreating(false)
          reload()
          setDetailId(id)
        }}
      />
    </section>
  )
}

function TicketRow({
  row,
  formatDate,
  onOpen,
  t,
}: {
  row: SupportTicket
  formatDate: (v: string) => string
  onOpen: () => void
  t: TFunction
}) {
  return (
    <tr
      className="cursor-pointer border-t border-border/60 hover:bg-surface-sunken/40"
      onClick={onOpen}
    >
      <td className="px-3 py-2 font-medium">{row.number}</td>
      <td className="px-3 py-2">
        <Link
          to={`/subscribers/${row.subscriber_id}`}
          onClick={(e) => e.stopPropagation()}
          className="text-brand hover:underline"
        >
          {row.subscriber_username}
        </Link>
      </td>
      <td className="max-w-[20rem] truncate px-3 py-2">{row.subject}</td>
      <td className="px-3 py-2">
        <Pill tone={priorityTone(row.priority)}>{t(`support.priority.${row.priority}`)}</Pill>
      </td>
      <td className="px-3 py-2">
        <Pill tone={statusTone(row.status)}>{t(`support.status.${row.status}`)}</Pill>
      </td>
      <td className="px-3 py-2 text-ink-muted">{formatDate(row.updated_at)}</td>
    </tr>
  )
}

function CreateTicketModal({
  open,
  onClose,
  onDone,
}: {
  open: boolean
  onClose: () => void
  onDone: (id: string) => void
}) {
  const t = useT()
  const { toast } = useToast()
  const [subscriberId, setSubscriberId] = useState('')
  const [subject, setSubject] = useState('')
  const [body, setBody] = useState('')
  const [priority, setPriority] = useState<TicketPriority>('normal')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (open) {
      setSubscriberId('')
      setSubject('')
      setBody('')
      setPriority('normal')
    }
  }, [open])

  async function submit() {
    setBusy(true)
    try {
      const tk = await createSupportTicket({
        subscriber_id: subscriberId.trim(),
        subject: subject.trim(),
        body: body.trim(),
        priority,
      })
      toast(t('support.createDone'), 'ok')
      onDone(tk.id)
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal open={open} onOpenChange={(o) => !o && onClose()} title={t('support.new')}>
      <div className="space-y-4">
        <Field
          label={t('support.form.subscriberId')}
          htmlFor="ticket-subscriber-id"
          hint={t('support.form.subscriberIdHint')}
        >
          <TextInput
            id="ticket-subscriber-id"
            dir="ltr"
            value={subscriberId}
            onChange={(e) => setSubscriberId(e.target.value)}
          />
        </Field>
        <Field label={t('support.form.subject')} htmlFor="ticket-subject">
          <TextInput
            id="ticket-subject"
            value={subject}
            onChange={(e) => setSubject(e.target.value)}
          />
        </Field>
        <Field label={t('support.form.priority')} htmlFor="ticket-priority">
          <Select
            id="ticket-priority"
            value={priority}
            onChange={(e) => setPriority(e.target.value as TicketPriority)}
          >
            {PRIORITIES.map((p) => (
              <option key={p} value={p}>
                {t(`support.priority.${p}`)}
              </option>
            ))}
          </Select>
        </Field>
        <Field label={t('support.form.body')} htmlFor="ticket-body">
          <Textarea
            id="ticket-body"
            rows={4}
            value={body}
            onChange={(e) => setBody(e.target.value)}
          />
        </Field>
        <div className="flex justify-end gap-2 border-t border-border pt-4">
          <Button variant="ghost" disabled={busy} onClick={onClose}>
            {t('ui.cancel')}
          </Button>
          <Button
            disabled={busy || !subscriberId.trim() || !subject.trim()}
            onClick={() => void submit()}
          >
            {busy ? t('ui.working') : t('support.create')}
          </Button>
        </div>
      </div>
    </Modal>
  )
}

function TicketDetailModal({
  id,
  onClose,
  onDone,
}: {
  id: string | null
  onClose: () => void
  onDone: () => void
}) {
  const t = useT()
  const { formatDate } = useFormatters()
  const { toast } = useToast()
  const {
    data: ticket,
    loading,
    reload,
  } = useAsync<TicketDetail | null>(() => (id ? getSupportTicket(id) : Promise.resolve(null)), [id])
  const [reply, setReply] = useState('')
  const [internal, setInternal] = useState(false)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    setReply('')
    setInternal(false)
  }, [id])

  async function sendReply() {
    if (!ticket || !reply.trim()) return
    setBusy(true)
    try {
      await addSupportTicketMessage(ticket.id, { body: reply.trim(), internal })
      setReply('')
      setInternal(false)
      await reload()
      onDone()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    } finally {
      setBusy(false)
    }
  }

  async function changeStatus(s: TicketStatus) {
    if (!ticket) return
    setBusy(true)
    try {
      await setSupportTicketStatus(ticket.id, s)
      await reload()
      onDone()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    } finally {
      setBusy(false)
    }
  }

  async function doWithdraw() {
    if (!ticket) return
    setBusy(true)
    try {
      await withdrawSupportTicket(ticket.id)
      await reload()
      onDone()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open={id !== null}
      onOpenChange={(o) => !o && onClose()}
      title={t('support.detailTitle')}
      size="lg"
    >
      {loading || !ticket ? (
        <LoadingState />
      ) : (
        <div className="space-y-4">
          <div className="grid gap-2 text-sm sm:grid-cols-2">
            <div>
              <span className="text-ink-muted">{t('support.col.number')}: </span>
              {ticket.number}
            </div>
            <div>
              <span className="text-ink-muted">{t('support.col.subscriber')}: </span>
              <Link
                to={`/subscribers/${ticket.subscriber_id}`}
                className="text-brand hover:underline"
              >
                {ticket.subscriber_username}
              </Link>
            </div>
            <div className="sm:col-span-2">
              <span className="text-ink-muted">{t('support.col.subject')}: </span>
              {ticket.subject}
            </div>
            <div>
              <span className="text-ink-muted">{t('support.col.priority')}: </span>
              <Pill tone={priorityTone(ticket.priority)}>
                {t(`support.priority.${ticket.priority}`)}
              </Pill>
            </div>
            <div>
              <span className="text-ink-muted">{t('support.col.status')}: </span>
              <Pill tone={statusTone(ticket.status)}>{t(`support.status.${ticket.status}`)}</Pill>
            </div>
          </div>

          <div>
            <p className="mb-2 text-sm font-semibold">{t('support.thread')}</p>
            <ul className="space-y-2">
              {ticket.messages.length === 0 ? (
                <li className="text-xs text-ink-muted">{t('support.noMessages')}</li>
              ) : null}
              {ticket.messages.map((m) => (
                <li
                  key={m.id}
                  className={`rounded-md border p-2 text-sm ${
                    m.internal ? 'border-warn/40 bg-warn/10' : 'border-border'
                  }`}
                >
                  <div className="mb-1 flex items-center justify-between text-xs text-ink-muted">
                    <span>
                      {t(`support.author.${m.author_type}`)}
                      {m.internal ? ` · ${t('support.internalNoteBadge')}` : ''}
                    </span>
                    <span>{formatDate(m.created_at)}</span>
                  </div>
                  <p className="whitespace-pre-wrap">{m.body}</p>
                </li>
              ))}
            </ul>
          </div>

          {ticket.status !== 'withdrawn' ? (
            <div className="space-y-2 border-t border-border pt-4">
              <Field label={t('support.form.reply')} htmlFor="ticket-reply">
                <Textarea
                  id="ticket-reply"
                  rows={3}
                  value={reply}
                  onChange={(e) => setReply(e.target.value)}
                />
              </Field>
              <div className="flex items-center justify-between">
                <Switch
                  label={t('support.internalNote')}
                  description={t('support.internalNoteHint')}
                  checked={internal}
                  onChange={(e) => setInternal(e.target.checked)}
                />
                <Button disabled={busy || !reply.trim()} onClick={() => void sendReply()}>
                  {busy
                    ? t('ui.working')
                    : internal
                      ? t('support.addNote')
                      : t('support.sendReply')}
                </Button>
              </div>
            </div>
          ) : null}

          <div className="flex flex-wrap justify-end gap-2 border-t border-border pt-4">
            {ticket.status !== 'withdrawn' ? (
              <Button variant="ghost" disabled={busy} onClick={() => void doWithdraw()}>
                {t('support.withdraw')}
              </Button>
            ) : null}
            {STATUSES.filter((s) => s !== ticket.status).map((s) => (
              <Button
                key={s}
                variant="secondary"
                disabled={busy}
                onClick={() => void changeStatus(s)}
              >
                {t(`support.moveTo.${s}`)}
              </Button>
            ))}
          </div>
        </div>
      )}
    </Modal>
  )
}
