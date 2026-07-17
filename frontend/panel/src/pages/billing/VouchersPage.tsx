import { useState } from 'react'

import { ErrorState, IQDAmount, LoadingState, useT } from '@hikrad/shared'

import {
  createVoucherBatch,
  getVoucherBatch,
  listVoucherBatches,
  voidVoucherBatch,
  VoucherBatchError,
  type VoucherBatch,
  type VoucherCode,
} from '../../api/billing'
import { listProfiles } from '../../api/profiles'
import type { Profile } from '../../api/types'
import { useAuth } from '../../auth/AuthContext'
import { PERM_VOUCHERS_CREATE } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { Field, Select, TextInput } from '../../components/form'
import { Modal } from '../../components/Modal'
import { PageHeader } from '../../components/PageHeader'
import { useToast } from '../../components/Toast'
import { useAsync } from '../../hooks/useAsync'

/** Voucher batches (FR-22): create wizard → CSV, batch list + drill-down + void. */
export function VouchersPage() {
  const t = useT()
  const { can } = useAuth()
  const batches = useAsync(() => listVoucherBatches(), [])
  const [createOpen, setCreateOpen] = useState(false)
  const [detail, setDetail] = useState<VoucherBatch | null>(null)
  const [voidTarget, setVoidTarget] = useState<VoucherBatch | null>(null)
  const { toast } = useToast()

  async function doVoid() {
    if (!voidTarget) return
    const res = await voidVoucherBatch(voidTarget.id)
    toast(t('vouchers.voided', { n: res.voided_unused }), 'ok')
    batches.reload()
  }

  return (
    <section>
      <PageHeader
        title={t('vouchers.title')}
        actions={
          can(PERM_VOUCHERS_CREATE) ? (
            <Button size="sm" onClick={() => setCreateOpen(true)}>
              {t('vouchers.create')}
            </Button>
          ) : null
        }
      />

      {batches.error ? (
        <ErrorState onRetry={batches.reload} />
      ) : batches.loading || !batches.data ? (
        <LoadingState />
      ) : batches.data.items.length === 0 ? (
        <p className="rounded-md border border-dashed border-surface-sunken p-8 text-center text-sm text-ink-muted">
          {t('vouchers.empty')}
        </p>
      ) : (
        <div className="overflow-x-auto rounded-md border border-surface-sunken">
          <table className="w-full min-w-[36rem] text-sm">
            <thead className="bg-surface-sunken/40 text-xs text-ink-muted">
              <tr>
                <Th>{t('vouchers.col.prefix')}</Th>
                <Th className="text-end">{t('vouchers.col.count')}</Th>
                <Th className="text-end">{t('vouchers.col.used')}</Th>
                <Th className="text-end">{t('vouchers.col.unused')}</Th>
                <Th className="text-end">{t('vouchers.col.price')}</Th>
                <Th />
              </tr>
            </thead>
            <tbody>
              {batches.data.items.map((b) => (
                <tr key={b.id} className="border-t border-surface-sunken/60">
                  <td className="px-3 py-2">
                    <button
                      type="button"
                      className="text-brand-strong hover:underline"
                      onClick={() => setDetail(b)}
                    >
                      <code>{b.prefix || '—'}</code>
                    </button>
                  </td>
                  <td className="px-3 py-2 text-end">{b.count}</td>
                  <td className="px-3 py-2 text-end">{b.used}</td>
                  <td className="px-3 py-2 text-end">{b.unused}</td>
                  <td className="px-3 py-2 text-end">
                    <IQDAmount amount={b.unit_price} currency={b.currency} />
                  </td>
                  <td className="px-3 py-2 text-end">
                    {can(PERM_VOUCHERS_CREATE) && b.unused > 0 ? (
                      <Button size="sm" variant="ghost" onClick={() => setVoidTarget(b)}>
                        {t('vouchers.void')}
                      </Button>
                    ) : null}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <CreateBatchModal open={createOpen} onOpenChange={setCreateOpen} onCreated={batches.reload} />
      <BatchDetailModal batch={detail} onClose={() => setDetail(null)} />
      <ConfirmDialog
        open={voidTarget !== null}
        onOpenChange={(o) => !o && setVoidTarget(null)}
        title={t('vouchers.voidTitle')}
        body={t('vouchers.voidBody', { n: voidTarget?.unused ?? 0 })}
        confirmLabel={t('vouchers.void')}
        destructive
        onConfirm={doVoid}
      />
    </section>
  )
}

function CreateBatchModal({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  onCreated: () => void
}) {
  const t = useT()
  const { toast } = useToast()
  const profilesQ = useAsync(() => listProfiles(true), [])
  const profiles: Profile[] = profilesQ.data?.items ?? []
  const [profileId, setProfileId] = useState('')
  const [count, setCount] = useState(10)
  const [prefix, setPrefix] = useState('')
  const [codeLength, setCodeLength] = useState(10)
  const [expiresAt, setExpiresAt] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit() {
    setBusy(true)
    setError(null)
    try {
      const { batchId } = await createVoucherBatch({
        profile_id: profileId,
        count,
        prefix: prefix.trim() || undefined,
        expires_at: expiresAt || null,
        code_length: codeLength,
      })
      toast(t('vouchers.created', { id: batchId }), 'ok')
      onOpenChange(false)
      onCreated()
    } catch (err) {
      setError(err instanceof VoucherBatchError ? err.message : t('common.error.body'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open={open}
      onOpenChange={busy ? () => {} : onOpenChange}
      title={t('vouchers.createTitle')}
    >
      <form
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
        className="space-y-4"
      >
        <Field label={t('vouchers.profile')} htmlFor="vb-profile">
          <Select
            id="vb-profile"
            value={profileId}
            onChange={(e) => setProfileId(e.target.value)}
            required
          >
            <option value="" disabled>
              {t('vouchers.pickProfile')}
            </option>
            {profiles.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </Select>
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label={t('vouchers.count')} htmlFor="vb-count">
            <TextInput
              id="vb-count"
              type="number"
              min={1}
              max={5000}
              value={count}
              onChange={(e) => setCount(Number(e.target.value))}
              dir="ltr"
            />
          </Field>
          <Field label={t('vouchers.prefix')} htmlFor="vb-prefix">
            <TextInput
              id="vb-prefix"
              value={prefix}
              onChange={(e) => setPrefix(e.target.value)}
              dir="ltr"
            />
          </Field>
        </div>
        <Field
          label={t('vouchers.codeLength')}
          hint={t('vouchers.codeLengthHint')}
          htmlFor="vb-length"
        >
          <TextInput
            id="vb-length"
            type="number"
            min={10}
            max={24}
            value={codeLength}
            onChange={(e) => setCodeLength(Number(e.target.value))}
            dir="ltr"
          />
        </Field>
        <Field label={t('vouchers.expiry')} hint={t('vouchers.expiryHint')} htmlFor="vb-exp">
          <TextInput
            id="vb-exp"
            type="date"
            value={expiresAt}
            onChange={(e) => setExpiresAt(e.target.value)}
          />
        </Field>
        {error ? <p className="text-sm text-danger">{error}</p> : null}
        <p className="text-xs text-ink-muted">{t('vouchers.csvNote')}</p>
        <div className="flex justify-end gap-2">
          <Button variant="ghost" disabled={busy} onClick={() => onOpenChange(false)}>
            {t('ui.cancel')}
          </Button>
          <Button
            type="submit"
            disabled={busy || !profileId || count < 1 || codeLength < 10 || codeLength > 24}
          >
            {busy ? t('ui.working') : t('vouchers.createDownload')}
          </Button>
        </div>
      </form>
    </Modal>
  )
}

function BatchDetailModal({ batch, onClose }: { batch: VoucherBatch | null; onClose: () => void }) {
  const t = useT()
  const codes = useAsync<{ items: VoucherCode[] }>(
    () => (batch ? getVoucherBatch(batch.id) : Promise.resolve({ items: [] })),
    [batch?.id],
  )
  return (
    <Modal
      open={batch !== null}
      onOpenChange={(o) => !o && onClose()}
      title={t('vouchers.detailTitle')}
      size="lg"
    >
      {codes.error ? (
        <ErrorState onRetry={codes.reload} />
      ) : codes.loading ? (
        <LoadingState />
      ) : (
        <ul className="max-h-80 space-y-1 overflow-y-auto text-sm">
          {(codes.data?.items ?? []).map((c) => (
            <li
              key={c.id}
              className="flex items-center justify-between border-b border-surface-sunken/60 py-1.5"
            >
              <code>{c.id}</code>
              <span className="text-xs text-ink-muted">{t(`vouchers.state.${c.state}`)}</span>
            </li>
          ))}
        </ul>
      )}
    </Modal>
  )
}

function Th({ children, className = '' }: { children?: React.ReactNode; className?: string }) {
  return <th className={`px-3 py-2 text-start font-medium ${className}`}>{children}</th>
}
