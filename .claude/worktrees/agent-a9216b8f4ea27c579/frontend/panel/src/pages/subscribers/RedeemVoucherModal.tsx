import { useState } from 'react'

import { useT } from '@hikrad/shared'

import { redeemVoucher } from '../../api/billing'
import { ApiError } from '../../api/client'
import { Button } from '../../components/Button'
import { Field, TextInput } from '../../components/form'
import { Modal } from '../../components/Modal'
import { useToast } from '../../components/Toast'
import { CoaBadge } from './RenewModal'
import type { CoAResult } from '../../api/billing'

/** Operator voucher redemption on the user page (FR-22): apply a code to renew. */
export function RedeemVoucherModal({
  open,
  onOpenChange,
  subscriberId,
  onRedeemed,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  subscriberId: string
  onRedeemed: () => void
}) {
  const t = useT()
  const { toast } = useToast()
  const [code, setCode] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [coa, setCoa] = useState<CoAResult | null>(null)

  async function submit() {
    setBusy(true)
    setError(null)
    try {
      const res = await redeemVoucher({ code: code.trim(), subscriber_id: subscriberId })
      setCoa(res.coa_result)
      toast(t('vouchers.redeemDone'), 'ok')
      onRedeemed()
    } catch (err) {
      if (err instanceof ApiError && err.code === 'voucher_invalid')
        setError(t('vouchers.redeemInvalid'))
      else setError(err instanceof Error ? err.message : t('common.error.body'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open={open}
      onOpenChange={busy ? () => {} : onOpenChange}
      title={t('vouchers.redeemTitle')}
    >
      {coa ? (
        <div className="space-y-4">
          <div className="rounded-md bg-ok/10 p-3 text-sm text-ok">{t('vouchers.redeemDone')}</div>
          <CoaBadge outcome={coa} />
          <div className="flex justify-end">
            <Button onClick={() => onOpenChange(false)}>{t('ui.done')}</Button>
          </div>
        </div>
      ) : (
        <form
          onSubmit={(e) => {
            e.preventDefault()
            void submit()
          }}
          className="space-y-4"
        >
          <Field label={t('vouchers.code')} error={error ?? undefined} htmlFor="redeem-code">
            <TextInput
              id="redeem-code"
              value={code}
              onChange={(e) => setCode(e.target.value)}
              dir="ltr"
              autoComplete="off"
            />
          </Field>
          <div className="flex justify-end gap-2">
            <Button variant="ghost" disabled={busy} onClick={() => onOpenChange(false)}>
              {t('ui.cancel')}
            </Button>
            <Button type="submit" disabled={busy || code.trim().length < 3}>
              {busy ? t('ui.working') : t('vouchers.redeem')}
            </Button>
          </div>
        </form>
      )}
    </Modal>
  )
}
