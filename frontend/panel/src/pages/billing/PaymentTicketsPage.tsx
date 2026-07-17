import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'

import {
  EmptyState,
  ErrorState,
  IQDAmount,
  LoadingState,
  useFormatters,
  useT,
  type TFunction,
} from '@hikrad/shared'

import {
  approveTicket,
  fetchAttachmentBlobUrl,
  getTicket,
  listTickets,
  rejectTicket,
  revealTicketCard,
  type TicketListItem,
  type TicketState,
} from '../../api/paymentTickets'
import { Button } from '../../components/Button'
import { CopyButton } from '../../components/CopyButton'
import { Field, Select, Textarea } from '../../components/form'
import { Modal } from '../../components/Modal'
import { PageHeader } from '../../components/PageHeader'
import { useToast } from '../../components/Toast'
import { useAsync } from '../../hooks/useAsync'

const STATES: (TicketState | '')[] = ['', 'pending', 'approved', 'rejected']

/** Waiting time since submission, e.g. "5m"/"2h 15m" (oldest-first queue). */
function formatWaiting(createdAt: string, t: TFunction): string {
  const ms = Date.now() - new Date(createdAt).getTime()
  const minutes = Math.max(0, Math.floor(ms / 60_000))
  if (minutes < 60) return t('paymentTickets.waitingMinutes', { n: minutes })
  const hours = Math.floor(minutes / 60)
  const rem = minutes % 60
  return t('paymentTickets.waitingHours', { h: hours, m: rem })
}

function methodLabel(row: TicketListItem, t: TFunction): string {
  if (row.method_key === 'scratch_card') return t('paymentTickets.method.scratchCard')
  if (row.method_key === 'voucher') return t('paymentTickets.method.voucher')
  return t('paymentTickets.method.provider')
}

const EVENT_KEYS: Record<string, string> = {
  submitted: 'paymentTickets.event.submitted',
  attachment_added: 'paymentTickets.event.attachmentAdded',
  attachment_failed: 'paymentTickets.event.attachmentFailed',
  trial_granted: 'paymentTickets.event.trialGranted',
  approved: 'paymentTickets.event.approved',
  rejected: 'paymentTickets.event.rejected',
}

function eventLabel(eventType: string, t: TFunction): string {
  const key = EVENT_KEYS[eventType]
  return key ? t(key) : eventType
}

/**
 * Payment ticket verification queue/log (v2-2, C9, generalizes FR-59's
 * card-payment queue to every method). `scope=mine` (default) matches the
 * caller's own subscribers; an unscoped admin may switch to `scope=all` —
 * a scoped caller's toggle is silently downgraded server-side (C9), so it's
 * always safe to show.
 */
