import { useState } from 'react'

import { ErrorState, LoadingState, Ltr, StatCard, useT } from '@hikrad/shared'

import { applyUpdate, checkUpdate, getUpdateInfo } from '../../../api/nasUpdate'
import { useAuth } from '../../../auth/AuthContext'
import { PERM_NAS_UPDATE } from '../../../auth/permissions'
import { Button } from '../../../components/Button'
import { Modal } from '../../../components/Modal'
import { useToast } from '../../../components/Toast'
import { useAsync } from '../../../hooks/useAsync'
import { routerErrorMessage } from '../../../lib/routerErrors'

/**
 * FR-147 RouterOS Update tab. Read and report by default; apply is
 * double-confirmed (same two-step pattern as General's Reboot) and gated on
 * its own `nas.update` permission — separate from both `nas.edit` and
 * `nas.reboot`, since installing firmware is neither. Live read, no
 * device_config_cache envelope (same posture as the Internet tab).
 */
export function UpdateTab({ nasId }: { nasId: string }) {
  const t = useT()
  const { can } = useAuth()
  const { toast } = useToast()
  const { data, error, loading, reload } = useAsync(() => getUpdateInfo(nasId), [nasId])

  const [checking, setChecking] = useState(false)
  const [applyStage, setApplyStage] = useState<0 | 1 | 2>(0)
  const [applying, setApplying] = useState(false)

  async function runCheck() {
    setChecking(true)
    try {
      await checkUpdate(nasId)
      reload()
    } catch (err) {
      toast(routerErrorMessage(err, t), 'danger')
    } finally {
      setChecking(false)
    }
  }

  async function runApply() {
    setApplying(true)
    try {
      await applyUpdate(nasId)
      toast(t('nas.update.installingSent'), 'ok')
      setApplyStage(0)
    } catch (err) {
      toast(routerErrorMessage(err, t), 'danger')
    } finally {
      setApplying(false)
    }
  }

  if (error) return <ErrorState onRetry={reload} />
  if (loading) return <LoadingState />
  if (!data) return null

  const canUpdate = can(PERM_NAS_UPDATE)

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <StatCard label={t('nas.update.channel')} value={data.channel || '—'} />
        <StatCard
          label={t('nas.update.installed')}
          value={<Ltr>{data.installed_version || '—'}</Ltr>}
        />
        <StatCard
          label={t('nas.update.latest')}
          value={<Ltr>{data.latest_version || '—'}</Ltr>}
          hint={data.update_available ? t('nas.update.available') : t('nas.update.upToDate')}
        />
      </div>

      <div className="rounded-md border border-border bg-surface-raised p-4">
        <h2 className="mb-1 text-sm font-semibold">{t('nas.update.packagesTitle')}</h2>
        {data.packages && data.packages.length > 0 ? (
          <ul className="mt-2 space-y-1 text-sm">
            {data.packages.map((p) => (
              <li key={p.name} className="flex items-center justify-between gap-2">
                <Ltr>
                  <code className="text-xs">{p.name}</code>
                </Ltr>
                <span className="text-xs text-ink-muted">
                  <Ltr>{p.version}</Ltr>
                  {!p.enabled ? ` (${t('nas.update.disabled')})` : ''}
                </span>
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-sm text-ink-muted">{t('nas.update.noPackages')}</p>
        )}
      </div>

      {canUpdate ? (
        <div className="flex flex-wrap items-center gap-2">
          <Button size="sm" variant="secondary" disabled={checking} onClick={() => void runCheck()}>
            {checking ? t('ui.working') : t('nas.update.checkButton')}
          </Button>
          {data.update_available ? (
            <div className="rounded-md border border-danger/30 bg-danger/5 p-3">
              <p className="mb-2 text-xs text-ink-muted">{t('nas.update.rebootWarning')}</p>
              <Button variant="danger" size="sm" onClick={() => setApplyStage(1)}>
                {t('nas.update.applyButton')}
              </Button>
            </div>
          ) : null}
        </div>
      ) : null}

      <Modal
        open={applyStage === 1}
        onOpenChange={(o) => !o && setApplyStage(0)}
        title={t('nas.update.confirm1Title')}
      >
        <p className="text-sm text-ink-muted">{t('nas.update.confirm1Body')}</p>
        <div className="mt-6 flex justify-end gap-2">
          <Button variant="ghost" onClick={() => setApplyStage(0)}>
            {t('ui.cancel')}
          </Button>
          <Button variant="danger" onClick={() => setApplyStage(2)}>
            {t('ui.continue')}
          </Button>
        </div>
      </Modal>

      <Modal
        open={applyStage === 2}
        onOpenChange={(o) => !o && !applying && setApplyStage(0)}
        title={t('nas.update.confirm2Title')}
      >
        <p className="text-sm font-medium text-danger">{t('nas.update.confirm2Body')}</p>
        <div className="mt-6 flex justify-end gap-2">
          <Button variant="ghost" disabled={applying} onClick={() => setApplyStage(0)}>
            {t('ui.cancel')}
          </Button>
          <Button variant="danger" disabled={applying} onClick={() => void runApply()}>
            {applying ? t('ui.working') : t('nas.update.confirm2Button')}
          </Button>
        </div>
      </Modal>
    </div>
  )
}
