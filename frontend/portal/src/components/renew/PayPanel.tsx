import { useState, type ChangeEvent, type FormEvent } from 'react'

import { useFormatters, useT } from '@hikrad/shared'

import { NetworkError } from '../../api/client'
import { listPayMethods, submitTicket, type PayMethod } from '../../api/payMethods'
import { useAsync } from '../../hooks/useAsync'
import { VoucherPanel } from './VoucherPanel'

const MAX_ATTACHMENTS = 5

/**
 * Unified Pay screen (v2-2, contracts C4/C5/C13, FR-78): one tile list of
 * every method the subscriber's owning manager has enabled AND (for a
 * provider) configured an account for — no fallback to any other manager's
 * account exists anywhere in this resolution (Decision 37). Replaces the old
 * separate gateway-list and scratch-card screens; the voucher tile reuses
 * VoucherPanel's existing instant-redeem flow unchanged (a voucher code is
 * self-verifying, unlike a transfer/card-code ticket that needs a human).
 */
export function PayPanel({ onDone }: { onDone?: () => void }) {
  const t = useT()
  const methods = useAsync(listPayMethods, [])
  const [selected, setSelected] = useState<PayMethod | null>(null)

  if (methods.loading) {
    return <p className="py-4 text-center text-sm text-ink-muted">{t('common.loading')}</p>
  }

  const items = methods.data?.items ?? []

  if (items.length === 0) {
    return (
      <div className="flex flex-col gap-2 rounded-xl bg-surface-raised p-4 text-sm shadow-sm">
        <p className="font-semibold">{t('portal.renew.pay.noneTitle')}</p>
        <p className="text-ink-muted">{t('portal.renew.pay.noneBody')}</p>
      </div>
    )
  }

  if (selected) {
    return (
      <div className="flex flex-col gap-3">
        <button
          type="button"
          onClick={() => setSelected(null)}
          className="self-start text-sm font-semibold text-brand underline"
        >
          {t('portal.renew.pay.back')}
        </button>
        {selected.kind === 'voucher' ? (
          <VoucherPanel onRedeemed={onDone} />
        ) : selected.kind === 'scratch_card' ? (
          <ScratchCardForm onSubmitted={onDone} />
        ) : (
          <ProviderTransferForm method={selected} onSubmitted={onDone} />
        )}
      </div>
    )
  }

  return (
    <ul className="flex flex-col gap-2">
      {items.map((m) => (
        <li key={m.key}>
          <button
            type="button"
            onClick={() => setSelected(m)}
            className="flex w-full items-center justify-between rounded-xl bg-surface-raised p-4 text-sm font-semibold shadow-sm"
          >
            <span>{payMethodLabel(t, m)}</span>
            <span aria-hidden="true">›</span>
          </button>
        </li>
      ))}
    </ul>
  )
}

function payMethodLabel(t: ReturnType<typeof useT>, m: PayMethod): string {
  if (m.kind === 'voucher') return t('portal.renew.tab.voucher')
  if (m.kind === 'scratch_card') return t('portal.renew.pay.scratchCardTile')
  return m.provider_name ?? m.key
}

