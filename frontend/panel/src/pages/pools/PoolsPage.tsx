import { useEffect, useState } from 'react'

import { ErrorState, LoadingState, useFormatters, useT } from '@hikrad/shared'

import { createPool, deletePool, listPools, updatePool } from '../../api/pools'
import { ApiError } from '../../api/client'
import type { Pool, PoolPurpose, PoolWrite } from '../../api/types'
import { useAuth } from '../../auth/AuthContext'
import { Button } from '../../components/Button'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { Modal } from '../../components/Modal'
import { PageHeader } from '../../components/PageHeader'
import { Field, Select, Textarea, TextInput } from '../../components/form'
import { useToast } from '../../components/Toast'
import { useAsync } from '../../hooks/useAsync'

const PURPOSES: PoolPurpose[] = ['active', 'expired', 'static']

/** IP pool management (FR-16): utilization bars, CRUD, purpose designation. */
export function PoolsPage() {
  const t = useT()
  const { can } = useAuth()
  const { toast } = useToast()
  const { data, error, loading, reload } = useAsync(() => listPools(), [])

  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<Pool | undefined>(undefined)
  const [deleteTarget, setDeleteTarget] = useState<Pool | null>(null)

  const canCreate = can('pools.create')
  const canEdit = can('pools.edit')
  const canDelete = can('pools.delete')

  async function doDelete(p: Pool) {
    try {
      await deletePool(p.id)
      toast(t('pools.deleted'), 'ok')
      reload()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    }
  }

  return (
    <section>
      <PageHeader
        title={t('nav.pools')}
        subtitle={t('pools.subtitle')}
        actions={
          canCreate && (
            <Button
              size="sm"
              onClick={() => {
                setEditing(undefined)
                setFormOpen(true)
              }}
            >
              {t('pools.add')}
            </Button>
          )
        }
      />

      {/* FR-64.3, documented here because this screen is where the pilot's "no
          more free addresses in the pool" bug was invisible: a profile's pool
          is a PPPoE pool and is never sent on a hotspot login. */}
      <p className="mb-3 rounded-md border border-surface-sunken bg-surface-raised p-3 text-xs text-ink-muted">
        {t('pools.serviceNote')}
      </p>

      {error ? (
        <ErrorState onRetry={reload} />
      ) : loading ? (
        <LoadingState />
      ) : (data?.items.length ?? 0) === 0 ? (
        <p className="rounded-md border border-dashed border-surface-sunken p-10 text-center text-sm text-ink-muted">
          {t('pools.empty')}
        </p>
      ) : (
        <div className="grid gap-3 md:grid-cols-2">
          {data?.items.map((p) => (
            <PoolCard
              key={p.id}
              pool={p}
              canEdit={canEdit}
              canDelete={canDelete}
              onEdit={() => {
                setEditing(p)
                setFormOpen(true)
              }}
              onDelete={() => setDeleteTarget(p)}
            />
          ))}
        </div>
      )}

      <PoolFormModal
        open={formOpen}
        onOpenChange={setFormOpen}
        existing={editing}
        onSaved={() => {
          setFormOpen(false)
          reload()
        }}
      />

      <ConfirmDialog
        open={deleteTarget !== null}
        onOpenChange={(o) => !o && setDeleteTarget(null)}
        title={t('pools.deleteTitle')}
        body={t('pools.deleteBody')}
        confirmLabel={t('ui.delete')}
        destructive
        onConfirm={async () => {
          if (deleteTarget) await doDelete(deleteTarget)
        }}
      />
    </section>
  )
}

