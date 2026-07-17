import { useEffect, useMemo, useRef, useState } from 'react'

import { IQDAmount, useFormatters, useLocale, useT } from '@hikrad/shared'

import { renewSubscriber, printReceipt, type CoAResult, type RenewResult } from '../../api/billing'
import { ApiError } from '../../api/client'
import type { Profile, Subscriber } from '../../api/types'
import { Button } from '../../components/Button'
import { Field, Select, Textarea } from '../../components/form'
import { Modal } from '../../components/Modal'

/**
 * Renew dialog (FR-19, key flow 2) — the product's hero flow. Opens
 * pre-selecting the subscriber's current profile and its resolved price so the
 * whole flow is search → open user (1) → Renew (2) → Confirm (3) = ≤ 3 clicks
 * (NFR-5). The optional profile switch and note never add a required click.
 *
 * On success it surfaces the new expiry, the amount charged, the CoA outcome
 * (restored / disconnect fallback / failed → retry), and receipt actions. A
 * single idempotency key is minted per open and reused across retries so a
 * double-submit can never double-charge.
 */
export function RenewModal({
  open,
  onOpenChange,
  subscriber,
  currentProfileId,
  profiles,
  onRenewed,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  subscriber: Subscriber
  currentProfileId: string | null
  profiles: Profile[]
  onRenewed: () => void
}) {
  const t = useT()

  // Active (non-archived) profiles plus, defensively, the current one even if it
  // was archived after assignment, so the select can always show the default.
  const options = useMemo(
    () => profiles.filter((p) => !p.archived || p.id === currentProfileId),
    [profiles, currentProfileId],
  )

  const [profileId, setProfileId] = useState(currentProfileId ?? options[0]?.id ?? '')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<{ code: string; message: string } | null>(null)
  const [result, setResult] = useState<RenewResult | null>(null)
  // The renew response never carries the price charged (only ledger_tx_id/
  // receipt_no/new_expires_at/coa_result/currency) — captured client-side at
  // submit time from the same preview the operator already saw instead.
  const [charged, setCharged] = useState<{ amount: number; currency: string } | null>(null)

  // Mint a fresh idempotency key whenever the dialog opens; keep it stable across
  // retries within the same open so a retried submit is deduped server-side.
  const idemKey = useRef('')
  useEffect(() => {
    if (open) {
      idemKey.current = crypto.randomUUID()
      setProfileId(currentProfileId ?? options[0]?.id ?? '')
      setNote('')
      setError(null)
      setResult(null)
      setCharged(null)
      setBusy(false)
    }
    // options/currentProfileId are stable enough; reset only on open toggle.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const selected = options.find((p) => p.id === profileId) ?? null
  // Resolved price preview: an active price override applies only when renewing
  // on the current profile; a profile switch always bills the new profile's
  // price. The server is authoritative — this is guidance for the operator.
  const previewPrice =
    selected == null
      ? null
      : profileId === currentProfileId && subscriber.price_override != null
        ? subscriber.price_override
        : selected.price

  async function submit() {
    setBusy(true)
    setError(null)
    try {
      const chargedAtSubmit =
        previewPrice != null && selected != null
          ? { amount: previewPrice, currency: selected.currency }
          : null
      const res = await renewSubscriber(
        subscriber.id,
        { profile_id: profileId || undefined, note: note.trim() || undefined },
        idemKey.current,
      )
      setResult(res)
      setCharged(chargedAtSubmit)
      onRenewed()
    } catch (err) {
      if (err instanceof ApiError) {
        setError({ code: err.code, message: err.message })
        // A profile archived mid-open (422) means the operator's selection is
        // stale; refresh the parent so a re-open shows current profiles.
        if (err.code === 'profile_archived') onRenewed()
      } else {
        setError({ code: 'network', message: t('common.error.body') })
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open={open}
      onOpenChange={busy ? () => {} : onOpenChange}
      title={t('renew.title', { username: subscriber.username })}
    >
      {result ? (
        <RenewSuccess result={result} charged={charged} onClose={() => onOpenChange(false)} />
      ) : (
        <form
          onSubmit={(e) => {
            e.preventDefault()
            void submit()
          }}
          className="space-y-4"
        >
          <Field label={t('renew.profile')} htmlFor="renew-profile">
            <Select
              id="renew-profile"
              value={profileId}
              onChange={(e) => setProfileId(e.target.value)}
              disabled={busy}
            >
              {options.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                  {p.id === currentProfileId ? ` · ${t('renew.currentTag')}` : ''}
                </option>
              ))}
            </Select>
          </Field>

          <div className="flex items-center justify-between rounded-md border border-surface-sunken bg-surface p-3">
            <span className="text-sm text-ink-muted">{t('renew.price')}</span>
            {previewPrice != null ? (
              <span className="text-lg font-semibold">
                <IQDAmount amount={previewPrice} currency={selected?.currency} />
              </span>
            ) : (
              <span className="text-sm text-ink-muted">{t('ui.none')}</span>
            )}
          </div>

          <Field label={t('renew.note')} htmlFor="renew-note">
            <Textarea
              id="renew-note"
              rows={2}
              value={note}
              onChange={(e) => setNote(e.target.value)}
              disabled={busy}
            />
          </Field>

          {error ? <RenewError code={error.code} message={error.message} /> : null}

          <div className="flex justify-end gap-2 pt-1">
            <Button variant="ghost" disabled={busy} onClick={() => onOpenChange(false)}>
              {t('ui.cancel')}
            </Button>
            <Button type="submit" disabled={busy || !profileId}>
              {busy ? t('ui.working') : t('renew.confirm')}
            </Button>
          </div>
        </form>
      )}
    </Modal>
  )
}

/** Inline error with a helpful hint for the balance case (Hassan's agents). */
function RenewError({ code, message }: { code: string; message: string }) {
  const t = useT()
  const known = code === 'insufficient_balance' || code === 'profile_archived'
  return (
    <div role="alert" className="rounded-md bg-danger/10 p-3 text-sm text-danger">
      <p className="font-medium">{known ? t(`renew.error.${code}`) : message}</p>
      {code === 'insufficient_balance' ? (
        <p className="mt-1 text-xs">{t('renew.error.insufficient_balance_hint')}</p>
      ) : null}
    </div>
  )
}

/** Success state: new expiry, amount, CoA outcome, and receipt actions. */
function RenewSuccess({
  result,
  charged,
  onClose,
}: {
  result: RenewResult
  charged: { amount: number; currency: string } | null
  onClose: () => void
}) {
  const t = useT()
  const { locale } = useLocale()
  const { formatDate } = useFormatters()
  const [printing, setPrinting] = useState(false)

  async function print(lang: 'ar' | 'en') {
    setPrinting(true)
    try {
      await printReceipt(result.receipt_no, lang)
    } finally {
      setPrinting(false)
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2 rounded-md bg-ok/10 p-3 text-ok">
        <span aria-hidden="true">✓</span>
        <p className="text-sm font-medium">{t('renew.success')}</p>
      </div>

      <dl className="grid grid-cols-2 gap-3 text-sm">
        <div>
          <dt className="text-xs text-ink-muted">{t('renew.newExpiry')}</dt>
          <dd className="mt-0.5 font-medium">{formatDate(result.new_expires_at)}</dd>
        </div>
        <div>
          <dt className="text-xs text-ink-muted">{t('renew.charged')}</dt>
          <dd className="mt-0.5 font-medium">
            {charged != null ? (
              <IQDAmount amount={charged.amount} currency={charged.currency} />
            ) : (
              <span className="text-ink-muted">{t('ui.none')}</span>
            )}
          </dd>
        </div>
        <div className="col-span-2">
          <dt className="text-xs text-ink-muted">{t('renew.coa')}</dt>
          <dd className="mt-1">
            <CoaBadge outcome={result.coa_result} />
          </dd>
        </div>
        <div className="col-span-2">
          <dt className="text-xs text-ink-muted">{t('renew.receiptNo')}</dt>
          <dd className="mt-0.5">
            <code className="text-sm">{result.receipt_no}</code>
          </dd>
        </div>
      </dl>

      <div className="flex flex-wrap gap-2 border-t border-surface-sunken pt-3">
        <Button size="sm" variant="secondary" disabled={printing} onClick={() => void print('en')}>
          {t('renew.printEn')}
        </Button>
        <Button size="sm" variant="secondary" disabled={printing} onClick={() => void print('ar')}>
          {t('renew.printAr')}
        </Button>
        <span className="grow" />
        <Button size="sm" onClick={onClose}>
          {t('ui.done')}
        </Button>
      </div>
      {locale === 'ku' ? (
        <p className="text-xs text-ink-muted">{t('renew.receiptLangNote')}</p>
      ) : null}
    </div>
  )
}

/** CoA outcome badge; the failed case invites a manual disconnect elsewhere. */
export function CoaBadge({ outcome }: { outcome: CoAResult }) {
  const t = useT()
  const styles: Record<CoAResult, string> = {
    restored: 'bg-ok/10 text-ok',
    disconnect_fallback: 'bg-warn/10 text-warn',
    not_online: 'bg-surface-sunken text-ink-muted',
    failed: 'bg-danger/10 text-danger',
  }
  return (
    <span className={`inline-block rounded px-2 py-1 text-xs font-medium ${styles[outcome]}`}>
      {t(`renew.coaResult.${outcome}`)}
    </span>
  )
}
