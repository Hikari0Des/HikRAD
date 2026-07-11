import { useMemo, useState } from 'react'

import { ErrorState, LoadingState, useT } from '@hikrad/shared'

import { ApiError } from '../../api/client'
import {
  createRole,
  deleteRole,
  getPermissionCatalog,
  listRoles,
  updateRole,
  type PermissionGroup,
  type Role,
} from '../../api/security'
import { useAuth } from '../../auth/AuthContext'
import {
  PERM_MANAGERS_CREATE,
  PERM_MANAGERS_DELETE,
  PERM_MANAGERS_EDIT,
} from '../../auth/permissions'
import { Button } from '../../components/Button'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { Checkbox, Field, TextInput } from '../../components/form'
import { Modal } from '../../components/Modal'
import { PageHeader } from '../../components/PageHeader'
import { useToast } from '../../components/Toast'
import { useAsync } from '../../hooks/useAsync'
import { RoleMatrix } from './RoleMatrix'

/** Roles matrix editor (FR-27.1): manage editable roles and their permissions. */
export function RolesPage() {
  const t = useT()
  const { can } = useAuth()
  const rolesQ = useAsync(() => listRoles(), [])
  const catalogQ = useAsync(() => getPermissionCatalog(), [])
  const [editing, setEditing] = useState<Role | null | 'new'>(null)
  const [deleting, setDeleting] = useState<Role | null>(null)
  const { toast } = useToast()

  if (rolesQ.error) return <ErrorState onRetry={rolesQ.reload} />
  if (rolesQ.loading || !rolesQ.data || catalogQ.loading) return <LoadingState />

  const catalog = catalogQ.data?.modules ?? []

  async function doDelete() {
    if (!deleting) return
    try {
      await deleteRole(deleting.id)
      toast(t('roles.deleted'), 'ok')
      rolesQ.reload()
    } catch (err) {
      toast(err instanceof ApiError ? err.message : t('common.error.body'), 'danger')
    }
  }

  return (
    <section>
      <PageHeader
        title={t('roles.title')}
        subtitle={t('roles.subtitle')}
        actions={
          can(PERM_MANAGERS_CREATE) ? (
            <Button size="sm" onClick={() => setEditing('new')}>
              {t('roles.create')}
            </Button>
          ) : null
        }
      />

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {rolesQ.data.items.map((r) => (
          <div key={r.id} className="rounded-md border border-surface-sunken bg-surface-raised p-4">
            <div className="flex items-start justify-between">
              <div>
                <h2 className="font-semibold">{r.name}</h2>
                <p className="mt-0.5 text-xs text-ink-muted">{r.description || '—'}</p>
              </div>
              {r.is_builtin ? (
                <span className="rounded bg-surface-sunken px-1.5 py-0.5 text-xs text-ink-muted">
                  {t('roles.builtin')}
                </span>
              ) : null}
            </div>
            <p className="mt-2 text-xs text-ink-muted">
              {t('roles.memberCount', { n: r.member_count })} ·{' '}
              {t('roles.permCount', { n: r.permissions.length })}
            </p>
            {r.require_2fa ? (
              <p className="mt-1 text-xs text-brand-strong">{t('roles.requires2fa')}</p>
            ) : null}
            <div className="mt-3 flex gap-1">
              {can(PERM_MANAGERS_EDIT) ? (
                <Button size="sm" variant="ghost" onClick={() => setEditing(r)}>
                  {t('ui.edit')}
                </Button>
              ) : null}
              {can(PERM_MANAGERS_DELETE) && !r.is_builtin ? (
                <Button size="sm" variant="ghost" onClick={() => setDeleting(r)}>
                  {t('ui.delete')}
                </Button>
              ) : null}
            </div>
          </div>
        ))}
      </div>

      {editing !== null ? (
        <RoleFormModal
          existing={editing === 'new' ? null : editing}
          catalog={catalog}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            rolesQ.reload()
          }}
        />
      ) : null}

      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(o) => !o && setDeleting(null)}
        title={t('roles.deleteTitle')}
        body={t('roles.deleteBody', { name: deleting?.name ?? '' })}
        confirmLabel={t('ui.delete')}
        destructive
        onConfirm={doDelete}
      />
    </section>
  )
}

function RoleFormModal({
  existing,
  catalog,
  onClose,
  onSaved,
}: {
  existing: Role | null
  catalog: PermissionGroup[]
  onClose: () => void
  onSaved: () => void
}) {
  const t = useT()
  const { toast } = useToast()
  const [name, setName] = useState(existing?.name ?? '')
  const [description, setDescription] = useState(existing?.description ?? '')
  const [require2fa, setRequire2fa] = useState(existing?.require_2fa ?? false)
  const [perms, setPerms] = useState<Set<string>>(() => new Set(existing?.permissions ?? []))
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const readOnly = existing?.is_builtin ?? false

  const permsArray = useMemo(() => [...perms], [perms])

  async function submit() {
    setBusy(true)
    setError(null)
    try {
      const body = {
        name: name.trim(),
        description: description.trim(),
        require_2fa: require2fa,
        permissions: permsArray,
      }
      if (existing) await updateRole(existing.id, body)
      else await createRole(body)
      toast(t('roles.saved'), 'ok')
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
      title={existing ? t('roles.editTitle') : t('roles.createTitle')}
      size="lg"
    >
      <form
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
        className="space-y-4"
      >
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label={t('roles.name')} htmlFor="role-name">
            <TextInput
              id="role-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              disabled={readOnly}
              required
            />
          </Field>
          <Field label={t('roles.description')} htmlFor="role-desc">
            <TextInput
              id="role-desc"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              disabled={readOnly}
            />
          </Field>
        </div>
        <Checkbox
          label={t('roles.require2faLabel')}
          checked={require2fa}
          disabled={readOnly}
          onChange={(e) => setRequire2fa(e.target.checked)}
        />
        <div>
          <p className="mb-2 text-sm font-medium">{t('roles.permissions')}</p>
          <RoleMatrix catalog={catalog} value={perms} onChange={setPerms} disabled={readOnly} />
        </div>
        {error ? <p className="text-sm text-danger">{error}</p> : null}
        <div className="flex justify-end gap-2 border-t border-surface-sunken pt-3">
          <Button variant="ghost" disabled={busy} onClick={onClose}>
            {t('ui.cancel')}
          </Button>
          {!readOnly ? (
            <Button type="submit" disabled={busy}>
              {busy ? t('ui.working') : t('ui.save')}
            </Button>
          ) : null}
        </div>
      </form>
    </Modal>
  )
}
