import { useState } from 'react'

import { ErrorState, IQDAmount, LoadingState, useT } from '@hikrad/shared'

import {
  createExpense,
  listExpenses,
  postExpensePayment,
  voidExpense,
  type Expense,
} from '../../api/accounting'
import { useAuth } from '../../auth/AuthContext'
import { PERM_EXPENSES_CREATE, PERM_EXPENSES_MANAGE } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { Field, Select, TextInput, Textarea } from '../../components/form'
import { Modal } from '../../components/Modal'
import { PageHeader } from '../../components/PageHeader'
import { useToast } from '../../components/Toast'
import { usePaginated } from '../../hooks/usePaginated'

const STATUSES = ['unpaid', 'partial', 'due', 'paid', 'void'] as const

/** FR-155: expenses (incurred -> accrued), voidable, payments settle them. */
export function ExpensesPage() {
  const t = useT()
  const { can } = useAuth()
  const [status, setStatus] = useState('')
  const [createOpen, setCreateOpen] = useState(false)
  const [paymentRow, setPaymentRow] = useState<Expense | null>(null)
  const [voidRow, setVoidRow] = useState<Expense | null>(null)

  const page = usePaginated<Expense>((cursor) => listExpenses({ cursor }, status), status)

  return (
    <section>
      <PageHeader
        title={t('expenses.title')}
        actions={
          can(PERM_EXPENSES_CREATE) ? (
            <Button size="sm" onClick={() => setCreateOpen(true)}>
              {t('expenses.create')}
            </Button>
          ) : null
        }
      />

      <label className="mb-4 block w-48 text-xs">
        <span className="mb-1 block text-ink-muted">{t('expenses.filter.status')}</span>
        <Select value={status} onChange={(e) => setStatus(e.target.value)}>
          <option value="">{t('expenses.filter.allStatuses')}</option>
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
                <th className="px-3 py-2 text-start">{t('expenses.col.no')}</th>
                <th className="px-3 py-2 text-start">{t('expenses.col.category')}</th>
                <th className="px-3 py-2 text-start">{t('expenses.col.vendor')}</th>
                <th className="px-3 py-2 text-end">{t('expenses.col.amount')}</th>
                <th className="px-3 py-2 text-end">{t('expenses.col.paid')}</th>
                <th className="px-3 py-2 text-start">{t('expenses.col.status')}</th>
                <th className="px-3 py-2" />
              </tr>
            </thead>
            <tbody>
              {page.items.map((exp) => (
                <tr key={exp.id} className="border-t border-border/60">
                  <td className="px-3 py-2 font-mono text-xs">{exp.expense_no}</td>
                  <td className="px-3 py-2">{exp.category}</td>
                  <td className="px-3 py-2">{exp.vendor || '—'}</td>
                  <td className="px-3 py-2 text-end">
                    <IQDAmount amount={exp.amount} />
                  </td>
                  <td className="px-3 py-2 text-end">
                    <IQDAmount amount={exp.amount_paid} />
                  </td>
                  <td className="px-3 py-2">{t(`invoices.status.${exp.status}`)}</td>
                  <td className="px-3 py-2 text-end whitespace-nowrap">
                    {can(PERM_EXPENSES_CREATE) && exp.status !== 'void' && exp.status !== 'paid' ? (
                      <Button size="sm" variant="ghost" onClick={() => setPaymentRow(exp)}>
                        {t('expenses.payment')}
                      </Button>
                    ) : null}
                    {can(PERM_EXPENSES_MANAGE) && exp.status !== 'void' ? (
                      <Button size="sm" variant="ghost" onClick={() => setVoidRow(exp)}>
                        {t('expenses.void')}
                      </Button>
                    ) : null}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {page.loading ? <LoadingState /> : null}
          {page.items.length === 0 && !page.loading ? (
            <p className="p-6 text-center text-sm text-ink-muted">{t('expenses.empty')}</p>
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

      <CreateExpenseDialog
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onDone={page.reset}
      />
      <PaymentDialog row={paymentRow} onClose={() => setPaymentRow(null)} onDone={page.reset} />
      <VoidExpenseDialog row={voidRow} onClose={() => setVoidRow(null)} onDone={page.reset} />
    </section>
  )
}

function CreateExpenseDialog({
  open,
  onClose,
  onDone,
}: {
  open: boolean
  onClose: () => void
  onDone: () => void
}) {
  const t = useT()
  const { toast } = useToast()
  const [category, setCategory] = useState('')
  const [vendor, setVendor] = useState('')
  const [amount, setAmount] = useState('')
  const [dueDate, setDueDate] = useState('')
  const [memo, setMemo] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit() {
    setBusy(true)
    try {
      await createExpense({
        category,
        vendor: vendor || undefined,
        amount: Number(amount) || 0,
        due_date: dueDate,
        memo: memo || undefined,
      })
      toast(t('expenses.created'), 'ok')
      setCategory('')
      setVendor('')
      setAmount('')
      setDueDate('')
      setMemo('')
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
      title={t('expenses.create')}
    >
      <div className="space-y-4">
        <Field label={t('expenses.field.category')} htmlFor="exp-category">
          <TextInput
            id="exp-category"
            value={category}
            onChange={(e) => setCategory(e.target.value)}
          />
        </Field>
        <Field label={t('expenses.field.vendor')} htmlFor="exp-vendor">
          <TextInput id="exp-vendor" value={vendor} onChange={(e) => setVendor(e.target.value)} />
        </Field>
        <Field label={t('expenses.field.amount')} htmlFor="exp-amount">
          <TextInput
            id="exp-amount"
            type="number"
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            dir="ltr"
          />
        </Field>
        <Field label={t('expenses.field.dueDate')} htmlFor="exp-due">
          <TextInput
            id="exp-due"
            type="date"
            value={dueDate}
            onChange={(e) => setDueDate(e.target.value)}
          />
        </Field>
        <Field label={t('expenses.field.memo')} htmlFor="exp-memo">
          <Textarea id="exp-memo" rows={2} value={memo} onChange={(e) => setMemo(e.target.value)} />
        </Field>
        <div className="flex justify-end gap-2">
          <Button variant="ghost" disabled={busy} onClick={onClose}>
            {t('ui.cancel')}
          </Button>
          <Button disabled={busy || !category || !amount || !dueDate} onClick={() => void submit()}>
            {busy ? t('ui.working') : t('expenses.create')}
          </Button>
        </div>
      </div>
    </Modal>
  )
}

function PaymentDialog({
  row,
  onClose,
  onDone,
}: {
  row: Expense | null
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
      await postExpensePayment(row.id, { amount: Number(amount) || 0 })
      toast(t('expenses.paymentRecorded'), 'ok')
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
      title={t('expenses.payment')}
    >
      <div className="space-y-4">
        <Field label={t('expenses.field.amount')} htmlFor="exp-payment-amount">
          <TextInput
            id="exp-payment-amount"
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
            {busy ? t('ui.working') : t('expenses.payment')}
          </Button>
        </div>
      </div>
    </Modal>
  )
}

function VoidExpenseDialog({
  row,
  onClose,
  onDone,
}: {
  row: Expense | null
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
      await voidExpense(row.id, reason.trim())
      toast(t('expenses.voided'), 'ok')
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
      title={t('expenses.void')}
    >
      <div className="space-y-4">
        <Field label={t('expenses.field.reason')} htmlFor="exp-void-reason">
          <Textarea
            id="exp-void-reason"
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
            {busy ? t('ui.working') : t('expenses.void')}
          </Button>
        </div>
      </div>
    </Modal>
  )
}