function ScratchCardForm({ onSubmitted }: { onSubmitted?: () => void }) {
  const t = useT()
  const { formatDate } = useFormatters()
  const [cardType, setCardType] = useState('')
  const [code, setCode] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [trialExpiresAt, setTrialExpiresAt] = useState<string | null>(null)

  if (trialExpiresAt) {
    return (
      <div
        role="status"
        className="flex flex-col gap-2 rounded-xl border border-warning bg-warning/10 p-4 text-sm"
      >
        <p className="font-semibold">{t('portal.renew.card.pendingTitle')}</p>
        <p className="text-ink-muted">
          {t('portal.renew.card.pendingBody', {
            time: formatDate(trialExpiresAt, { timeStyle: 'short' }),
          })}
        </p>
      </div>
    )
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    if (!cardType || !code.trim()) return
    setSubmitting(true)
    setError(null)
    try {
      const outcome = await submitTicket({
        methodKey: 'scratch_card',
        cardType,
        cardCode: code.trim(),
      })
      if (outcome.kind === 'ok') {
        setTrialExpiresAt(outcome.result.trial_expires_at ?? null)
        onSubmitted?.()
      } else {
        setError(t(`portal.renew.card.error.${outcome.kind}`))
      }
    } catch (err) {
      setError(
        t(
          err instanceof NetworkError
            ? 'portal.renew.card.error.network'
            : 'portal.renew.card.error.generic',
        ),
      )
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <form
      onSubmit={onSubmit}
      className="flex flex-col gap-3 rounded-xl bg-surface-raised p-4 shadow-sm"
    >
      <label htmlFor="card-type" className="flex flex-col gap-1 text-sm">
        {t('portal.renew.card.typeLabel')}
        <input
          id="card-type"
          value={cardType}
          onChange={(e) => setCardType(e.target.value)}
          placeholder={t('portal.renew.card.typePlaceholder')}
          className="rounded-md border border-surface-sunken bg-surface px-3 py-2 text-base"
        />
      </label>
      <label htmlFor="card-code" className="flex flex-col gap-1 text-sm">
        {t('portal.renew.card.codeLabel')}
        <input
          id="card-code"
          dir="ltr"
          value={code}
          onChange={(e) => setCode(e.target.value)}
          className="rounded-md border border-surface-sunken bg-surface px-3 py-2 font-mono text-base"
        />
      </label>
      {error ? (
        <p role="alert" className="text-sm text-danger">
          {error}
        </p>
      ) : null}
      <button
        type="submit"
        disabled={submitting || !cardType || !code.trim()}
        className="rounded-md bg-brand py-2 font-semibold text-ink-inverse disabled:opacity-60"
      >
        {submitting ? t('portal.renew.card.submitting') : t('portal.renew.card.submit')}
      </button>
    </form>
  )
}

function ProviderTransferForm({
  method,
  onSubmitted,
}: {
  method: PayMethod
  onSubmitted?: () => void
}) {
  const t = useT()
  const [reference, setReference] = useState('')
  const [note, setNote] = useState('')
  const [files, setFiles] = useState<File[]>([])
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [submitted, setSubmitted] = useState(false)

  if (submitted) {
    return (
      <div
        role="status"
        className="flex flex-col gap-2 rounded-xl border border-warning bg-warning/10 p-4 text-sm"
      >
        <p className="font-semibold">{t('portal.renew.pay.submittedTitle')}</p>
        <p className="text-ink-muted">{t('portal.renew.pay.submittedBody')}</p>
      </div>
    )
  }

  function onFilesSelected(e: ChangeEvent<HTMLInputElement>) {
    const chosen = Array.from(e.target.files ?? []).slice(0, MAX_ATTACHMENTS)
    setFiles(chosen)
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    if (!reference.trim()) return
    setSubmitting(true)
    setError(null)
    try {
      const outcome = await submitTicket({
        methodKey: method.key,
        transferReference: reference.trim(),
        note: note.trim() || undefined,
        attachments: files,
      })
      if (outcome.kind === 'ok') {
        setSubmitted(true)
        onSubmitted?.()
      } else {
        setError(t(`portal.renew.pay.error.${outcome.kind}`))
      }
    } catch (err) {
      setError(
        t(
          err instanceof NetworkError
            ? 'portal.renew.pay.error.network'
            : 'portal.renew.pay.error.generic',
        ),
      )
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <form
      onSubmit={onSubmit}
      className="flex flex-col gap-3 rounded-xl bg-surface-raised p-4 shadow-sm"
    >
      {method.account_details ? (
        <div className="rounded-lg bg-surface-sunken p-3 text-sm">
          <p className="font-semibold">{t('portal.renew.pay.accountDetails')}</p>
          <p dir="ltr" className="font-mono">
            {method.account_details}
          </p>
          {method.instructions_text ? (
            <p className="mt-1 text-ink-muted">{method.instructions_text}</p>
          ) : null}
        </div>
      ) : null}
      <label htmlFor="transfer-reference" className="flex flex-col gap-1 text-sm">
        {t('portal.renew.pay.referenceLabel')}
        <input
          id="transfer-reference"
          value={reference}
          onChange={(e) => setReference(e.target.value)}
          className="rounded-md border border-surface-sunken bg-surface px-3 py-2 text-base"
        />
      </label>
      <label htmlFor="transfer-note" className="flex flex-col gap-1 text-sm">
        {t('portal.renew.pay.noteLabel')}
        <textarea
          id="transfer-note"
          value={note}
          onChange={(e) => setNote(e.target.value)}
          rows={2}
          className="rounded-md border border-surface-sunken bg-surface px-3 py-2 text-base"
        />
      </label>
      <label htmlFor="transfer-attachments" className="flex flex-col gap-1 text-sm">
        {t('portal.renew.pay.attachmentsLabel')}
        <input
          id="transfer-attachments"
          type="file"
          accept="image/jpeg,image/png,image/webp,application/pdf"
          multiple
          onChange={onFilesSelected}
          className="text-sm"
        />
      </label>
      {error ? (
        <p role="alert" className="text-sm text-danger">
          {error}
        </p>
      ) : null}
      <button
        type="submit"
        disabled={submitting || !reference.trim()}
        className="rounded-md bg-brand py-2 font-semibold text-ink-inverse disabled:opacity-60"
      >
        {submitting ? t('portal.renew.pay.submitting') : t('portal.renew.pay.submit')}
      </button>
    </form>
  )
}
