import { useEffect, useRef, useState } from 'react'

import { Ltr, useT } from '@hikrad/shared'

import { exportCsv, getBulkJob, startBulk } from '../../api/subscribers'
import type { BulkAction, BulkFilter, BulkJob, Profile } from '../../api/types'
import type { ManagerView } from '../../api/managers'
import { useAuth } from '../../auth/AuthContext'
import { PERM_EXPORT } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { Modal } from '../../components/Modal'
import { Field, Select, TextInput } from '../../components/form'
import { useToast } from '../../components/Toast'

/**
 * Bulk-action bar (FR-4). Actions run against the *server-side filter* (never
 * just the visible page) via D's async bulk endpoint: we submit the current
 * filter, then poll the job for progress and render per-row failures. Export is
 * synchronous (a CSV download) and needs the `export` permission.
 */
export function BulkBar({
  filter,
  profiles,
  managers,
  onDone,
}: {
  filter: BulkFilter
  profiles: Profile[]
  managers: ManagerView[]
  onDone: () => void
}) {
  const t = useT()
  const { can } = useAuth()
  const { toast } = useToast()
  const canEdit = can('subscribers.edit')

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
      const initial = await startBulk({ filter, action, params })
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
      const { url, filename } = await exportCsv(filter)
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
      <p className="mb-2 text-xs text-ink-muted">{t('bulk.scopeHint')}</p>
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
            <Button size="sm" variant="secondary" onClick={() => setPrompt('set_allow_hotspot')}>
              {t('bulk.setAllowHotspot')}
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
  const [allowHotspot, setAllowHotspot] = useState('true')

  if (!action || action === 'enable' || action === 'export') return null

  const titles: Record<string, string> = {
    disable: t('bulk.disable'),
    change_profile: t('bulk.changeProfile'),
    extend_expiry: t('bulk.extendExpiry'),
    move_owner: t('bulk.moveOwner'),
    set_allow_hotspot: t('bulk.setAllowHotspot'),
  }

  function submit() {
    switch (action) {
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
      case 'set_allow_hotspot':
        onSubmit('set_allow_hotspot', { allow_hotspot: allowHotspot === 'true' })
        break
    }
  }

  return (
    <Modal open onOpenChange={(o) => !o && onCancel()} title={titles[action] ?? ''}>
      <div className="space-y-3">
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
        {action === 'set_allow_hotspot' && (
          <Field label={t('subscriber.allowHotspot')}>
            <Select value={allowHotspot} onChange={(e) => setAllowHotspot(e.target.value)}>
              <option value="true">{t('ui.yes')}</option>
              <option value="false">{t('ui.no')}</option>
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
