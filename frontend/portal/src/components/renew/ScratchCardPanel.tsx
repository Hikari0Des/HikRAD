import { useState, type FormEvent } from 'react'

import { useFormatters, useT } from '@hikrad/shared'

import { NetworkError } from '../../api/client'
import { listCardTypes, submitCardPayment, type MyCardPayment } from '../../api/cardPayments'
import { useAsync } from '../../hooks/useAsync'

/**
 * Scratch-card renewal (FR-59, contract C8, task 3b): card-type picker + code
 * entry → "1-day test internet active, pending ISP verification" state. If
 * the subscriber already has a pending/rejected card payment (from
 * `getMyCardPayment`, surfaced on the home screen too), this panel reflects
 * it here rather than letting them double-submit.
 */
export function ScratchCardPanel({
  myCardPayment,
}: {
  myCardPayment: MyCardPayment | null | undefined
}) {
  const t = useT()
  const { formatDate } = useFormatters()
  const types = useAsync(listCardTypes, [])
  const [cardType, setCardType] = useState('')
  const [code, setCode] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [trialExpiresAt, setTrialExpiresAt] = useState<string | null>(null)

  if (myCardPayment?.state === 'pending') {
    return (
      <div
        role="status"
        className="flex flex-col gap-2 rounded-xl border border-warning bg-warning/10 p-4 text-sm"
      >
        <p className="font-semibold">{t('portal.renew.card.pendingTitle')}</p>
        <p className="text-ink-muted">
          {t('portal.renew.card.pendingBody', {
            time: formatDate(myCardPayment.trial_expires_at, { timeStyle: 'short' }),
          })}
        </p>
      </div>
    )
  }

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
      const outcome = await submitCardPayment(cardType, code.trim())
      if (outcome.kind === 'ok') {
        setTrialExpiresAt(outcome.trial_expires_at)
      } else if (outcome.kind === 'cooldown') {
        setError(t('portal.renew.card.error.cooldown'))
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
      {/* Explicit htmlFor/id (not wrapping): a wrapped <select>'s option text
          would fold into its own accessible name, per the accname spec. */}
      <label htmlFor="card-type" className="flex flex-col gap-1 text-sm">
        {t('portal.renew.card.typeLabel')}
        <select
          id="card-type"
          value={cardType}
          onChange={(e) => setCardType(e.target.value)}
          className="rounded-md border border-surface-sunken bg-surface px-3 py-2 text-base"
        >
          <option value="">{t('portal.renew.card.typePlaceholder')}</option>
          {types.data?.items.map((ct) => (
            <option key={ct.id} value={ct.id}>
              {ct.name}
            </option>
          ))}
        </select>
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
