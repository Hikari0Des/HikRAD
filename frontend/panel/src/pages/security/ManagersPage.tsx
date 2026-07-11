import { useState } from 'react'

import { ErrorState, IQDAmount, LoadingState, useT } from '@hikrad/shared'

import { getManagerBalance, topupManager } from '../../api/billing'
import { ApiError } from '../../api/client'
import {
  createManager,
  listManagers,
  resetManagerTotp,
  unlockManager,
  updateManager,
  type ManagerView,
} from '../../api/managers'
import { listRoles, type Role } from '../../api/security'
import { useAuth } from '../../auth/AuthContext'
import { PERM_MANAGERS_CREATE, PERM_MANAGERS_EDIT, PERM_TOPUP } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { Checkbox, Field, Select, TextInput } from '../../components/form'
import { Modal } from '../../components/Modal'
import { PageHeader } from '../../components/PageHeader'
import { useToast } from '../../components/Toast'
import { useAsync } from '../../hooks/useAsync'
import { AllowlistModal } from './AllowlistModal'

// Builtin role names used only as a fallback when the roles endpoint is empty
// or forbidden; these are identifiers, not UI copy.
const BUILTIN_ROLES = ['admin', 'operator', 'agent']

/** Managers CRUD + balances + top-up + security actions (FR-27/FR-20/FR-30). */
export function ManagersPage() {
  const t = useT()
  const { can } = useAuth()
  const managers = useAsync(() => listManagers(), [])
  const rolesQ = useAsync(() => listRoles().catch(() => ({ items: [] as Role[] })), [])
  const roles = rolesQ.data?.items ?? []

  const [editing, setEditing] = useState<ManagerView | null | 'new'>(null)
  const [topupFor, setTopupFor] = useState<ManagerView | null>(null)
  const [allowlistFor, setAllowlistFor] = useState<ManagerView | null>(null)

  if (managers.error) return <ErrorState onRetry={managers.reload} />

  return (
    <section>
      <PageHeader
        title={t('managers.title')}
        actions={
          can(PERM_MANAGERS_CREATE) ? (
            <Button size="sm" onClick={() => setEditing('new')}>
              {t('managers.create')}
            </Button>
          ) : null
        }
      />

      {managers.loading || !managers.data ? (
        <LoadingState />
      ) : (
        <div className="overflow-x-auto rounded-md border border-surface-sunken">
          <table className="w-full min-w-[42rem] text-sm">
            <thead className="bg-surface-sunken/40 text-xs text-ink-muted">
              <tr>
                <Th>{t('managers.col.username')}</Th>
                <Th>{t('managers.col.role')}</Th>
                <Th>{t('managers.col.scope')}</Th>
                <Th>{t('managers.col.twofa')}</Th>
                <Th className="text-end">{t('managers.col.balance')}</Th>
                <Th />
              </tr>
            </thead>
            <tbody>
              {managers.data.items.map((m) => (
                <tr key={m.id} className="border-t border-surface-sunken/60">
                  <td className="px-3 py-2 font-medium">
                    <bdi dir="ltr">{m.username}</bdi>
                  </td>
                  <td className="px-3 py-2">{m.role}</td>
                  <td className="px-3 py-2 text-ink-muted">
                    {m.scoped ? t('managers.scoped') : t('managers.global')}
                  </td>
                  <td className="px-3 py-2">
                    {m.totp_enabled ? (
                      <span className="text-ok">{t('ui.yes')}</span>
                    ) : (
                      <span className="text-ink-muted">{t('ui.no')}</span>
                    )}
                  </td>
                  <td className="px-3 py-2 text-end">
                    <BalanceCell managerId={m.id} />
                  </td>
                  <td className="px-3 py-2 text-end">
                    <RowActions
                      manager={m}
                      canEdit={can(PERM_MANAGERS_EDIT)}
                      canTopup={can(PERM_TOPUP)}
                      onEdit={() => setEditing(m)}
                      onTopup={() => setTopupFor(m)}
                      onAllowlist={() => setAllowlistFor(m)}
                      onChanged={managers.reload}
                    />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {editing !== null ? (
        <ManagerFormModal
          existing={editing === 'new' ? null : editing}
          roles={roles}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            managers.reload()
          }}
        />
      ) : null}

      {topupFor ? <TopupModal manager={topupFor} onClose={() => setTopupFor(null)} /> : null}

      {allowlistFor ? (
        <AllowlistModal manager={allowlistFor} onClose={() => setAllowlistFor(null)} />
      ) : null}
    </section>
  )
}

function BalanceCell({ managerId }: { managerId: string }) {
  const q = useAsync(() => getManagerBalance(managerId).catch(() => null), [managerId])
  if (!q.data) return <span className="text-ink-muted">—</span>
  return <IQDAmount amount={q.data.balance_iqd} />
}

function RowActions({
  manager,
  canEdit,
  canTopup,
  onEdit,
  onTopup,
  onAllowlist,
  onChanged,
}: {
  manager: ManagerView
  canEdit: boolean
  canTopup: boolean
  onEdit: () => void
  onTopup: () => void
  onAllowlist: () => void
  onChanged: () => void
}) {
  const t = useT()
  const { toast } = useToast()

  async function unlock() {
    await unlockManager(manager.id)
    toast(t('managers.unlocked'), 'ok')
    onChanged()
  }
  async function resetTotp() {
    await resetManagerTotp(manager.id)
    toast(t('managers.totpReset'), 'ok')
    onChanged()
  }

  return (
    <div className="flex flex-wrap justify-end gap-1">
      {canTopup ? (
        <Button size="sm" variant="ghost" onClick={onTopup}>
          {t('managers.topup')}
        </Button>
      ) : null}
      {canEdit ? (
        <>
          <Button size="sm" variant="ghost" onClick={onEdit}>
            {t('ui.edit')}
          </Button>
          <Button size="sm" variant="ghost" onClick={onAllowlist}>
            {t('managers.allowlist')}
          </Button>
          <Button size="sm" variant="ghost" onClick={() => void unlock()}>
            {t('managers.unlock')}
          </Button>
          {manager.totp_enabled ? (
            <Button size="sm" variant="ghost" onClick={() => void resetTotp()}>
              {t('managers.resetTotp')}
            </Button>
          ) : null}
        </>
      ) : null}
    </div>
  )
}

function ManagerFormModal({
  existing,
  roles,
  onClose,
  onSaved,
}: {
  existing: ManagerView | null
  roles: Role[]
  onClose: () => void
  onSaved: () => void
}) {
  const t = useT()
  const { toast } = useToast()
  const [username, setUsername] = useState(existing?.username ?? '')
  const [role, setRole] = useState(existing?.role ?? roles[0]?.name ?? 'operator')
  const [scoped, setScoped] = useState(existing?.scoped ?? false)
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit() {
    setBusy(true)
    setError(null)
    try {
      if (existing) {
        await updateManager(existing.id, { role, scoped, password: password || undefined })
      } else {
        await createManager({ username: username.trim(), password, role, scoped })
      }
      toast(t('managers.saved'), 'ok')
      onSaved()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t('common.error.body'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open
      onOpenChange={busy ? () => {} : (o) => !o && onClose()}
      title={existing ? t('managers.editTitle') : t('managers.createTitle')}
    >
      <form
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
        className="space-y-4"
      >
        <Field label={t('managers.username')} htmlFor="mgr-user">
          <TextInput
            id="mgr-user"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            disabled={!!existing}
            dir="ltr"
            required
          />
        </Field>
        <Field label={t('managers.role')} htmlFor="mgr-role">
          <Select id="mgr-role" value={role} onChange={(e) => setRole(e.target.value)}>
            {(roles.length === 0 ? BUILTIN_ROLES : roles.map((r) => r.name)).map((name) => (
              <option key={name} value={name}>
                {name}
              </option>
            ))}
          </Select>
        </Field>
        <Field
          label={existing ? t('managers.newPassword') : t('managers.password')}
          hint={existing ? t('managers.passwordEditHint') : undefined}
          htmlFor="mgr-pw"
        >
          <TextInput
            id="mgr-pw"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="new-password"
            required={!existing}
          />
        </Field>
        <Checkbox
          label={t('managers.scopedLabel')}
          description={t('managers.scopedHint')}
          checked={scoped}
          onChange={(e) => setScoped(e.target.checked)}
        />
        {error ? <p className="text-sm text-danger">{error}</p> : null}
        <div className="flex justify-end gap-2">
          <Button variant="ghost" disabled={busy} onClick={onClose}>
            {t('ui.cancel')}
          </Button>
          <Button type="submit" disabled={busy}>
            {busy ? t('ui.working') : t('ui.save')}
          </Button>
        </div>
      </form>
    </Modal>
  )
}

function TopupModal({ manager, onClose }: { manager: ManagerView; onClose: () => void }) {
  const t = useT()
  const { toast } = useToast()
  const [amount, setAmount] = useState(0)
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit() {
    setBusy(true)
    try {
      const res = await topupManager(manager.id, {
        amount_iqd: amount,
        note: note.trim() || undefined,
      })
      toast(t('managers.topupDone'), 'ok')
      void res
      onClose()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open
      onOpenChange={busy ? () => {} : (o) => !o && onClose()}
      title={t('managers.topupTitle', { name: manager.username })}
    >
      <form
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
        className="space-y-4"
      >
        <Field label={t('managers.topupAmount')} htmlFor="topup-amt">
          <TextInput
            id="topup-amt"
            type="number"
            min={1}
            value={amount}
            onChange={(e) => setAmount(Number(e.target.value))}
            dir="ltr"
            required
          />
        </Field>
        <Field label={t('managers.topupNote')} htmlFor="topup-note">
          <TextInput id="topup-note" value={note} onChange={(e) => setNote(e.target.value)} />
        </Field>
        <div className="flex justify-end gap-2">
          <Button variant="ghost" disabled={busy} onClick={onClose}>
            {t('ui.cancel')}
          </Button>
          <Button type="submit" disabled={busy || amount < 1}>
            {busy ? t('ui.working') : t('managers.topup')}
          </Button>
        </div>
      </form>
    </Modal>
  )
}

function Th({ children, className = '' }: { children?: React.ReactNode; className?: string }) {
  return <th className={`px-3 py-2 text-start font-medium ${className}`}>{children}</th>
}
