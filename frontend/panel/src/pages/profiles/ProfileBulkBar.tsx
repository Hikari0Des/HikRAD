import { useEffect, useRef, useState } from 'react'

import { useT } from '@hikrad/shared'

import { exportProfilesCsv, getProfileBulkJob, startProfileBulk } from '../../api/profiles'
import type {
  ExpiryBehavior,
  Pool,
  ProfileBulkAction,
  ProfileBulkFilter,
  ProfileBulkJob,
  QuotaBehavior,
} from '../../api/types'
import { useAuth } from '../../auth/AuthContext'
import { PERM_EXPORT } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { Modal } from '../../components/Modal'
import { Field, Select, TextInput } from '../../components/form'
import { useToast } from '../../components/Toast'

/**
 * Bulk-action bar for the plan catalogue (FR-111.2) — the Subscribers page's
 * BulkBar, ported to profiles. Two target modes, and the bar always says which
 * is live: rows ticked → the action runs against exactly those ids; nothing
 * ticked → it runs against the server-side filter (the whole match set).
 *
 * Unlike subscribers there is no manager scope — profiles are instance-global —
 * so "every matching plan" really is every plan the filter matches, full stop.
 */
export function ProfileBulkBar({
  filter,
  selectedIds,
  pools,
  onDone,
}: {
  filter: ProfileBulkFilter
  selectedIds: string[]
  pools: Pool[]
  onDone: () => void
}) {
  const t = useT()
  const { can } = useAuth()
  const { toast } = useToast()
  const canEdit = can('profiles.edit')
  const canDelete = can('profiles.delete')
  const hasSelection = selectedIds.length > 0

  const [job, setJob] = useState<ProfileBulkJob | null>(null)
  const [prompt, setPrompt] = useState<ProfileBulkAction | null>(null)
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)

  useEffect(() => () => stopPolling(), [])

  function stopPolling() {
    if (pollRef.current) {
      clearInterval(pollRef.current)
      pollRef.current = null
    }
  }

  async function run(action: ProfileBulkAction, params?: Record<string, unknown>) {
    setPrompt(null)
    try {
      const initial = await startProfileBulk({
        filter,
        profile_ids: hasSelection ? selectedIds : undefined,
        action,
        params,
      })
      setJob(initial)
      stopPolling()
      pollRef.current = setInterval(async () => {
        try {
          const next = await getProfileBulkJob(initial.id)
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
      const { url, filename } = await exportProfilesCsv(
        filter,
        hasSelection ? selectedIds : undefined,
      )
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
    <div className="rounded-md border border-border bg-surface-raised p-3">
      <p className="mb-2 text-xs text-ink-muted">
        {hasSelection
          ? t('bulk.scopeSelected', { count: selectedIds.length })
          : t('profiles.bulk.scopeHint')}
      </p>
      <div className="flex flex-wrap gap-2">
        {canEdit && (
          <>
            <Button size="sm" variant="secondary" onClick={() => run('archive')}>
              {t('profiles.bulk.archive')}
            </Button>
            <Button size="sm" variant="secondary" onClick={() => run('unarchive')}>
              {t('profiles.bulk.unarchive')}
            </Button>
            <Button size="sm" variant="secondary" onClick={() => setPrompt('change_pool')}>
              {t('profiles.bulk.changePool')}
            </Button>
            <Button size="sm" variant="secondary" onClick={() => setPrompt('set_address_list')}>
              {t('profiles.bulk.setAddressList')}
            </Button>
            <Button size="sm" variant="secondary" onClick={() => setPrompt('set_expiry_behavior')}>
              {t('profiles.bulk.setExpiryBehavior')}
            </Button>
            <Button size="sm" variant="secondary" onClick={() => setPrompt('set_quota_behavior')}>
              {t('profiles.bulk.setQuotaBehavior')}
            </Button>
          </>
        )}
        {can(PERM_EXPORT) && (
          <Button size="sm" variant="secondary" onClick={doExport}>
            {t('bulk.export')}
          </Button>
        )}
        {canDelete && (
          <Button size="sm" variant="danger" onClick={() => setPrompt('delete')}>
            {t('profiles.bulk.delete')}
          </Button>
        )}
      </div>

      {job && (
        <div className="mt-3 rounded border border-border p-3 text-sm">
          <div className="flex items-center justify-between">
            <span className="font-medium">{t(`profiles.bulk.action.${job.action}`)}</span>
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
                  <li key={f.profile_id}>
                    <span className="font-medium">{f.name}</span> — {f.error}
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
      )}

      <ProfileBulkPrompt
        action={prompt}
        pools={pools}
        onCancel={() => setPrompt(null)}
        onSubmit={run}
      />
    </div>
  )
}

const EXPIRY: ExpiryBehavior[] = ['block', 'expired_pool']
// throttle is deliberately absent: it needs a per-plan rate a bulk verb can't
// supply, and the backend refuses it — so the picker never offers it.
const QUOTA_BEH: QuotaBehavior[] = ['block', 'expired_pool']

function ProfileBulkPrompt({
  action,
  pools,
  onCancel,
  onSubmit,
}: {
  action: ProfileBulkAction | null
  pools: Pool[]
  onCancel: () => void
  onSubmit: (action: ProfileBulkAction, params: Record<string, unknown>) => void
}) {
  const t = useT()
  const [poolId, setPoolId] = useState('')
  const [addressList, setAddressList] = useState('')
  const [expiryBehavior, setExpiryBehavior] = useState<ExpiryBehavior>('block')
  const [quotaBehavior, setQuotaBehavior] = useState<QuotaBehavior>('block')

  if (!action || action === 'archive' || action === 'unarchive' || action === 'export') return null

  const titles: Record<string, string> = {
    change_pool: t('profiles.bulk.changePool'),
    set_address_list: t('profiles.bulk.setAddressList'),
    set_expiry_behavior: t('profiles.bulk.setExpiryBehavior'),
    set_quota_behavior: t('profiles.bulk.setQuotaBehavior'),
    delete: t('profiles.bulk.delete'),
  }

  function submit() {
    switch (action) {
      case 'delete':
        onSubmit('delete', {})
        break
      case 'change_pool':
        onSubmit('change_pool', { pool_id: poolId })
        break
      case 'set_address_list':
        onSubmit('set_address_list', { address_list: addressList })
        break
      case 'set_expiry_behavior':
        onSubmit('set_expiry_behavior', { expiry_behavior: expiryBehavior })
        break
      case 'set_quota_behavior':
        onSubmit('set_quota_behavior', { quota_behavior: quotaBehavior })
        break
    }
  }

  return (
    <Modal open onOpenChange={(o) => !o && onCancel()} title={titles[action] ?? ''}>
      <div className="space-y-3">
        {action === 'delete' && (
          <div className="space-y-2 text-sm">
            <p className="font-medium text-danger">{t('profiles.bulk.deleteWarning')}</p>
            <p className="text-ink-muted">{t('profiles.bulk.deleteInUseNote')}</p>
          </div>
        )}
        {action === 'change_pool' && (
          <Field label={t('profiles.pool')} htmlFor="bulk-pool">
            <Select id="bulk-pool" value={poolId} onChange={(e) => setPoolId(e.target.value)}>
              <option value="">{t('ui.none')}</option>
              {pools.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                  {p.management_mode === 'router' ? ` (${t('pools.mode.router')})` : ''}
                </option>
              ))}
            </Select>
          </Field>
        )}
        {action === 'set_address_list' && (
          <Field label={t('profiles.addressList')} htmlFor="bulk-address-list">
            <TextInput
              id="bulk-address-list"
              dir="ltr"
              value={addressList}
              onChange={(e) => setAddressList(e.target.value)}
            />
          </Field>
        )}
        {action === 'set_expiry_behavior' && (
          <Field label={t('profiles.expiryBehavior')} htmlFor="bulk-expiry">
            <Select
              id="bulk-expiry"
              value={expiryBehavior}
              onChange={(e) => setExpiryBehavior(e.target.value as ExpiryBehavior)}
            >
              {EXPIRY.map((b) => (
                <option key={b} value={b}>
                  {t(`profiles.expiry.${b}`)}
                </option>
              ))}
            </Select>
          </Field>
        )}
        {action === 'set_quota_behavior' && (
          <Field
            label={t('profiles.quotaBehavior')}
            hint={t('profiles.bulk.throttleExcluded')}
            htmlFor="bulk-quota"
          >
            <Select
              id="bulk-quota"
              value={quotaBehavior}
              onChange={(e) => setQuotaBehavior(e.target.value as QuotaBehavior)}
            >
              {QUOTA_BEH.map((b) => (
                <option key={b} value={b}>
                  {t(`profiles.quotaBeh.${b}`)}
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
