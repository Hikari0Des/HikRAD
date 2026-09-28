import { useEffect, useState } from 'react'

import { ErrorState, LoadingState, Ltr, useT } from '@hikrad/shared'

import {
  applyFirewallChange,
  confirmFirewallChange,
  listFirewallRules,
  listPendingFirewall,
  planFirewallChange,
} from '../../../api/nasFirewall'
import type {
  AutoSetupApplyResultItem,
  AutoSetupPreview,
  FirewallPendingChange,
  FirewallRule,
  FirewallTable,
} from '../../../api/types'
import { useAuth } from '../../../auth/AuthContext'
import { PERM_NAS_EDIT } from '../../../auth/permissions'
import { Button } from '../../../components/Button'
import { Modal } from '../../../components/Modal'
import { PlanApplyResults, PlanPreview } from '../../../components/PlanPreview'
import { Select, TextInput } from '../../../components/form'
import { FormField } from '../../../components/patterns'
import { useToast } from '../../../components/Toast'
import { useAsync } from '../../../hooks/useAsync'
import { routerErrorMessage } from '../../../lib/routerErrors'

/**
 * FR-146 Firewall tab. This is the phase's safety-critical write path: an
 * apply that touches an existing HikRAD rule always snapshots the pre-change
 * value first, and any apply with something to revert comes back with a
 * `revert` handle — a live countdown the operator MUST confirm within the
 * window or the backend auto-reverts on its own (gate item 11). The banner
 * below is deliberately impossible to miss: full-width, danger-toned, timer
 * visible at all times while pending.
 */
export function FirewallTab({ nasId }: { nasId: string }) {
  const t = useT()
  const { can } = useAuth()
  const { data, error, loading, reload } = useAsync(() => listFirewallRules(nasId), [nasId])
  const pending = useAsync(() => listPendingFirewall(nasId), [nasId])

  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<FirewallRule | null>(null)
  const [activeRevert, setActiveRevert] = useState<FirewallPendingChange | null>(null)

  const canEdit = can(PERM_NAS_EDIT)
  const rules = data?.items ?? []

  // Whichever pending change is soonest to expire drives the banner — an
  // operator who reloaded mid-window must see the same countdown they'd
  // have seen right after apply, not silence.
  useEffect(() => {
    const items = pending.data?.items ?? []
    if (items.length === 0) {
      setActiveRevert(null)
      return
    }
    setActiveRevert([...items].sort((a, b) => a.deadline.localeCompare(b.deadline))[0])
  }, [pending.data])

  function afterApplyWithRevert(snapshotId: string, deadline: string, description: string) {
    setActiveRevert({ id: snapshotId, description, deadline, created_at: new Date().toISOString() })
  }

  if (error) return <ErrorState onRetry={reload} />
  if (loading) return <LoadingState />

  return (
    <div className="space-y-3">
      {activeRevert ? (
        <RevertBanner
          nasId={nasId}
          pending={activeRevert}
          onConfirmed={() => {
            setActiveRevert(null)
            pending.reload()
          }}
        />
      ) : null}

      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-xs text-ink-muted">{t('nas.firewall.hint')}</p>
        {canEdit ? (
          <Button
            size="sm"
            onClick={() => {
              setEditing(null)
              setFormOpen(true)
            }}
          >
            {t('nas.firewall.addRule')}
          </Button>
        ) : null}
      </div>

      {rules.length === 0 ? (
        <p className="rounded-md border border-dashed border-border bg-surface-raised p-6 text-center text-sm text-ink-muted">
          {t('nas.firewall.empty')}
        </p>
      ) : (
        <ul className="space-y-2">
          {rules.map((r) => (
            <li key={r.id} className="rounded-md border border-border bg-surface-raised p-3">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="flex flex-wrap items-center gap-2 text-sm">
                  <span className="rounded bg-surface-sunken px-1.5 py-0.5 text-xs uppercase">
                    {r.table}
                  </span>
                  <Ltr>
                    <code className="text-xs">
                      chain={r.chain} action={r.action}
                    </code>
                  </Ltr>
                  {r.disabled ? (
                    <span className="rounded bg-surface-sunken px-1.5 py-0.5 text-xs text-ink-muted">
                      {t('nas.firewall.disabled')}
                    </span>
                  ) : null}
                  <span
                    className={`rounded px-1.5 py-0.5 text-xs ${
                      r.ownership === 'system'
                        ? 'bg-brand-soft text-brand-strong'
                        : 'bg-surface-sunken text-ink-muted'
                    }`}
                  >
                    {t(`nas.firewall.ownership.${r.ownership}`)}
                  </span>
                </div>
                {canEdit && r.ownership === 'system' ? (
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => {
                      setEditing(r)
                      setFormOpen(true)
                    }}
                  >
                    {t('ui.edit')}
                  </Button>
                ) : (
                  <span className="text-xs text-ink-muted">{t('nas.firewall.readOnly')}</span>
                )}
              </div>
              {r.comment ? <p className="mt-1 text-xs text-ink-muted">{r.comment}</p> : null}
            </li>
          ))}
        </ul>
      )}

      <FirewallRuleModal
        nasId={nasId}
        open={formOpen}
        editing={editing}
        onOpenChange={setFormOpen}
        onApplied={(revert) => {
          setFormOpen(false)
          reload()
          if (revert)
            afterApplyWithRevert(revert.snapshot_id, revert.deadline, editing?.comment ?? '')
        }}
      />
    </div>
  )
}

