import { useState } from 'react'

import { ErrorState, LoadingState, useFormatters, useT } from '@hikrad/shared'

import {
  approveRequest,
  listApprovalRequests,
  rejectRequest,
  withdrawRequest,
  type ApprovalRequest,
} from '../../api/accounting'
import { useAuth } from '../../auth/AuthContext'
import { PERM_APPROVALS_MANAGE } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { Checkbox, Field, Textarea } from '../../components/form'
import { Modal } from '../../components/Modal'
import { PageHeader } from '../../components/PageHeader'
import { useToast } from '../../components/Toast'
import { useAsync } from '../../hooks/useAsync'

/**
 * FR-159: the agent approval queue. A manager holding approvals.manage sees
 * every pending request; anyone sees their own history via "mine" — the
 * same self-service scope undo/void already give an agent server-side.
 */
export function ApprovalsQueuePage() {
  const t = useT()
  const { can } = useAuth()
  const [mineOnly, setMineOnly] = useState(!can(PERM_APPROVALS_MANAGE))
  const [rejectTarget, setRejectTarget] = useState<ApprovalRequest | null>(null)
  const { toast } = useToast()

  const q = useAsync(
    () => listApprovalRequests({ state: 'pending', mine: mineOnly || undefined }),
    [mineOnly],
  )

  async function approve(id: string) {
    try {
      await approveRequest(id)
      toast(t('approvals.approved'), 'ok')
      q.reload()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    }
  }

  async function withdraw(id: string) {
    try {
      await withdrawRequest(id)
      toast(t('approvals.withdrawn'), 'ok')
      q.reload()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    }
  }

  return (
    <section>
      <PageHeader title={t('approvals.title')} />

      {can(PERM_APPROVALS_MANAGE) ? (
        <div className="mb-4">
          <Checkbox
            label={t('approvals.mineOnly')}
            checked={mineOnly}
            onChange={(e) => setMineOnly(e.target.checked)}
          />
        </div>
      ) : null}

      {q.error ? (
        <ErrorState onRetry={q.reload} />
      ) : q.loading || !q.data ? (
        <LoadingState />
      ) : (
        <div className="overflow-x-auto rounded-md border border-border">
          <table className="w-full min-w-[40rem] text-sm">
            <thead className="bg-surface-sunken/40 text-start text-xs text-ink-muted">
              <tr>
                <th className="px-3 py-2 text-start">{t('approvals.col.kind')}</th>
                <th className="px-3 py-2 text-start">{t('approvals.col.description')}</th>
                <th className="px-3 py-2 text-start">{t('approvals.col.requestedAt')}</th>
                <th className="px-3 py-2" />
              </tr>
            </thead>
            <tbody>
              {q.data.items.map((req) => (
                <ApprovalRow
                  key={req.id}
                  req={req}
                  canDecide={can(PERM_APPROVALS_MANAGE)}
                  onApprove={() => void approve(req.id)}
                  onReject={() => setRejectTarget(req)}
                  onWithdraw={() => void withdraw(req.id)}
                />
              ))}
            </tbody>
          </table>
          {q.data.items.length === 0 ? (
            <p className="p-6 text-center text-sm text-ink-muted">{t('approvals.empty')}</p>
          ) : null}
        </div>
      )}

      <RejectDialog
        row={rejectTarget}
        onClose={() => setRejectTarget(null)}
        onDone={() => q.reload()}
      />
    </section>
  )
}

function ApprovalRow({
  req,
  canDecide,
  onApprove,
  onReject,
  onWithdraw,
}: {
  req: ApprovalRequest
  canDecide: boolean
  onApprove: () => void
  onReject: () => void
  onWithdraw: () => void
}) {
  const t = useT()
  const { formatDate } = useFormatters()
  return (
    <tr className="border-t border-border/60">
      <td className="px-3 py-2">{t(`approvals.kind.${req.kind}`)}</td>
      <td className="px-3 py-2">{req.description || '—'}</td>
      <td className="px-3 py-2 whitespace-nowrap">{formatDate(req.created_at)}</td>
      <td className="px-3 py-2 text-end whitespace-nowrap">
        {canDecide ? (
          <>
            <Button size="sm" variant="secondary" onClick={onApprove}>
              {t('approvals.approve')}
            </Button>
            <Button size="sm" variant="ghost" onClick={onReject}>
              {t('approvals.reject')}
            </Button>
          </>
        ) : (
          <Button size="sm" variant="ghost" onClick={onWithdraw}>
            {t('approvals.withdraw')}
          </Button>
        )}
      </td>
    </tr>
  )
}

function RejectDialog({
  row,
  onClose,
  onDone,
}: {
  row: ApprovalRequest | null
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
      await rejectRequest(row.id, reason.trim())
      toast(t('approvals.rejected'), 'ok')
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
      title={t('approvals.reject')}
    >
      <div className="space-y-4">
        <Field label={t('approvals.field.reason')} htmlFor="approval-reject-reason">
          <Textarea
            id="approval-reject-reason"
            rows={3}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
          />
        </Field>
        <div className="flex justify-end gap-2">
          <Button variant="ghost" disabled={busy} onClick={onClose}>
            {t('ui.cancel')}
          </Button>
          <Button variant="danger" disabled={busy} onClick={() => void submit()}>
            {busy ? t('ui.working') : t('approvals.reject')}
          </Button>
        </div>
      </div>
    </Modal>
  )
}
