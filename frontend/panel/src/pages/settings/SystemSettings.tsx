import { ErrorState, LoadingState, Ltr, useT } from '@hikrad/shared'

import { getSystemVersion } from '../../api/setup'
import { useAsync } from '../../hooks/useAsync'
import { useToast } from '../../components/Toast'

/**
 * Settings > System (item 1, guided update): shows the running version and
 * walks the operator through `hikrad update` on the host. A true one-click
 * update needs a privileged host-side updater (the panel runs inside the
 * container it would have to replace) — that is planned for v2; this screen
 * makes the safe path obvious in the meantime.
 */
export function SystemSettings() {
  const t = useT()
  const { toast } = useToast()
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
    </div>
  )
}