/**
 * The auto-revert countdown banner (gate item 11). Ticks every second so the
 * operator always sees how much time remains; Confirm cancels the pending
 * revert server-side.
 */
function RevertBanner({
  nasId,
  pending,
  onConfirmed,
}: {
  nasId: string
  pending: FirewallPendingChange
  onConfirmed: () => void
}) {
  const t = useT()
  const { toast } = useToast()
  const [now, setNow] = useState(() => Date.now())
  const [confirming, setConfirming] = useState(false)

  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(id)
  }, [])

  const remainingMs = Math.max(0, new Date(pending.deadline).getTime() - now)
  const remainingSec = Math.ceil(remainingMs / 1000)
  const mm = Math.floor(remainingSec / 60)
  const ss = remainingSec % 60
  const expired = remainingMs <= 0

  async function runConfirm() {
    setConfirming(true)
    try {
      await confirmFirewallChange(nasId, pending.id)
      toast(t('nas.firewall.revert.confirmed'), 'ok')
      onConfirmed()
    } catch (err) {
      toast(routerErrorMessage(err, t), 'danger')
    } finally {
      setConfirming(false)
    }
  }

  return (
    <div className="rounded-md border-2 border-danger bg-danger/10 p-4">
      <p className="text-sm font-semibold text-danger">{t('nas.firewall.revert.title')}</p>
      <p className="mt-1 text-sm text-ink">{t('nas.firewall.revert.body')}</p>
      <div className="mt-3 flex flex-wrap items-center justify-between gap-3">
        <p className="text-2xl font-bold tabular-nums text-danger">
          {expired ? (
            t('nas.firewall.revert.reverting')
          ) : (
            <Ltr>
              {String(mm).padStart(2, '0')}:{String(ss).padStart(2, '0')}
            </Ltr>
          )}
        </p>
        <Button variant="danger" disabled={confirming || expired} onClick={() => void runConfirm()}>
          {confirming ? t('ui.working') : t('nas.firewall.revert.confirmButton')}
        </Button>
      </div>
    </div>
  )
}

/**
 * Create-or-edit one rule. Editing a router-owned rule is refused server-side
 * (a non-resolvable conflict) — PlanPreview mode="abort" shows exactly why
 * with no keep/update option, only cancel.
 */
