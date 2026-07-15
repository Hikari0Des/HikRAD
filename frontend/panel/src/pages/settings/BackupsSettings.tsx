import { LoadingState, useFormatters, useT } from '@hikrad/shared'

import { listBackups } from '../../api/setup'
import { useAuth } from '../../auth/AuthContext'
import { PERM_SETTINGS_EDIT } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { Field, TextInput } from '../../components/form'
import { useAsync } from '../../hooks/useAsync'
import { useSettingsGroup } from './useSettingsGroup'

const STALE_HOURS = 30

/** Backup schedule/retention/path (FR-51) + last-backup-age staleness warning. */
export function BackupsSettings() {
  const t = useT()
  const { formatDate } = useFormatters()
  const { can } = useAuth()
  const canEdit = can(PERM_SETTINGS_EDIT)
  const g = useSettingsGroup('backups')
  const runs = useAsync(() => listBackups(), [])

  if (!g.loaded) return <LoadingState />

  const v = g.values as { schedule_hour?: number; retention_count?: number; path?: string }
  const lastRun = runs.data?.items[0]
  const hoursAgo = lastRun
    ? (Date.now() - new Date(lastRun.finished_at ?? lastRun.started_at).getTime()) / 3_600_000
    : null
  const stale = hoursAgo !== null && hoursAgo > STALE_HOURS

  async function submit() {
    await g.save(
      {
        schedule_hour: v.schedule_hour ?? 3,
        retention_count: v.retention_count ?? 14,
        path: v.path ?? '',
      },
      t('settings.saved'),
      t('common.error.body'),
    )
  }

  return (
    <div className="max-w-md space-y-4">
      {lastRun ? (
        <div
          className={`rounded-md p-3 text-sm ${stale ? 'bg-warn/10 text-warn' : 'bg-ok/10 text-ok'}`}
        >
          {stale ? t('settings.backups.stale') : t('settings.backups.ok')}
          {' — '}
          {t('settings.backups.lastRun', {
            at: formatDate(lastRun.finished_at ?? lastRun.started_at),
          })}
        </div>
      ) : (
        <div className="rounded-md bg-warn/10 p-3 text-sm text-warn">
          {t('settings.backups.never')}
        </div>
      )}
      <Field
        label={t('settings.backups.scheduleHour')}
        hint={t('settings.backups.scheduleHourHint')}
        error={g.errors.schedule_hour}
      >
        <TextInput
          type="number"
          dir="ltr"
          min={0}
          max={23}
          disabled={!canEdit}
          value={v.schedule_hour ?? 3}
          onChange={(e) => g.setField('schedule_hour', Number(e.target.value))}
        />
      </Field>
      <Field label={t('settings.backups.retentionCount')} error={g.errors.retention_count}>
        <TextInput
          type="number"
          dir="ltr"
          disabled={!canEdit}
          value={v.retention_count ?? 14}
          onChange={(e) => g.setField('retention_count', Number(e.target.value))}
        />
      </Field>
      <Field label={t('settings.backups.path')} error={g.errors.path}>
        <TextInput
          dir="ltr"
          disabled={!canEdit}
          value={v.path ?? ''}
          onChange={(e) => g.setField('path', e.target.value)}
        />
      </Field>
      {canEdit ? (
        <Button disabled={g.saving} onClick={() => void submit()}>
          {g.saving ? t('ui.working') : t('ui.save')}
        </Button>
      ) : null}
    </div>
  )
}
