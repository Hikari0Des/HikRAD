import { useEffect, useRef, useState } from 'react'

import { Ltr, useT } from '@hikrad/shared'

import { exportCsv, getBulkJob, startBulk } from '../../api/subscribers'
import { SERVICE_TYPES } from '../../api/types'
import type { BulkAction, BulkFilter, BulkJob, Profile, ServiceType } from '../../api/types'
import type { ManagerView } from '../../api/managers'
import { useAuth } from '../../auth/AuthContext'
import { PERM_EXPORT } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { Modal } from '../../components/Modal'
import { Field, Select, TextInput } from '../../components/form'
import { useToast } from '../../components/Toast'

/**
 * Bulk-action bar (FR-4).
 *
 * Two target modes, and the bar always says which one is live:
 *
 *  - Rows ticked → the action runs against exactly those ids.
 *  - Nothing ticked → it runs against the *server-side filter*, which is the
 *    whole match set, not just the visible page.
 *
 * The distinction matters enough to state in words above the buttons: "disable"
 * meaning three rows and "disable" meaning every expired subscriber look
 * identical once clicked, and only one of them is undoable by hand.
 *
 * Mutations go through D's async job endpoint (poll for progress + per-row
 * failures); export is a synchronous CSV download needing the `export`
 * permission.
 */
