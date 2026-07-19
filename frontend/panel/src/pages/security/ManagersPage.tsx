import { useState } from 'react'

import { ErrorState, IQDAmount, LoadingState, useT } from '@hikrad/shared'

import { getManagerBalance, listCurrencies, topupManager } from '../../api/billing'
import { ApiError } from '../../api/client'
import {
  createManager,
  deleteManager,
  listManagers,
  resetManagerTotp,
  unlockManager,
  updateManager,
  type ManagerView,
} from '../../api/managers'
import { listRoles, type Role } from '../../api/security'
import { useAuth } from '../../auth/AuthContext'
import {
  PERM_MANAGERS_CREATE,
  PERM_MANAGERS_DELETE,
  PERM_MANAGERS_EDIT,
  PERM_TOPUP,
} from '../../auth/permissions'
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
  const { can, manager: me } = useAuth()
  const managers = useAsync(() => listManagers(), [])
  const rolesQ = useAsync(() => listRoles().catch(() => ({ items: [] as Role[] })), [])
  const roles = rolesQ.data?.items ?? []

  const [editing, setEditing] = useState<ManagerView | null | 'new'>(null)
  const [topupFor, setTopupFor] = useState<ManagerView | null>(null)
  const [allowlistFor, setAllowlistFor] = useState<ManagerView | null>(null)
  const [removing, setRemoving] = useState<ManagerView | null>(null)

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
                <Th>{t('managers.col.status')}</Th>
                <Th>{t('managers.col.twofa')}</Th>
                <Th className="text-end">{t('managers.col.balance')}</Th>
                <Th />
              </tr>
            </thead>
            <tbody>
              {managers.data.items.map((m) => (
                <tr key={m.id} className="border-t border-surface-sunken/60">
                  <td className="px-3 py-2">
                    <span className="font-medium">
                      <bdi dir="ltr">{m.username}</bdi>
                    </span>
                    {m.full_name ? (
                      <span className="block text-xs text-ink-muted">{m.full_name}</span>
                    ) : null}
                    {m.phone ? (
                      <span className="block text-xs text-ink-muted" dir="ltr">
                        {m.phone}
                      </span>
                    ) : null}
                  </td>
                  <td className="px-3 py-2">{m.role}</td>
                  <td className="px-3 py-2 text-ink-muted">
                    {m.scoped ? t('managers.scoped') : t('managers.global')}
                  </td>
                  <td className="px-3 py-2">
                    {m.disabled ? (
                      <span className="rounded bg-danger/10 px-1.5 py-0.5 text-xs font-medium text-danger">
                        {t('managers.disabledBadge')}
                      </span>
                    ) : (
                      <span className="text-ok">{t('managers.active')}</span>
                    )}
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
                      isSelf={me?.id === m.id}
                      canEdit={can(PERM_MANAGERS_EDIT)}
                      canDelete={can(PERM_MANAGERS_DELETE)}
                      canTopup={can(PERM_TOPUP)}
                      onEdit={() => setEditing(m)}
                      onTopup={() => setTopupFor(m)}
                      onAllowlist={() => setAllowlistFor(m)}
                      onRemove={() => setRemoving(m)}
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

      {removing ? (
        <RemoveManagerModal
          manager={removing}
          onClose={() => setRemoving(null)}
          onChanged={() => {
            setRemoving(null)
            managers.reload()
          }}
        />
      ) : null}
    </section>
  )
}

/** Maps the delete/disable 409 codes to operator-readable messages. */
function removalErrorMessage(t: ReturnType<typeof useT>, err: unknown): string {
  if (err instanceof ApiError) {
    if (err.code === 'cannot_remove_self' || err.code === 'cannot_disable_self')
      return t('managers.errSelf')
    if (err.code === 'last_admin') return t('managers.errLastAdmin')
    if (err.code === 'has_history') return t('managers.errHasHistory')
    if (err.message) return err.message
  }
  return t('common.error.body')
}

function RemoveManagerModal({
  manager,
  onClose,
  onChanged,
}: {
  manager: ManagerView
  onClose: () => void
  onChanged: () => void
}) {
  const t = useT()
  const { toast } = useToast()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // has_history managers cannot be hard-deleted; offer disable as the way out.
  const [offerDisable, setOfferDisable] = useState(false)

  async function remove() {
    setBusy(true)
    setError(null)
    try {
      await deleteManager(manager.id)
      toast(t('managers.removed'), 'ok')
      onChanged()
    } catch (err) {
      setError(removalErrorMessage(t, err))
      if (err instanceof ApiError && err.code === 'has_history') setOfferDisable(true)
    } finally {
      setBusy(false)
    }
  }
  async function disable() {
    setBusy(true)
    setError(null)
    try {
      await updateManager(manager.id, { disabled: true })
      toast(t('managers.disabledDone'), 'ok')
      onChanged()
    } catch (err) {
      setError(removalErrorMessage(t, err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open
      onOpenChange={busy ? () => {} : (o) => !o && onClose()}
      title={t('managers.removeTitle')}
    >
      <div className="space-y-4">
        <p className="text-sm">{t('managers.removeBody', { name: manager.username })}</p>
        {error ? <p className="text-sm text-danger">{error}</p> : null}
        <div className="flex justify-end gap-2">
          <Button variant="ghost" disabled={busy} onClick={onClose}>
            {t('ui.cancel')}
          </Button>
          {offerDisable ? (
            <Button variant="danger" disabled={busy} onClick={() => void disable()}>
              {busy ? t('ui.working') : t('managers.disable')}
            </Button>
          ) : (
            <Button variant="danger" disabled={busy} onClick={() => void remove()}>
              {busy ? t('ui.working') : t('managers.remove')}
            </Button>
          )}
        </div>
      </div>
    </Modal>
  )
}

function BalanceCell({ managerId }: { managerId: string }) {
  const q = useAsync(() => getManagerBalance(managerId).catch(() => null), [managerId])
  if (!q.data) return <span className="text-ink-muted">—</span>
  return <IQDAmount amount={q.data.balance} currency={q.data.currency} />
}

function RowActions({
  manager,
  isSelf,
  canEdit,
  canDelete,
  canTopup,
  onEdit,
  onTopup,
  onAllowlist,
  onRemove,
  onChanged,
}: {
  manager: ManagerView
  isSelf: boolean
  canEdit: boolean
  canDelete: boolean
  canTopup: boolean
  onEdit: () => void
  onTopup: () => void
  onAllowlist: () => void
  onRemove: () => void
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
  async function setDisabled(disabled: boolean) {
    try {
      await updateManager(manager.id, { disabled })
      toast(disabled ? t('managers.disabledDone') : t('managers.enabledDone'), 'ok')
      onChanged()
    } catch (err) {
      toast(removalErrorMessage(t, err), 'danger')
    }
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
          {!isSelf ? (
            <Button size="sm" variant="ghost" onClick={() => void setDisabled(!manager.disabled)}>
              {manager.disabled ? t('managers.enable') : t('managers.disable')}
            </Button>
          ) : null}
        </>
      ) : null}
      {canDelete && !isSelf ? (
        <Button size="sm" variant="ghost" className="text-danger" onClick={onRemove}>
          {t('managers.remove')}
        </Button>
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
  const [fullName, setFullName] = useState(existing?.full_name ?? '')
  const [phone, setPhone] = useState(existing?.phone ?? '')
  const [email, setEmail] = useState(existing?.email ?? '')
  const [address, setAddress] = useState(existing?.address ?? '')
  const [notes, setNotes] = useState(existing?.notes ?? '')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit() {
    setBusy(true)
    setError(null)
    // Profile fields are always sent: "" clears server-side (NULLIF).
    const profile = {
      full_name: fullName.trim(),
      phone: phone.trim(),
      email: email.trim(),
      address: address.trim(),
      notes: notes.trim(),
    }
    try {
      if (existing) {
        await updateManager(existing.id, {
          role,
          scoped,
          password: password || undefined,
          ...profile,
        })
      } else {
        await createManager({ username: username.trim(), password, role, scoped, ...profile })
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
        <Field label={t('managers.fullName')} htmlFor="mgr-fullname">
          <TextInput
            id="mgr-fullname"
            value={fullName}
            onChange={(e) => setFullName(e.target.value)}
            autoComplete="off"
          />
        </Field>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={t('managers.phone')} htmlFor="mgr-phone">
            <TextInput
              id="mgr-phone"
              value={phone}
              onChange={(e) => setPhone(e.target.value)}
              dir="ltr"
              inputMode="tel"
            />
          </Field>
          <Field label={t('managers.email')} htmlFor="mgr-email">
            <TextInput
              id="mgr-email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              dir="ltr"
              inputMode="email"
            />
          </Field>
        </div>
        <Field label={t('managers.address')} htmlFor="mgr-address">
          <TextInput
            id="mgr-address"
            value={address}
            onChange={(e) => setAddress(e.target.value)}
          />
        </Field>
        <Field label={t('managers.notes')} htmlFor="mgr-notes">
          <TextInput id="mgr-notes" value={notes} onChange={(e) => setNotes(e.target.value)} />
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
  const { data: currencies } = useAsync(() => listCurrencies(), [])
  const [amount, setAmount] = useState(0)
  const [currency, setCurrency] = useState('IQD')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit() {
    setBusy(true)
    try {
      const res = await topupManager(manager.id, {
        amount,
        currency,
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
        <Field label={t('managers.topupCurrency')} htmlFor="topup-currency">
          <Select
            id="topup-currency"
            value={currency}
            onChange={(e) => setCurrency(e.target.value)}
          >
            {(currencies?.items ?? [{ code: 'IQD' }]).map((c) => (
              <option key={c.code} value={c.code}>
                {c.code}
              </option>
            ))}
          </Select>
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
