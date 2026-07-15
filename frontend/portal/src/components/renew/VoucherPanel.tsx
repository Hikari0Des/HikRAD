import { useState, type FormEvent } from 'react'

import { useFormatters, useT } from '@hikrad/shared'

import { redeemVoucher, type RenewResult } from '../../api/vouchers'

/** Voucher redeem (FR-42): format hints, clear used/expired/invalid errors,
 * success screen with new expiry + a CoA-restore reassurance note. */
export function VoucherPanel({ onRedeemed }: { onRedeemed?: () => void }) {
  const t = useT()
  const { formatDate } = useFormatters()
  const [code, setCode] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [result, setResult] = useState<RenewResult | null>(null)

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    if (!code.trim()) return
    setSubmitting(true)
    setError(null)
    try {
      const outcome = await redeemVoucher(code.trim())
      if (outcome.kind === 'ok') {
        setResult(outcome.result)
        onRedeemed?.()
      } else {
        setError(t(`portal.renew.voucher.error.${outcome.kind}`))
      }
    } catch {
      setError(t('portal.renew.voucher.error.network'))
    } finally {
      setSubmitting(false)
    }
  }

  if (result) {
    return (
      <div
        role="status"
        className="flex flex-col gap-2 rounded-xl border border-ok bg-ok/10 p-4 text-sm"
      >
        <p className="font-semibold">{t('portal.renew.voucher.successTitle')}</p>
        <p>
          {t('portal.renew.voucher.successExpiry', { date: formatDate(result.new_expires_at) })}
        </p>
        <p className="text-ink-muted">{t('portal.renew.reconnectNote')}</p>
        <button
          type="button"
          onClick={() => {
            setResult(null)
            setCode('')
          }}
          className="self-start font-semibold text-brand underline"
        >
          {t('portal.renew.voucher.redeemAnother')}
        </button>
      </div>
    )
  }

  return (
    <form
      onSubmit={onSubmit}
      className="flex flex-col gap-3 rounded-xl bg-surface-raised p-4 shadow-sm"
    >
      <label className="flex flex-col gap-1 text-sm">
        {t('portal.renew.voucher.label')}
        <input
          name="voucher"
          dir="ltr"
          placeholder={t('portal.renew.voucher.placeholder')}
          value={code}
          onChange={(e) => setCode(e.target.value.toUpperCase())}
          className="rounded-md border border-surface-sunken bg-surface px-3 py-2 font-mono text-base tracking-wide"
        />
      </label>
      {/* Kept outside the <label> so the input's accessible name stays just
          "Voucher code" (a sibling inside the label would fold into it). */}
      <span className="-mt-2 text-xs text-ink-muted">{t('portal.renew.voucher.hint')}</span>
      {error ? (
        <p role="alert" className="text-sm text-danger">
          {error}
        </p>
      ) : null}
      <button
        type="submit"
        disabled={submitting || !code.trim()}
        className="rounded-md bg-brand py-2 font-semibold text-ink-inverse disabled:opacity-60"
      >
        {submitting ? t('portal.renew.voucher.submitting') : t('portal.renew.voucher.submit')}
      </button>
    </form>
  )
}