export function BulkBar({
  filter,
  selectedIds,
  profiles,
  managers,
  onDone,
}: {
  filter: BulkFilter
  selectedIds: string[]
  profiles: Profile[]
  managers: ManagerView[]
  onDone: () => void
}) {
  const t = useT()
  const { can } = useAuth()
  const { toast } = useToast()
  const canEdit = can('subscribers.edit')
  const canDelete = can('subscribers.delete')
  const hasSelection = selectedIds.length > 0

  const [job, setJob] = useState<BulkJob | null>(null)
  const [prompt, setPrompt] = useState<BulkAction | null>(null)
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)

  useEffect(() => () => stopPolling(), [])

  function stopPolling() {
    if (pollRef.current) {
      clearInterval(pollRef.current)
      pollRef.current = null
    }
  }

  async function run(action: BulkAction, params?: Record<string, unknown>) {
    setPrompt(null)
    try {
      // Send the selection when there is one; the backend then ignores the
      // filter entirely.
      const initial = await startBulk({
        filter,
        subscriber_ids: hasSelection ? selectedIds : undefined,
        action,
        params,
      })
      setJob(initial)
      stopPolling()
      pollRef.current = setInterval(async () => {
        try {
          const next = await getBulkJob(initial.id)
          setJob(next)
          if (next.status === 'completed') {
            stopPolling()
            onDone()
            toast(
              t('bulk.done', { succeeded: next.succeeded, failed: next.failed }),
              next.failed > 0 ? 'danger' : 'ok',
            )
          }
        } catch {
          stopPolling()
        }
      }, 700)
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    }
  }

  async function doExport() {
    try {
      const { url, filename } = await exportCsv(filter, hasSelection ? selectedIds : undefined)
      const a = document.createElement('a')
      a.href = url
      a.download = filename
      document.body.appendChild(a)
      a.click()
      a.remove()
      URL.revokeObjectURL(url)
      toast(t('bulk.exported'), 'ok')
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    }
  }

  return (
    <div className="rounded-md border border-surface-sunken bg-surface-raised p-3">
      {/* Always say what the buttons will hit. "Disable" over 3 ticked rows and
          "Disable" over every expired subscriber are one click apart. */}
      <p className="mb-2 text-xs text-ink-muted">
        {hasSelection
          ? t('bulk.scopeSelected', { count: selectedIds.length })
          : t('bulk.scopeHint')}
      </p>
      <div className="flex flex-wrap gap-2">
        {canEdit && (
          <>
            <Button size="sm" variant="secondary" onClick={() => run('enable')}>
              {t('bulk.enable')}
            </Button>
            <Button size="sm" variant="secondary" onClick={() => setPrompt('disable')}>
              {t('bulk.disable')}
            </Button>
            <Button size="sm" variant="secondary" onClick={() => setPrompt('change_profile')}>
              {t('bulk.changeProfile')}
            </Button>
            <Button size="sm" variant="secondary" onClick={() => setPrompt('extend_expiry')}>
              {t('bulk.extendExpiry')}
            </Button>
            <Button size="sm" variant="secondary" onClick={() => setPrompt('set_service_type')}>
              {t('bulk.setServiceType')}
            </Button>
            {managers.length > 0 && (
              <Button size="sm" variant="secondary" onClick={() => setPrompt('move_owner')}>
                {t('bulk.moveOwner')}
              </Button>
            )}
          </>
        )}
        {can(PERM_EXPORT) && (
          <Button size="sm" variant="secondary" onClick={doExport}>
            {t('bulk.export')}
          </Button>
        )}
        {canDelete && (
          <Button size="sm" variant="danger" onClick={() => setPrompt('delete')}>
            {t('bulk.delete')}
          </Button>
        )}
      </div>

      {job && (
        <div className="mt-3 rounded border border-surface-sunken p-3 text-sm">
          <div className="flex items-center justify-between">
            <span className="font-medium">{t(`bulk.action.${job.action}`)}</span>
            <span className="text-ink-muted">
              {t('bulk.progress', { done: job.done, total: job.total })}
            </span>
          </div>
          <div className="mt-2 h-1.5 overflow-hidden rounded-full bg-surface-sunken">
            <div
              className="h-full rounded-full bg-brand transition-all"
              style={{ inlineSize: `${job.total ? (job.done / job.total) * 100 : 0}%` }}
            />
          </div>
          {job.failures.length > 0 && (
            <div className="mt-2">
              <p className="text-xs font-medium text-danger">
                {t('bulk.failedCount', { count: job.failed })}
              </p>
              <ul className="mt-1 max-h-32 space-y-0.5 overflow-y-auto text-xs text-ink-muted">
                {job.failures.map((f) => (
                  <li key={f.subscriber_id}>
                    <Ltr className="font-medium">{f.username}</Ltr> — {f.error}
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
      )}

      <BulkPrompt
        action={prompt}
        profiles={profiles}
        managers={managers}
        onCancel={() => setPrompt(null)}
        onSubmit={run}
      />
    </div>
  )
}

/** Parameter prompt for the bulk actions that need input. */
function BulkPrompt({
  action,
  profiles,
  managers,
  onCancel,
  onSubmit,
}: {
  action: BulkAction | null
  profiles: Profile[]
  managers: ManagerView[]
  onCancel: () => void
  onSubmit: (action: BulkAction, params: Record<string, unknown>) => void
}) {
  const t = useT()
  const [profileId, setProfileId] = useState('')
  const [days, setDays] = useState('30')
  const [ownerId, setOwnerId] = useState('')
  const [reason, setReason] = useState('')
  const [serviceType, setServiceType] = useState<ServiceType>('pppoe')

  if (!action || action === 'enable' || action === 'export') return null

  const titles: Record<string, string> = {
    disable: t('bulk.disable'),
    change_profile: t('bulk.changeProfile'),
    extend_expiry: t('bulk.extendExpiry'),
    move_owner: t('bulk.moveOwner'),
    set_service_type: t('bulk.setServiceType'),
    delete: t('bulk.delete'),
  }

  function submit() {
    switch (action) {
      case 'delete':
        onSubmit('delete', {})
        break
      case 'disable':
        onSubmit('disable', { disabled_reason: reason })
        break
      case 'change_profile':
        onSubmit('change_profile', { profile_id: profileId })
        break
      case 'extend_expiry':
        onSubmit('extend_expiry', { days: Number(days) })
        break
      case 'move_owner':
        onSubmit('move_owner', { owner_manager_id: ownerId })
        break
      case 'set_service_type':
        onSubmit('set_service_type', { service_type: serviceType })
        break
    }
  }

  return (
    <Modal open onOpenChange={(o) => !o && onCancel()} title={titles[action] ?? ''}>
      <div className="space-y-3">
        {action === 'delete' && (
          // Deletion is the one bulk action nothing undoes, so it says so and
          // offers no parameters to distract from the decision. The backend
          // refuses any subscriber with billing history and reports them as
          // per-row failures, which is stated here rather than discovered.
          <div className="space-y-2 text-sm">
            <p className="font-medium text-danger">{t('bulk.deleteWarning')}</p>
            <p className="text-ink-muted">{t('bulk.deleteBillingNote')}</p>
          </div>
        )}
        {action === 'disable' && (
          <Field label={t('bulk.disabledReason')}>
            <TextInput value={reason} onChange={(e) => setReason(e.target.value)} />
          </Field>
        )}
        {action === 'change_profile' && (
          <Field label={t('subscriber.profile')}>
            <Select value={profileId} onChange={(e) => setProfileId(e.target.value)}>
              <option value="">{t('ui.choose')}</option>
              {profiles.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </Select>
          </Field>
        )}
        {action === 'extend_expiry' && (
          <Field label={t('bulk.days')}>
            <TextInput
              type="number"
              value={days}
              onChange={(e) => setDays(e.target.value)}
              dir="ltr"
            />
          </Field>
        )}
        {action === 'move_owner' && (
          <Field label={t('subscriber.owner')}>
            <Select value={ownerId} onChange={(e) => setOwnerId(e.target.value)}>
              <option value="">{t('ui.choose')}</option>
              {managers.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.username}
                </option>
              ))}
            </Select>
          </Field>
        )}
        {action === 'set_service_type' && (
          <Field label={t('subscriber.serviceType')}>
            <Select
              value={serviceType}
              onChange={(e) => setServiceType(e.target.value as ServiceType)}
            >
              {SERVICE_TYPES.map((v) => (
                <option key={v} value={v}>
                  {t(`serviceType.${v}`)}
                </option>
              ))}
            </Select>
          </Field>
        )}
        <div className="flex justify-end gap-2 pt-2">
          <Button variant="ghost" onClick={onCancel}>
            {t('ui.cancel')}
          </Button>
          <Button onClick={submit}>{t('ui.apply')}</Button>
        </div>
      </div>
    </Modal>
  )
}
