import { useEffect, useRef, useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'

import { useFormatters, useT } from '@hikrad/shared'

import { getIntent, type PaymentIntent } from '../api/payments'
import { clearPendingIntent, getPendingIntent } from '../lib/pendingPayment'

const POLL_MS = 3000

/**
 * Payment return/callback route (FR-42, task 3): the gateway hands the
 * subscriber back here after redirect. Polls `GET /portal/payments/intents/
 * {id}` until a terminal state. Deep-link safe: if the tab was closed
 * mid-payment, reopening the app surfaces the persisted pending intent
 * (RenewPage's resume banner) which lands here again with the same id.
 */
export function PaymentReturnPage() {
  const t = useT()
  const { formatDate } = useFormatters()
  // The :gateway segment identifies which gateway redirected back (kept in
  // the URL for clarity/analytics) — polling only needs the intent id.
  useParams<{ gateway: string }>()
  const [searchParams] = useSearchParams()
  const intentId = searchParams.get('intent') ?? getPendingIntent()?.intentId ?? null

  const [intent, setIntent] = useState<PaymentIntent | null>(null)
  const [error, setError] = useState(false)
  const timer = useRef<number>()

  useEffect(() => {
    if (!intentId) return
    let cancelled = false

    async function poll() {
      try {
        const result = await getIntent(intentId!)
        if (cancelled) return
        setIntent(result)
        setError(false)
        if (result.state === 'pending') {
          timer.current = window.setTimeout(poll, POLL_MS)
        } else {
          clearPendingIntent()
        }
      } catch {
        if (cancelled) return
        setError(true)
        timer.current = window.setTimeout(poll, POLL_MS)
      }
    }
    void poll()

    return () => {
      cancelled = true
      window.clearTimeout(timer.current)
    }
  }, [intentId])

  if (!intentId) {
    return (
      <section className="flex flex-col items-center gap-3 py-12 text-center">
        <p className="text-sm text-ink-muted">{t('portal.renew.return.noIntent')}</p>
        <Link to="/renew" className="font-semibold text-brand underline">
          {t('portal.renew.return.backToRenew')}
        </Link>
      </section>
    )
  }

  if (!intent) {
    return (
      <section className="flex flex-col items-center gap-3 py-12 text-center">
        <p role="status" className="text-sm text-ink-muted">
          {error ? t('portal.renew.return.reconnecting') : t('common.loading')}
        </p>
      </section>
    )
  }

  if (intent.state === 'pending') {
    return (
      <section className="flex flex-col items-center gap-3 rounded-xl bg-surface-raised p-6 text-center shadow-sm">
        <p role="status" className="font-semibold">
          {t('portal.renew.return.pendingTitle')}
        </p>
        <p className="text-sm text-ink-muted">{t('portal.renew.return.pendingBody')}</p>
        <p className="text-xs text-ink-muted">
          {t('portal.renew.return.reference', { ref: intent.gateway_ref })}
        </p>
      </section>
    )
  }

  if (intent.state === 'confirmed' || intent.state === 'renewed') {
    return (
      <section
        role="status"
        className="flex flex-col items-center gap-3 rounded-xl border border-ok bg-ok/10 p-6 text-center"
      >
        <p className="font-semibold">{t('portal.renew.return.successTitle')}</p>
        {intent.new_expires_at ? (
          <p className="text-sm">
            {t('portal.renew.voucher.successExpiry', { date: formatDate(intent.new_expires_at) })}
          </p>
        ) : null}
        <p className="text-xs text-ink-muted">{t('portal.renew.reconnectNote')}</p>
        <Link to="/home" className="font-semibold text-brand underline">
          {t('portal.renew.return.backToHome')}
        </Link>
      </section>
    )
  }

  return (
    <section
      role="alert"
      className="flex flex-col items-center gap-3 rounded-xl border border-danger bg-danger/10 p-6 text-center"
    >
      <p className="font-semibold">
        {intent.state === 'expired'
          ? t('portal.renew.return.expiredTitle')
          : t('portal.renew.return.failedTitle')}
      </p>
      <p className="text-sm text-ink-muted">{t('portal.renew.return.failedBody')}</p>
      <Link to="/renew" className="font-semibold text-brand underline">
        {t('portal.renew.return.retry')}
      </Link>
    </section>
  )
}
