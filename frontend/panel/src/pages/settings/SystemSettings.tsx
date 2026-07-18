import { useEffect, useRef, useState } from 'react'

import { ErrorState, LoadingState, Ltr, useT } from '@hikrad/shared'

import { getSystemVersion } from '../../api/setup'
import {
  checkForUpdate,
  getUpdateStatus,
  openUpdateStream,
  startUpdate,
  type UpdateCheckResult,
  type UpdateStreamHandle,
} from '../../api/updates'
import { useAsync } from '../../hooks/useAsync'
import { useToast } from '../../components/Toast'
import { useAuth } from '../../auth/AuthContext'
import { PERM_SYSTEM_UPDATE } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { ConfirmDialog } from '../../components/ConfirmDialog'

/**
 * Settings > System (item 1): shows the running version, the guided
 * `hikrad update` command (always available, works regardless of whether
 * the one-click path below is provisioned), and — when hikrad-updaterd is
 * provisioned and the caller holds `system.update` (v2 phase 7, FR-86/87) —
 * "Check for update" / "Update now" with live progress.
 */
export function SystemSettings() {
  const t = useT()
  const { toast } = useToast()
  const { can } = useAuth()
  const canUpdate = can(PERM_SYSTEM_UPDATE)
  const q = useAsync(getSystemVersion, [])

  if (q.error) return <ErrorState onRetry={q.reload} />
  if (q.loading || !q.data) return <LoadingState />
  const v = q.data

  async function copy(cmd: string) {
    try {
      await navigator.clipboard.writeText(cmd)
      toast(t('ui.copied'), 'ok')
    } catch {
      /* clipboard unavailable — the command is selectable text anyway */
    }
  }

  return (
    <div className="max-w-2xl space-y-6">
      <section className="rounded-lg border border-surface-sunken bg-surface-raised p-4">
        <h3 className="font-semibold">{t('settings.system.versionTitle')}</h3>
        <dl className="mt-3 space-y-1 text-sm">
          <div className="flex justify-between gap-2">
            <dt className="text-ink-muted">{t('settings.system.appVersion')}</dt>
            <dd>
              <Ltr>{v.app_version}</Ltr>
            </dd>
          </div>
          <div className="flex justify-between gap-2">
            <dt className="text-ink-muted">{t('settings.system.schemaVersion')}</dt>
            <dd>
              <Ltr>{String(v.schema_version)}</Ltr>
            </dd>
          </div>
          <div className="flex justify-between gap-2">
            <dt className="text-ink-muted">{t('settings.system.channel')}</dt>
            <dd>
              <Ltr>{v.channel}</Ltr>
            </dd>
          </div>
        </dl>
        {v.schema_dirty ? (
          <p role="alert" className="mt-3 rounded-md bg-danger/10 p-2 text-sm text-danger">
            {t('settings.system.schemaDirty')}
          </p>
        ) : null}
      </section>

      <section className="rounded-lg border border-surface-sunken bg-surface-raised p-4">
        <h3 className="font-semibold">{t('settings.system.updateTitle')}</h3>
        <p className="mt-2 text-sm text-ink-muted">{t('settings.system.updateIntro')}</p>
        <ol className="mt-3 list-decimal space-y-3 ps-5 text-sm">
          <li>{t('settings.system.updateStep1')}</li>
          <li>
            {t('settings.system.updateStep2')}
            <div className="mt-1 flex items-center gap-2">
              <code dir="ltr" className="rounded bg-surface-sunken px-2 py-1 font-mono">
                sudo hikrad update
              </code>
              <button
                type="button"
                onClick={() => copy('sudo hikrad update')}
                className="text-xs text-brand-strong hover:underline"
              >
                {t('ui.copy')}
              </button>
            </div>
          </li>
          <li>{t('settings.system.updateStep3')}</li>
        </ol>
        <p className="mt-3 text-sm text-ink-muted">{t('settings.system.updateSafety')}</p>
        <p className="mt-2 text-sm text-ink-muted">
          {t('settings.system.updateOffline')}{' '}
          <code dir="ltr" className="rounded bg-surface-sunken px-1.5 py-0.5 font-mono text-xs">
            sudo hikrad update --bundle hikrad-vX.Y.Z.tar
          </code>
        </p>
      </section>

      {canUpdate ? (
        <OneClickUpdateSection currentVersion={v.app_version} onUpdated={q.reload} />
      ) : null}
    </div>
  )
}

type Stage = 'idle' | 'backup' | 'apply' | 'health_check' | 'rolling_back' | 'done' | 'rolled_back'

interface Outcome {
  kind: 'done' | 'rolled_back'
  version?: string
}

/**
 * The one-click path (FR-87). Rendered only for a caller holding
 * `system.update` — the server re-checks this on every call regardless
 * (the panel gate is a convenience, never the security boundary, same
 * posture as every other permission-gated screen).
 */