function FirewallRuleModal({
  nasId,
  open,
  editing,
  onOpenChange,
  onApplied,
}: {
  nasId: string
  open: boolean
  editing: FirewallRule | null
  onOpenChange: (o: boolean) => void
  onApplied: (revert?: { snapshot_id: string; deadline: string }) => void
}) {
  const t = useT()
  const { toast } = useToast()
  const [table, setTable] = useState<FirewallTable>('filter')
  const [chain, setChain] = useState('input')
  const [action, setAction] = useState('accept')
  const [description, setDescription] = useState('')
  const [preview, setPreview] = useState<AutoSetupPreview | null>(null)
  const [results, setResults] = useState<{
    items: AutoSetupApplyResultItem[]
    allOk: boolean
  } | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open) return
    setTable(editing?.table ?? 'filter')
    setChain(editing?.chain ?? 'input')
    setAction(editing?.action ?? 'accept')
    setDescription('')
    setPreview(null)
    setResults(null)
    setError(null)
  }, [open, editing])

  function fields(): Record<string, string> {
    return { chain, action }
  }

  async function runPlan() {
    setBusy(true)
    setError(null)
    try {
      setPreview(
        await planFirewallChange(nasId, {
          rule_id: editing?.id,
          table,
          fields: fields(),
          description,
        }),
      )
    } catch (err) {
      setError(routerErrorMessage(err, t))
    } finally {
      setBusy(false)
    }
  }

  async function runApply() {
    if (!preview) return
    setBusy(true)
    try {
      const res = await applyFirewallChange(nasId, {
        rule_id: editing?.id,
        table,
        fields: fields(),
        description,
        preview_hash: preview.preview_hash,
      })
      setResults({ items: res.results, allOk: res.all_ok })
      if (res.all_ok) {
        toast(
          res.revert ? t('nas.firewall.applyOkWithRevert') : t('nas.internet.applyOk'),
          res.revert ? 'info' : 'ok',
        )
        onApplied(res.revert)
      } else {
        toast(t('nas.autoSetup.applyPartial'), 'danger')
      }
    } catch (err) {
      toast(routerErrorMessage(err, t), 'danger')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open={open}
      onOpenChange={busy ? () => {} : onOpenChange}
      size="lg"
      title={editing ? t('nas.firewall.editTitle') : t('nas.firewall.createTitle')}
    >
      <div className="space-y-4">
        {!preview && !results ? (
          <>
            <div className="grid gap-3 sm:grid-cols-2">
              <FormField label={t('nas.firewall.table')}>
                {(id) => (
                  <Select
                    id={id}
                    value={table}
                    disabled={!!editing}
                    onChange={(e) => setTable(e.target.value as FirewallTable)}
                  >
                    <option value="filter">{t('nas.firewall.tableFilter')}</option>
                    <option value="nat">{t('nas.firewall.tableNat')}</option>
                  </Select>
                )}
              </FormField>
              <FormField label={t('nas.firewall.chain')}>
                {(id) => (
                  <TextInput
                    id={id}
                    dir="ltr"
                    value={chain}
                    onChange={(e) => setChain(e.target.value)}
                  />
                )}
              </FormField>
              <FormField label={t('nas.firewall.action')}>
                {(id) => (
                  <TextInput
                    id={id}
                    dir="ltr"
                    value={action}
                    onChange={(e) => setAction(e.target.value)}
                  />
                )}
              </FormField>
              <FormField label={t('nas.firewall.description')}>
                {(id) => (
                  <TextInput
                    id={id}
                    value={description}
                    onChange={(e) => setDescription(e.target.value)}
                  />
                )}
              </FormField>
            </div>
            {error ? <p className="text-sm text-danger">{error}</p> : null}
            <Button disabled={busy} onClick={() => void runPlan()}>
              {busy ? t('ui.working') : t('nas.plan.previewButton')}
            </Button>
          </>
        ) : null}

        {preview && !results ? (
          <div className="space-y-3">
            <PlanPreview preview={preview} mode="abort" />
            <div className="flex justify-between gap-2 pt-2">
              <Button variant="ghost" size="sm" disabled={busy} onClick={() => setPreview(null)}>
                {t('ui.back')}
              </Button>
              {preview.conflicts.length === 0 && preview.items.length > 0 ? (
                <Button disabled={busy} onClick={() => void runApply()}>
                  {busy ? t('ui.working') : t('nas.plan.applyButton')}
                </Button>
              ) : null}
            </div>
          </div>
        ) : null}

        {results ? <PlanApplyResults results={results.items} allOk={results.allOk} /> : null}
      </div>
    </Modal>
  )
}