export function PaymentTicketsPage() {
  const t = useT()
  const { formatDate } = useFormatters()
  const [scope, setScope] = useState<'mine' | 'all'>('mine')
  const [filter, setFilter] = useState<TicketState | ''>('pending')
  const { data, error, loading, reload } = useAsync(
    () => listTickets({ scope, state: filter || undefined, limit: 100 }),
    [scope, filter],
  )
  const [detailId, setDetailId] = useState<string | null>(null)

  const rows = (data?.items ?? []).slice().sort((a, b) => {
    if (filter !== 'pending')
      return new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
    return new Date(a.created_at).getTime() - new Date(b.created_at).getTime() // oldest first
  })

  return (
    <section>
      <PageHeader title={t('paymentTickets.title')} />
      <div className="mb-4 flex flex-wrap gap-4">
        <label className="block text-xs">
          <span className="mb-1 block text-ink-muted">{t('paymentTickets.filter')}</span>
          <Select
            value={filter}
            onChange={(e) => setFilter(e.target.value as TicketState | '')}
            className="max-w-xs"
          >
            {STATES.map((s) => (
              <option key={s || 'all'} value={s}>
                {s ? t(`paymentTickets.state.${s}`) : t('paymentTickets.filter.all')}
              </option>
            ))}
          </Select>
        </label>
        <label className="block text-xs">
          <span className="mb-1 block text-ink-muted">{t('paymentTickets.scope')}</span>
          <Select value={scope} onChange={(e) => setScope(e.target.value as 'mine' | 'all')}>
            <option value="mine">{t('paymentTickets.scope.mine')}</option>
            <option value="all">{t('paymentTickets.scope.all')}</option>
          </Select>
        </label>
      </div>

      {error ? (
        <ErrorState onRetry={reload} />
      ) : loading ? (
        <LoadingState />
      ) : rows.length === 0 ? (
        <EmptyState title={t('paymentTickets.empty.title')} body={t('paymentTickets.empty.body')} />
      ) : (
        <div className="overflow-x-auto rounded-md border border-surface-sunken">
          <table className="w-full min-w-[46rem] text-sm">
            <thead className="bg-surface-sunken/40 text-start text-xs text-ink-muted">
              <tr>
                <th className="px-3 py-2 text-start font-medium">
                  {t('paymentTickets.col.subscriber')}
                </th>
                <th className="px-3 py-2 text-start font-medium">
                  {t('paymentTickets.col.method')}
                </th>
                <th className="px-3 py-2 text-start font-medium">
                  {t('paymentTickets.col.amount')}
                </th>
                <th className="px-3 py-2 text-start font-medium">
                  {filter === 'pending'
                    ? t('paymentTickets.col.waiting')
                    : t('paymentTickets.col.decided')}
                </th>
                <th className="px-3 py-2" />
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={r.id} className="border-t border-surface-sunken/60">
                  <td className="px-3 py-2">
                    <Link
                      to={`/subscribers/${r.subscriber_id}`}
                      className="text-brand hover:underline"
                    >
                      {r.subscriber_username}
                    </Link>
                  </td>
                  <td className="px-3 py-2">{methodLabel(r, t)}</td>
                  <td className="px-3 py-2">
                    <IQDAmount amount={r.amount} currency={r.currency} />
                  </td>
                  <td className="px-3 py-2 text-ink-muted">
                    {filter === 'pending'
                      ? formatWaiting(r.created_at, t)
                      : r.decided_at
                        ? formatDate(r.decided_at)
                        : '—'}
                  </td>
                  <td className="px-3 py-2 text-end">
                    {r.state === 'pending' ? (
                      <Button size="sm" variant="secondary" onClick={() => setDetailId(r.id)}>
                        {t('paymentTickets.review')}
                      </Button>
                    ) : (
                      <button
                        type="button"
                        onClick={() => setDetailId(r.id)}
                        className={`hover:underline ${r.state === 'approved' ? 'text-ok' : 'text-danger'}`}
                      >
                        {t(`paymentTickets.state.${r.state}`)}
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <TicketDetailModal id={detailId} onClose={() => setDetailId(null)} onDone={reload} />
    </section>
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
  const { data: ticket, loading } = useAsync(
    () => (id ? getTicket(id) : Promise.resolve(null)),
    [id],
  )
  const [busy, setBusy] = useState(false)
  const [rejecting, setRejecting] = useState(false)
  const [reason, setReason] = useState('')
  const [revealed, setRevealed] = useState<string | null>(null)

  useEffect(() => {
    setRejecting(false)
    setReason('')
    setRevealed(null)
  }, [id])

  async function doApprove() {
    if (!ticket) return
    setBusy(true)
    try {
      await approveTicket(ticket.id)
      toast(t('paymentTickets.approveDone'), 'ok')
      onDone()
      onClose()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    } finally {
      setBusy(false)
    }
  }

  async function doReject() {
    if (!ticket) return
    setBusy(true)
    try {
      await rejectTicket(ticket.id, reason.trim())
      toast(t('paymentTickets.rejectDone'), 'ok')
      onDone()
      onClose()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    } finally {
      setBusy(false)
    }
  }

  async function doReveal() {
    if (!ticket) return
    setBusy(true)
    try {
      const res = await revealTicketCard(ticket.id)
      setRevealed(res.code)
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
      title={t('paymentTickets.detailTitle')}
      size="lg"
    >
      {loading || !ticket ? (
        <LoadingState />
      ) : (
        <div className="space-y-4">
          <div className="grid gap-2 text-sm sm:grid-cols-2">
            <div>
              <span className="text-ink-muted">{t('paymentTickets.col.subscriber')}: </span>
              {ticket.subscriber_username}
            </div>
            <div>
              <span className="text-ink-muted">{t('paymentTickets.col.method')}: </span>
              {methodLabel(ticket, t)}
            </div>
            <div>
              <span className="text-ink-muted">{t('paymentTickets.col.amount')}: </span>
              <IQDAmount amount={ticket.amount} currency={ticket.currency} />
            </div>
            {ticket.transfer_reference ? (
              <div>
                <span className="text-ink-muted">{t('paymentTickets.transferReference')}: </span>
                {ticket.transfer_reference}
              </div>
            ) : null}
            {ticket.note ? (
              <div className="sm:col-span-2">
                <span className="text-ink-muted">{t('paymentTickets.note')}: </span>
                {ticket.note}
              </div>
            ) : null}
          </div>

          {ticket.method_key === 'scratch_card' ? (
            <div className="rounded-md border border-surface-sunken p-3">
              {revealed ? (
                <div className="flex items-center gap-2">
                  <code
                    dir="ltr"
                    className="flex-1 rounded-md bg-ink/90 px-3 py-2 text-ink-inverse"
                  >
                    {revealed}
                  </code>
                  <CopyButton text={revealed} />
                </div>
              ) : (
                <Button size="sm" variant="ghost" disabled={busy} onClick={() => void doReveal()}>
                  {t('paymentTickets.reveal')}
                </Button>
              )}
            </div>
          ) : null}

          {ticket.attachments.length > 0 ? (
            <div>
              <p className="mb-2 text-sm font-semibold">{t('paymentTickets.attachments')}</p>
              <div className="flex flex-wrap gap-2">
                {ticket.attachments.map((a) => (
                  <AttachmentThumb key={a.id} ticketId={ticket.id} attachment={a} />
                ))}
              </div>
            </div>
          ) : null}

          <div>
            <p className="mb-2 text-sm font-semibold">{t('paymentTickets.timeline')}</p>
            <ul className="space-y-1 text-xs text-ink-muted">
              {ticket.events.map((e, i) => (
                <li key={i} className="flex justify-between gap-2">
                  <span>{eventLabel(e.event_type, t)}</span>
                  <span>{formatDate(e.at)}</span>
                </li>
              ))}
            </ul>
          </div>

          {ticket.state === 'pending' ? (
            rejecting ? (
              <div className="space-y-3 border-t border-surface-sunken pt-4">
                <div className="rounded-md bg-warn/10 p-3 text-sm text-warn">
                  {t('paymentTickets.rejectConsequence')}
                </div>
                <Field label={t('paymentTickets.rejectReason')} htmlFor="ticket-reject-reason">
                  <Textarea
                    id="ticket-reject-reason"
                    rows={3}
                    value={reason}
                    onChange={(e) => setReason(e.target.value)}
                  />
                </Field>
                <div className="flex justify-end gap-2">
                  <Button variant="ghost" disabled={busy} onClick={() => setRejecting(false)}>
                    {t('ui.cancel')}
                  </Button>
                  <Button
                    variant="danger"
                    disabled={busy || reason.trim().length < 3}
                    onClick={() => void doReject()}
                  >
                    {busy ? t('ui.working') : t('paymentTickets.reject')}
                  </Button>
                </div>
              </div>
            ) : (
              <div className="flex justify-end gap-2 border-t border-surface-sunken pt-4">
                <Button variant="danger" disabled={busy} onClick={() => setRejecting(true)}>
                  {t('paymentTickets.reject')}
                </Button>
                <Button disabled={busy} onClick={() => void doApprove()}>
                  {busy ? t('ui.working') : t('paymentTickets.approve')}
                </Button>
              </div>
            )
          ) : (
            <div className="border-t border-surface-sunken pt-4 text-sm">
              <span className={ticket.state === 'approved' ? 'text-ok' : 'text-danger'}>
                {t(`paymentTickets.state.${ticket.state}`)}
              </span>
              {ticket.reject_reason ? (
                <span className="ms-2 text-ink-muted">— {ticket.reject_reason}</span>
              ) : null}
            </div>
          )}
        </div>
      )}
    </Modal>
  )
}

function AttachmentThumb({
  ticketId,
  attachment,
}: {
  ticketId: string
  attachment: { id: string; filename: string; content_type: string }
}) {
  const t = useT()
  const [url, setUrl] = useState<string | null>(null)
  const [error, setError] = useState(false)

  useEffect(() => {
    let revoked = false
    let objectUrl: string | null = null
    fetchAttachmentBlobUrl(ticketId, attachment.id)
      .then((u) => {
        if (revoked) {
          URL.revokeObjectURL(u)
          return
        }
        objectUrl = u
        setUrl(u)
      })
      .catch(() => setError(true))
    return () => {
      revoked = true
      if (objectUrl) URL.revokeObjectURL(objectUrl)
    }
  }, [ticketId, attachment.id])

  if (error) return null
  if (!url) return <div className="h-16 w-16 animate-pulse rounded-md bg-surface-sunken" />

  const isImage = attachment.content_type.startsWith('image/')
  return isImage ? (
    <a href={url} target="_blank" rel="noreferrer">
      <img
        src={url}
        alt={attachment.filename}
        className="h-16 w-16 rounded-md border border-surface-sunken object-cover"
      />
    </a>
  ) : (
    <a
      href={url}
      target="_blank"
      rel="noreferrer"
      className="flex h-16 w-16 items-center justify-center rounded-md border border-surface-sunken text-xs text-brand underline"
    >
      {t('paymentTickets.attachmentFile')}
    </a>
  )
}
