import { useState } from 'react'
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
  approveCardPayment,
  listCardPayments,
  rejectCardPayment,
  revealCardPayment,
  type CardPaymentRow,
  type CardPaymentState,
} from '../../api/cardpayments'
import { Button } from '../../components/Button'
import { CopyButton } from '../../components/CopyButton'
import { Field, Select, Textarea } from '../../components/form'
import { Modal } from '../../components/Modal'
import { PageHeader } from '../../components/PageHeader'
import { useToast } from '../../components/Toast'
import { useAsync } from '../../hooks/useAsync'

const STATES: (CardPaymentState | '')[] = ['', 'pending', 'approved', 'rejected']

/** Waiting time since submission, e.g. "5m"/"2h 15m" (oldest-first queue). */
function formatWaiting(createdAt: string, t: TFunction): string {
  const ms = Date.now() - new Date(createdAt).getTime()
  const minutes = Math.max(0, Math.floor(ms / 60_000))
  if (minutes < 60) return t('cardPayments.waitingMinutes', { n: minutes })
  const hours = Math.floor(minutes / 60)
  const rem = minutes % 60
  return t('cardPayments.waitingHours', { h: hours, m: rem })
}

/**
 * Card-payment verification queue (FR-59, task 2c). Pending items sort
 * oldest-first (they're a waiting-time queue); reveal/approve/reject are each
 * confirmed and audited server-side.
 */