function PoolCard({
  pool,
  canEdit,
  canDelete,
  onEdit,
  onDelete,
}: {
  pool: Pool
  canEdit: boolean
  canDelete: boolean
  onEdit: () => void
  onDelete: () => void
}) {
  const t = useT()
  const { formatNumber } = useFormatters()
  const pct = Math.min(100, pool.util_percent)
  return (
    <div className="rounded-lg border border-surface-sunken bg-surface-raised p-4">
      <div className="flex items-start justify-between gap-2">
        <div>
          <h3 className="font-semibold">{pool.name}</h3>
          <span className="rounded bg-surface-sunken px-1.5 py-0.5 text-xs text-ink-muted">
            {t(`pools.purpose.${pool.purpose}`)}
          </span>
        </div>
        {pool.exhausted && (
          <span className="rounded bg-danger/10 px-2 py-0.5 text-xs text-danger">
            {t('pools.exhausted')}
          </span>
        )}
      </div>

      <div className="mt-3">
        <div className="h-2 overflow-hidden rounded-full bg-surface-sunken">
          <div
            className={`h-full rounded-full ${pool.exhausted ? 'bg-danger' : 'bg-brand'}`}
            style={{ inlineSize: `${pct}%` }}
            role="progressbar"
            aria-valuenow={pool.used}
            aria-valuemin={0}
            aria-valuemax={pool.size}
          />
        </div>
        <p className="mt-1 text-xs text-ink-muted">
          {t('pools.utilization', {
            used: formatNumber(pool.used),
            size: formatNumber(pool.size),
            pct: formatNumber(pool.util_percent, { maximumFractionDigits: 1 }),
          })}
        </p>
      </div>

      <p className="mt-2 truncate text-xs text-ink-muted" dir="ltr">
        {pool.ranges.join(', ')}
      </p>

      {(canEdit || canDelete) && (
        <div className="mt-3 flex gap-2">
          {canEdit && (
            <Button size="sm" variant="ghost" onClick={onEdit}>
              {t('ui.edit')}
            </Button>
          )}
          {canDelete && (
            <Button size="sm" variant="ghost" onClick={onDelete}>
              {t('ui.delete')}
            </Button>
          )}
        </div>
      )}
    </div>
  )
}

function PoolFormModal({
  open,
  onOpenChange,
  existing,
  onSaved,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  existing?: Pool
  onSaved: () => void
}) {
  const t = useT()
  const { toast } = useToast()
  const editing = Boolean(existing)
  const [name, setName] = useState('')
  const [ranges, setRanges] = useState('')
  const [purpose, setPurpose] = useState<PoolPurpose>('active')
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)

  const openKey = `${open}:${existing?.id ?? 'new'}`
  useEffect(() => {
    if (open) {
      setName(existing?.name ?? '')
      setRanges(existing?.ranges.join('\n') ?? '')
      setPurpose(existing?.purpose ?? 'active')
      setErrors({})
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [openKey])

  async function save() {
    setBusy(true)
    setErrors({})
    const parsed = ranges
      .split(/[\n,]/)
      .map((r) => r.trim())
      .filter(Boolean)
    const body: PoolWrite = { name, ranges: parsed, purpose }
    try {
      if (editing && existing) await updatePool(existing.id, body)
      else await createPool(body)
      toast(editing ? t('pools.saved') : t('pools.created'), 'ok')
      onSaved()
    } catch (err) {
      if (err instanceof ApiError && err.fieldErrors.length > 0) {
        const map: Record<string, string> = {}
        for (const fe of err.fieldErrors) map[fe.field] = fe.message
        setErrors(map)
      } else if (err instanceof ApiError && err.code === 'conflict') {
        setErrors({ name: t('pools.nameTaken') })
      } else {
        toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open={open}
      onOpenChange={busy ? () => {} : onOpenChange}
      title={editing ? t('pools.editTitle') : t('pools.createTitle')}
    >
      <div className="space-y-3">
        <Field label={t('pools.name')} error={errors.name}>
          <TextInput value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label={t('pools.purposeLabel')} hint={t('pools.purposeHint')}>
          <Select value={purpose} onChange={(e) => setPurpose(e.target.value as PoolPurpose)}>
            {PURPOSES.map((p) => (
              <option key={p} value={p}>
                {t(`pools.purpose.${p}`)}
              </option>
            ))}
          </Select>
        </Field>
        <Field label={t('pools.ranges')} error={errors.ranges} hint={t('pools.rangesHint')}>
          <Textarea
            rows={3}
            dir="ltr"
            value={ranges}
            onChange={(e) => setRanges(e.target.value)}
            placeholder="10.10.0.0/16&#10;192.168.5.10"
          />
        </Field>
        <div className="flex justify-end gap-2 pt-2">
          <Button variant="ghost" disabled={busy} onClick={() => onOpenChange(false)}>
            {t('ui.cancel')}
          </Button>
          <Button disabled={busy} onClick={save}>
            {busy ? t('ui.working') : t('ui.save')}
          </Button>
        </div>
      </div>
    </Modal>
  )
}
