import { useState, type FormEvent } from 'react'

import { ErrorState, LoadingState, useFormatters, useT } from '@hikrad/shared'

import {
  createTicket,
  getTicket,
  getTickets,
  replyTicket,
  type PortalTicketDetail,
} from '../api/tickets'
import { Badge } from '../components/Badge'
import { useAsync } from '../hooks/useAsync'

/** Support tickets (FR-170, consuming v4-10's FR-164). Allowed under
 * reduced scope (auto-login) — creating/replying to a ticket is neither
 * password, contact, notification, nor payment-config change. */
export function TicketsPage() {
  const t = useT()
  const { formatDate } = useFormatters()
  const tickets = useAsync(getTickets, [])
  const [openId, setOpenId] = useState<string | null>(null)
  const [composing, setComposing] = useState(false)

  if (openId) {
    return <TicketDetail id={openId} onBack={() => setOpenId(null)} />
  }

  return (
    <section className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h1 className="text-lg font-semibold">{t('portal.tickets.title')}</h1>
        <button
          type="button"
          onClick={() => setComposing((v) => !v)}
          className="rounded-md bg-brand px-3 py-1.5 text-sm font-semibold text-ink-inverse"
        >
          {t('portal.tickets.newAction')}
        </button>
      </div>

      {composing ? (
        <NewTicketForm
          onCreated={(id) => {
            setComposing(false)
            tickets.reload()
            setOpenId(id)
          }}
        />
      ) : null}

      {tickets.loading ? (
        <LoadingState />
      ) : tickets.error || !tickets.data ? (
        <ErrorState body={t('portal.tickets.loadError')} onRetry={tickets.reload} />
      ) : tickets.data.items.length === 0 ? (
        <p className="py-6 text-center text-sm text-ink-muted">{t('portal.tickets.empty')}</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {tickets.data.items.map((tk) => (
            <li key={tk.id}>
              <button
                type="button"
                onClick={() => setOpenId(tk.id)}
                className="flex w-full flex-col gap-1 rounded-xl bg-surface-raised p-3 text-start shadow-sm"
              >
                <div className="flex items-center justify-between gap-2">
                  <span className="font-medium">{tk.subject}</span>
                  <Badge tone={tk.status}>{t(`portal.tickets.status.${tk.status}`)}</Badge>
                </div>
                <span className="text-xs text-ink-muted">
                  {tk.number} · {formatDate(tk.updated_at)}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

function NewTicketForm({ onCreated }: { onCreated: (id: string) => void }) {
  const t = useT()
  const [subject, setSubject] = useState('')
  const [body, setBody] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function onSubmit(event: FormEvent) {
    event.preventDefault()
    if (!subject) return
    setSubmitting(true)
    setError(null)
    try {
      const tk = await createTicket(subject, body || undefined)
      onCreated(tk.id)
    } catch {
      setError(t('portal.tickets.error.generic'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <form
      onSubmit={onSubmit}
      className="flex flex-col gap-3 rounded-xl bg-surface-raised p-4 shadow-sm"
    >
      <label className="flex flex-col gap-1 text-sm">
        {t('portal.tickets.subjectLabel')}
        <input
          value={subject}
          onChange={(e) => setSubject(e.target.value)}
          className="rounded-md border border-border bg-surface px-3 py-2 text-base"
        />
      </label>
      <label className="flex flex-col gap-1 text-sm">
        {t('portal.tickets.bodyLabel')}
        <textarea
          value={body}
          onChange={(e) => setBody(e.target.value)}
          rows={3}
          className="rounded-md border border-border bg-surface px-3 py-2 text-base"
        />
      </label>
      {error ? (
        <p role="alert" className="text-sm text-danger">
          {error}
        </p>
      ) : null}
      <button
        type="submit"
        disabled={submitting}
        className="rounded-md bg-brand py-2 font-semibold text-ink-inverse disabled:opacity-60"
      >
        {t('portal.tickets.submit')}
      </button>
    </form>
  )
}

function TicketDetail({ id, onBack }: { id: string; onBack: () => void }) {
  const t = useT()
  const { formatDate } = useFormatters()
  const detail = useAsync(() => getTicket(id), [id])
  const [reply, setReply] = useState('')
  const [submitting, setSubmitting] = useState(false)

  async function onReply(event: FormEvent) {
    event.preventDefault()
    if (!reply) return
    setSubmitting(true)
    try {
      await replyTicket(id, reply)
      setReply('')
      detail.reload()
    } finally {
      setSubmitting(false)
    }
  }

  if (detail.loading) return <LoadingState />
  if (detail.error || !detail.data) {
    return <ErrorState body={t('portal.tickets.loadError')} onRetry={detail.reload} />
  }
  const d: PortalTicketDetail = detail.data

  return (
    <section className="flex flex-col gap-4">
      <button type="button" onClick={onBack} className="self-start text-sm text-brand underline">
        {t('portal.tickets.back')}
      </button>
      <div className="flex items-center justify-between gap-2">
        <h1 className="text-lg font-semibold">{d.subject}</h1>
        <Badge tone={d.status}>{t(`portal.tickets.status.${d.status}`)}</Badge>
      </div>

      <ul className="flex flex-col gap-2">
        {d.messages.map((m) => (
          <li
            key={m.id}
            className={`rounded-xl p-3 text-sm shadow-sm ${
              m.author_type === 'subscriber' ? 'bg-brand/10' : 'bg-surface-raised'
            }`}
          >
            <p>{m.body}</p>
            <p className="mt-1 text-xs text-ink-muted">{formatDate(m.created_at)}</p>
          </li>
        ))}
      </ul>

      {d.status !== 'closed' && d.status !== 'withdrawn' ? (
        <form onSubmit={onReply} className="flex items-center gap-2">
          <input
            value={reply}
            onChange={(e) => setReply(e.target.value)}
            placeholder={t('portal.tickets.replyPlaceholder')}
            className="min-w-0 flex-1 rounded-md border border-border bg-surface px-3 py-2 text-sm"
          />
          <button
            type="submit"
            disabled={submitting}
            className="shrink-0 rounded-md bg-brand px-4 py-2 font-semibold text-ink-inverse disabled:opacity-60"
          >
            {t('portal.tickets.replySubmit')}
          </button>
        </form>
      ) : null}
    </section>
  )
}