function OneClickUpdateSection({
  currentVersion,
  onUpdated,
}: {
  currentVersion: string
  onUpdated: () => void
}) {
  const t = useT()
  const { toast } = useToast()

  const [checking, setChecking] = useState(false)
  const [checkResult, setCheckResult] = useState<UpdateCheckResult | null>(null)
  const [checkError, setCheckError] = useState<string | null>(null)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [stage, setStage] = useState<Stage | null>(null)
  const [outcome, setOutcome] = useState<Outcome | null>(null)
  const [reconnecting, setReconnecting] = useState(false)
  const streamRef = useRef<UpdateStreamHandle | null>(null)

  useEffect(() => {
    return () => streamRef.current?.close()
  }, [])

  // On mount: reconcile with whatever the daemon reports right now — the
  // page may have just reloaded after the panel's own container was
  // replaced mid-update (the case this whole feature exists for, FR-87.2).
  useEffect(() => {
    let cancelled = false
    getUpdateStatus()
      .then((st) => {
        if (cancelled) return
        if (st.locked) {
          setStage((st.stage as Stage) ?? 'apply')
          attachStream()
        } else if (st.last_action === 'update' && st.result) {
          setOutcome({
            kind: st.result === 'success' ? 'done' : 'rolled_back',
            version: st.version,
          })
        }
      })
      .catch(() => {
        /* not configured or unreachable — the guided command above still works */
      })
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function attachStream() {
    streamRef.current?.close()
    setReconnecting(false)
    streamRef.current = openUpdateStream(
      (evt) => {
        if (evt.type === 'progress') {
          setStage((evt.stage as Stage) ?? 'apply')
        } else {
          setStage(evt.type)
          setOutcome({ kind: evt.type, version: evt.version })
          streamRef.current = null
          onUpdated()
        }
      },
      () => setReconnecting(true),
    )
  }

  async function runCheck() {
    setChecking(true)
    setCheckError(null)
    try {
      const result = await checkForUpdate()
      setCheckResult(result)
    } catch (err) {
      setCheckError(err instanceof Error ? err.message : String(err))
    } finally {
      setChecking(false)
    }
  }

  async function confirmUpdate() {
    try {
      await startUpdate(checkResult?.bundle_path)
      setOutcome(null)
      setStage('backup')
      attachStream()
    } catch (err) {
      toast(
        err instanceof Error ? err.message : t('settings.system.oneClick.genericError'),
        'danger',
      )
    }
  }

  const updating = stage !== null && outcome === null

  return (
    <section className="rounded-lg border border-surface-sunken bg-surface-raised p-4">
      <h3 className="font-semibold">{t('settings.system.oneClick.title')}</h3>
      <p className="mt-2 text-sm text-ink-muted">{t('settings.system.oneClick.intro')}</p>

      {updating ? (
        <div className="mt-4 space-y-2">
          <p className="text-sm font-medium">{t(`settings.system.oneClick.stage.${stage}`)}</p>
          {reconnecting ? (
            <p className="text-xs text-ink-muted">{t('settings.system.oneClick.reconnecting')}</p>
          ) : null}
        </div>
      ) : (
        <div className="mt-4 flex flex-wrap items-center gap-3">
          <Button variant="ghost" disabled={checking} onClick={runCheck}>
            {checking
              ? t('settings.system.oneClick.checking')
              : t('settings.system.oneClick.checkButton')}
          </Button>
          {checkResult && !checkResult.available_version ? (
            <span className="text-sm text-ink-muted">
              {t('settings.system.oneClick.upToDate', { version: checkResult.current_version })}
            </span>
          ) : null}
          {checkResult?.available_version ? (
            <>
              <span className="text-sm">
                {t('settings.system.oneClick.updateAvailable', {
                  version: checkResult.available_version,
                })}
              </span>
              <Button variant="primary" onClick={() => setConfirmOpen(true)}>
                {t('settings.system.oneClick.updateNowButton')}
              </Button>
            </>
          ) : null}
          {checkError ? <span className="text-sm text-danger">{checkError}</span> : null}
        </div>
      )}

      {outcome ? (
        <p
          role="status"
          className={
            'mt-3 rounded-md p-2 text-sm ' +
            (outcome.kind === 'done' ? 'bg-success/10 text-success' : 'bg-danger/10 text-danger')
          }
        >
          {outcome.kind === 'done'
            ? t('settings.system.oneClick.resultDone', {
                version: outcome.version ?? currentVersion,
              })
            : t('settings.system.oneClick.resultRolledBack', {
                version: outcome.version ?? currentVersion,
              })}
        </p>
      ) : null}

      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t('settings.system.oneClick.confirmTitle')}
        body={t('settings.system.oneClick.confirmBody')}
        confirmLabel={t('settings.system.oneClick.confirmConfirm')}
        onConfirm={confirmUpdate}
      />
    </section>
  )
}
