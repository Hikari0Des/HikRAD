import { useState } from 'react'

import { ErrorState, IQDAmount, LoadingState, useT } from '@hikrad/shared'

import {
  createInvoice,
  isPendingApproval,
  listInvoices,
  postInvoiceReceipt,
  voidInvoice,
  type Invoice,
  type InvoiceMode,
} from '../../api/accounting'
import { useAuth } from '../../auth/AuthContext'
import {
  PERM_INVOICES_CREATE,
  PERM_INVOICES_MANAGE,
  PERM_INVOICES_NO_CHARGE,
} from '../../auth/permissions'
import { Button } from '../../components/Button'
import { Field, Select, TextInput, Textarea } from '../../components/form'
import { Modal } from '../../components/Modal'
import { PageHeader } from '../../components/PageHeader'
import { useToast } from '../../components/Toast'
import { usePaginated } from '../../hooks/usePaginated'

const STATUSES = ['unpaid', 'partial', 'due', 'paid', 'void'] as const

/** FR-154: invoices (full/prorated/no-charge), voidable, receipts settle them. */
export function InvoicesPage() {
  const t = useT()
  const { can } = useAuth()
  const [status, setStatus] = useState('')
  const [createOpen, setCreateOpen] = useState(false)
  const [receiptRow, setReceiptRow] = useState<Invoice | null>(null)
  const [voidRow, setVoidRow] = useState<Invoice | null>(null)

  const key = status
  const page = usePaginated<Invoice>((cursor) => listInvoices({ cursor }, { status }), key)

  return (
    <section>
      <PageHeader
        title={t('invoices.title')}
        actions={
          can(PERM_INVOICES_CREATE) ? (
            <Button size="sm" onClick={() => setCreateOpen(true)}>
              {t('invoices.create')}
            </Button>
          ) : null
        }
      />

      <label className="mb-4 block w-48 text-xs">
        <span className="mb-1 block text-ink-muted">{t('invoices.filter.status')}</span>
        <Select value={status} onChange={(e) => setStatus(e.target.value)}>
          <option value="">{t('invoices.filter.allStatuses')}</option>
          {STATUSES.map((s) => (
            <option key={s} value={s}>
              {t(`invoices.status.${s}`)}
            </option>
          ))}
        </Select>
      </label>

      {page.error ? (
        <ErrorState onRetry={page.reset} />
      ) : (
        <div className="overflow-x-auto rounded-md border border-border">
          <table className="w-full min-w-[44rem] text-sm">
            <thead className="bg-surface-sunken/40 text-start text-xs text-ink-muted">
              <tr>
                <th className="px-3 py-2 text-start">{t('invoices.col.no')}</th>
                <th className="px-3 py-2 text-start">{t('invoices.col.issueDate')}</th>
                <th className="px-3 py-2 text-start">{t('invoices.col.dueDate')}</th>
                <th className="px-3 py-2 text-end">{t('invoices.col.total')}</th>
                <th className="px-3 py-2 text-end">{t('invoices.col.paid')}</th>
                <th className="px-3 py-2 text-start">{t('invoices.col.status')}</th>
                <th className="px-3 py-2" />
              </tr>
            </thead>
            <tbody>
              {page.items.map((inv) => (
                <tr key={inv.id} className="border-t border-border/60">
                  <td className="px-3 py-2 font-mono text-xs">{inv.invoice_no}</td>
                  <td className="px-3 py-2 whitespace-nowrap">{inv.issue_date}</td>
                  <td className="px-3 py-2 whitespace-nowrap">{inv.due_date}</td>
                  <td className="px-3 py-2 text-end">
                    <IQDAmount amount={inv.total} />
                  </td>
                  <td className="px-3 py-2 text-end">
                    <IQDAmount amount={inv.amount_paid} />
                  </td>
                  <td className="px-3 py-2">
                    <StatusBadge status={inv.status} />
                  </td>
                  <td className="px-3 py-2 text-end whitespace-nowrap">
                    {inv.status !== 'void' && inv.status !== 'paid' ? (
                      <Button size="sm" variant="ghost" onClick={() => setReceiptRow(inv)}>
                        {t('invoices.receipt')}
                      </Button>
                    ) : null}
                    {can(PERM_INVOICES_MANAGE) && inv.status !== 'void' ? (
                      <Button size="sm" variant="ghost" onClick={() => setVoidRow(inv)}>
                        {t('invoices.void')}
                      </Button>
                    ) : null}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {page.loading ? <LoadingState /> : null}
          {page.items.length === 0 && !page.loading ? (
            <p className="p-6 text-center text-sm text-ink-muted">{t('invoices.empty')}</p>
          ) : null}
          {page.hasMore && !page.loading ? (
            <div className="p-3 text-center">
              <Button size="sm" variant="ghost" onClick={page.loadMore}>
                {t('ui.loadMore')}
              </Button>
            </div>
          ) : null}
        </div>
      )}

      <CreateInvoiceDialog
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onDone={page.reset}
      />
      <ReceiptDialog row={receiptRow} onClose={() => setReceiptRow(null)} onDone={page.reset} />
      <VoidDialog row={voidRow} onClose={() => setVoidRow(null)} onDone={page.reset} />
    </section>
  )
}

function StatusBadge({ status }: { status: Invoice['status'] }) {
  const t = useT()
  const cls: Record<Invoice['status'], string> = {
    unpaid: 'bg-surface-sunken text-ink-muted',
    partial: 'bg-warn/10 text-warn',
    due: 'bg-danger/10 text-danger',
    paid: 'bg-ok/10 text-ok',
    void: 'bg-surface-sunken text-ink-muted line-through',
  }
  return (
    <span className={`rounded px-1.5 py-0.5 text-xs font-medium ${cls[status]}`}>
      {t(`invoices.status.${status}`)}
    </span>
  )
}

function CreateInvoiceDialog({
  open,
  onClose,
  onDone,
}: {
  open: boolean
  onClose: () => void
  onDone: () => void
}) {
  const t = useT()
  const { can } = useAuth()
  const { toast } = useToast()
  const [subscriberId, setSubscriberId] = useState('')
  const [mode, setMode] = useState<InvoiceMode>('full')
  const [dueDate, setDueDate] = useState('')
  const [taxRate, setTaxRate] = useState('0')
  const [description, setDescription] = useState('')
  const [unitPrice, setUnitPrice] = useState('')
  const [busy, setBusy] = useState(false)

  function reset() {
    setSubscriberId('')
    setMode('full')
    setDueDate('')
    setTaxRate('0')
    setDescription('')
    setUnitPrice('')
  }

  async function submit() {
    setBusy(true)
    try {
      await createInvoice({
        subscriber_id: subscriberId || undefined,
        mode,
        due_date: dueDate,
        tax_rate: Number(taxRate) || 0,
        lines: [{ description, quantity: 1, unit_price: Number(unitPrice) || 0 }],
      })
      toast(t('invoices.created'), 'ok')
      reset()
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
      open={open}
      onOpenChange={busy ? () => {} : (o) => !o && onClose()}
      title={t('invoices.create')}
    >
      <div className="space-y-4">
        <Field label={t('invoices.field.subscriberId')} htmlFor="inv-sub">
          <TextInput
            id="inv-sub"
            value={subscriberId}
            onChange={(e) => setSubscriberId(e.target.value)}
            dir="ltr"
          />
        </Field>
        <Field label={t('invoices.field.mode')} htmlFor="inv-mode">
          <Select
            id="inv-mode"
            value={mode}
            onChange={(e) => setMode(e.target.value as InvoiceMode)}
          >
            <option value="full">{t('invoices.mode.full')}</option>
            <option value="prorated">{t('invoices.mode.prorated')}</option>
            {can(PERM_INVOICES_NO_CHARGE) ? (
              <option value="no_charge">{t('invoices.mode.noCharge')}</option>
            ) : null}
          </Select>
        </Field>
        <Field label={t('invoices.field.dueDate')} htmlFor="inv-due">
          <TextInput
            id="inv-due"
            type="date"
            value={dueDate}
            onChange={(e) => setDueDate(e.target.value)}
          />
        </Field>
        <Field label={t('invoices.field.taxRate')} htmlFor="inv-tax">
          <TextInput
            id="inv-tax"
            type="number"
            value={taxRate}
            onChange={(e) => setTaxRate(e.target.value)}
            dir="ltr"
          />
        </Field>
        <Field label={t('invoices.field.description')} htmlFor="inv-desc">
          <TextInput
            id="inv-desc"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
          />
        </Field>
        <Field label={t('invoices.field.unitPrice')} htmlFor="inv-price">
          <TextInput
            id="inv-price"
            type="number"
            value={unitPrice}
            onChange={(e) => setUnitPrice(e.target.value)}
            dir="ltr"
          />
        </Field>
        <div className="flex justify-end gap-2">
          <Button variant="ghost" disabled={busy} onClick={onClose}>
            {t('ui.cancel')}
          </Button>
          <Button disabled={busy || !dueDate || !description} onClick={() => void submit()}>
            {busy ? t('ui.working') : t('invoices.create')}
          </Button>
        </div>
      </div>
    </Modal>
  )
}

function ReceiptDialog({
  row,
  onClose,
  onDone,
}: {
  row: Invoice | null
  onClose: () => void
  onDone: () => void
}) {
  const t = useT()
  const { toast } = useToast()
  const [amount, setAmount] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit() {
    if (!row) return
    setBusy(true)
    try {
      await postInvoiceReceipt(row.id, { amount: Number(amount) || 0 })
      toast(t('invoices.receiptRecorded'), 'ok')
      setAmount('')
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
      title={t('invoices.receipt')}
    >
      <div className="space-y-4">
        <Field label={t('invoices.field.amount')} htmlFor="inv-receipt-amount">
          <TextInput
            id="inv-receipt-amount"
            type="number"
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            dir="ltr"
          />
        </Field>
        <div className="flex justify-end gap-2">
          <Button variant="ghost" disabled={busy} onClick={onClose}>
            {t('ui.cancel')}
          </Button>
          <Button disabled={busy || !amount} onClick={() => void submit()}>
            {busy ? t('ui.working') : t('invoices.receipt')}
          </Button>
        </div>
      </div>
    </Modal>
  )
}

function VoidDialog({
  row,
  onClose,
  onDone,
}: {
  row: Invoice | null
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
      const res = await voidInvoice(row.id, reason.trim())
      toast(isPendingApproval(res) ? t('approvals.parked') : t('invoices.voided'), 'ok')
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
      title={t('invoices.void')}
    >
      <div className="space-y-4">
        <Field label={t('invoices.field.reason')} htmlFor="inv-void-reason">
          <Textarea
            id="inv-void-reason"
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
            {busy ? t('ui.working') : t('invoices.void')}
          </Button>
        </div>
      </div>
    </Modal>
  )
}