export function CardPaymentsPage() {
  const t = useT()
  const { formatDate } = useFormatters()
  const [filter, setFilter] = useState<CardPaymentState | ''>('pending')
  const { data, error, loading, reload } = useAsync(
    () => listCardPayments(filter || undefined),
    [filter],
  )
  const [revealRow, setRevealRow] = useState<CardPaymentRow | null>(null)
  const [approveRow, setApproveRow] = useState<CardPaymentRow | null>(null)
  const [rejectRow, setRejectRow] = useState<CardPaymentRow | null>(null)

  const rows = (data?.items ?? []).slice().sort((a, b) => {
    if (filter !== 'pending')
      return new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
    return new Date(a.created_at).getTime() - new Date(b.created_at).getTime() // oldest first
  })

  return (
    <section>
      <PageHeader title={t('cardPayments.title')} />
      <label className="mb-4 block text-xs">
        <span className="mb-1 block text-ink-muted">{t('cardPayments.filter')}</span>
        <Select
          value={filter}
          onChange={(e) => setFilter(e.target.value as CardPaymentState | '')}
          className="max-w-xs"
        >
          {STATES.map((s) => (
            <option key={s || 'all'} value={s}>
              {s ? t(`cardPayments.state.${s}`) : t('cardPayments.filter.all')}
            </option>
          ))}
        </Select>
      </label>

      {error ? (
        <ErrorState onRetry={reload} />
      ) : loading ? (
        <LoadingState />
      ) : rows.length === 0 ? (
        <EmptyState title={t('cardPayments.empty.title')} body={t('cardPayments.empty.body')} />
      ) : (
        <div className="overflow-x-auto rounded-md border border-surface-sunken">
          <table className="w-full min-w-[42rem] text-sm">
            <thead className="bg-surface-sunken/40 text-start text-xs text-ink-muted">
              <tr>
                <th className="px-3 py-2 text-start font-medium">
                  {t('cardPayments.col.subscriber')}
                </th>
                <th className="px-3 py-2 text-start font-medium">
                  {t('cardPayments.col.profile')}
                </th>
                <th className="px-3 py-2 text-start font-medium">
                  {t('cardPayments.col.cardType')}
                </th>
                <th className="px-3 py-2 text-start font-medium">
                  {filter === 'pending'
                    ? t('cardPayments.col.waiting')
                    : t('cardPayments.col.decided')}
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
                      {r.username}
                    </Link>
                  </td>
                  <td className="px-3 py-2">
                    {r.profile_name} ·{' '}
                    <IQDAmount amount={r.requested_amount} currency={r.currency} />
                  </td>
                  <td className="px-3 py-2 capitalize">{r.card_type}</td>
                  <td className="px-3 py-2 text-ink-muted">
                    {filter === 'pending'
                      ? formatWaiting(r.created_at, t)
                      : r.decided_at
                        ? formatDate(r.decided_at)
                        : '—'}
                  </td>
                  <td className="px-3 py-2 text-end">
                    {r.state === 'pending' ? (
                      <div className="flex justify-end gap-1.5">
                        <Button size="sm" variant="ghost" onClick={() => setRevealRow(r)}>
                          {t('cardPayments.reveal')}
                        </Button>
                        <Button size="sm" variant="secondary" onClick={() => setApproveRow(r)}>
                          {t('cardPayments.approve')}
                        </Button>
                        <Button size="sm" variant="danger" onClick={() => setRejectRow(r)}>
                          {t('cardPayments.reject')}
                        </Button>
                      </div>
                    ) : (
                      <span className={r.state === 'approved' ? 'text-ok' : 'text-danger'}>
                        {t(`cardPayments.state.${r.state}`)}
                      </span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <RevealDialog row={revealRow} onClose={() => setRevealRow(null)} />
      <ApproveDialog row={approveRow} onClose={() => setApproveRow(null)} onDone={reload} />
      <RejectDialog row={rejectRow} onClose={() => setRejectRow(null)} onDone={reload} />
    </section>
  )
}

function RevealDialog({ row, onClose }: { row: CardPaymentRow | null; onClose: () => void }) {
  const t = useT()
  const { toast } = useToast()
  const [code, setCode] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function confirmReveal() {
    if (!row) return
    setBusy(true)
    try {
      const res = await revealCardPayment(row.id)
      setCode(res.code)
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
      onClose()
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open={row !== null}
      onOpenChange={(o) => {
        if (!o) {
          setCode(null)
          onClose()
        }
      }}
      title={t('cardPayments.revealTitle')}
    >
      {code ? (
        <div className="space-y-3">
          <p className="text-sm text-ink-muted">{t('cardPayments.revealedNote')}</p>
          <div className="flex items-center gap-2">
            <code dir="ltr" className="flex-1 rounded-md bg-ink/90 px-3 py-2 text-ink-inverse">
              {code}
            </code>
            <CopyButton text={code} />
          </div>
        </div>
      ) : (
        <div>
          <p className="text-sm text-ink-muted">{t('cardPayments.revealConfirm')}</p>
          <div className="mt-4 flex justify-end gap-2">
            <Button variant="ghost" disabled={busy} onClick={onClose}>
              {t('ui.cancel')}
            </Button>
            <Button disabled={busy} onClick={() => void confirmReveal()}>
              {busy ? t('ui.working') : t('cardPayments.reveal')}
            </Button>
          </div>
        </div>
      )}
    </Modal>
  )
}

function ApproveDialog({
  row,
  onClose,
  onDone,
}: {
  row: CardPaymentRow | null
  onClose: () => void
  onDone: () => void
}) {
  const t = useT()
  const { formatDate } = useFormatters()
  const { toast } = useToast()
  const [busy, setBusy] = useState(false)
  const [expiry, setExpiry] = useState<string | null>(null)

  async function confirm() {
    if (!row) return
    setBusy(true)
    try {
      const res = await approveCardPayment(row.id)
      setExpiry(res.new_expires_at)
      toast(t('cardPayments.approveDone'), 'ok')
      onDone()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open={row !== null}
      onOpenChange={(o) => {
        if (!o) {
          setExpiry(null)
          onClose()
        }
      }}
      title={t('cardPayments.approveTitle')}
    >
      {expiry ? (
        <p className="text-sm">{t('cardPayments.approveResult', { at: formatDate(expiry) })}</p>
      ) : (
        <>
          <p className="text-sm text-ink-muted">
            {row ? t('cardPayments.approveConfirm', { profile: row.profile_name }) : ''}
          </p>
          <div className="mt-4 flex justify-end gap-2">
            <Button variant="ghost" disabled={busy} onClick={onClose}>
              {t('ui.cancel')}
            </Button>
            <Button disabled={busy} onClick={() => void confirm()}>
              {busy ? t('ui.working') : t('cardPayments.approve')}
            </Button>
          </div>
        </>
      )}
    </Modal>
  )
}

function RejectDialog({
  row,
  onClose,
  onDone,
}: {
  row: CardPaymentRow | null
  onClose: () => void
  onDone: () => void
}) {
  const t = useT()
  const { toast } = useToast()
  const [reason, setReason] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit() {
    if (!row) return
    setBusy(true)
    try {
      await rejectCardPayment(row.id, reason.trim())
      toast(t('cardPayments.rejectDone'), 'ok')
      setReason('')
      onClose()
      onDone()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open={row !== null}
      onOpenChange={busy ? () => {} : (o) => !o && onClose()}
      title={t('cardPayments.rejectTitle')}
    >
      <div className="space-y-4">
        <div className="rounded-md bg-warn/10 p-3 text-sm text-warn">
          {t('cardPayments.rejectConsequence')}
        </div>
        <Field label={t('cardPayments.rejectReason')} htmlFor="reject-reason">
          <Textarea
            id="reject-reason"
            rows={3}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
          />
        </Field>
        <div className="flex justify-end gap-2">
          <Button variant="ghost" disabled={busy} onClick={onClose}>
            {t('ui.cancel')}
          </Button>
          <Button
            variant="danger"
            disabled={busy || reason.trim().length < 3}
            onClick={() => void submit()}
          >
            {busy ? t('ui.working') : t('cardPayments.reject')}
          </Button>
        </div>
      </div>
    </Modal>
  )
}
