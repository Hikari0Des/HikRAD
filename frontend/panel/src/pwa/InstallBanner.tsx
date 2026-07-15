import { useState } from 'react'

import { useT } from '@hikrad/shared'

import { useInstallPrompt } from './useInstallPrompt'

const DISMISS_KEY = 'hikrad.install_banner_dismissed'

/** "Add to Home Screen" (FR-54.5), panel side — Hassan's field-agent tool /
 * Omar's dashboard-on-phone persona. Mirrors the portal's InstallBanner. */
export function InstallBanner() {
  const t = useT()
  const { canPromptAndroid, showIosEducation, promptAndroid } = useInstallPrompt()
  const [dismissed, setDismissed] = useState(() => {
    try {
      return window.localStorage.getItem(DISMISS_KEY) === '1'
    } catch {
      return false
    }
  })

  if (dismissed || (!canPromptAndroid && !showIosEducation)) return null

  function dismiss() {
    setDismissed(true)
    try {
      window.localStorage.setItem(DISMISS_KEY, '1')
    } catch {
      // best-effort only
    }
  }

  return (
    <div
      role="dialog"
      aria-label={t('pwa.installTitle')}
      className="fixed inset-x-4 bottom-4 z-40 mx-auto flex max-w-md flex-col gap-2 rounded-xl bg-surface-raised p-4 text-sm shadow-lg"
    >
      <p className="font-semibold">{t('pwa.installTitle')}</p>
      <p className="text-ink-muted">
        {showIosEducation ? t('pwa.installIosBody') : t('pwa.installAndroidBody')}
      </p>
      <div className="flex justify-end gap-2">
        <button type="button" onClick={dismiss} className="rounded-md px-3 py-1.5 text-ink-muted">
          {t('pwa.installDismiss')}
        </button>
        {canPromptAndroid ? (
          <button
            type="button"
            onClick={async () => {
              await promptAndroid()
              dismiss()
            }}
            className="rounded-md bg-brand px-3 py-1.5 font-semibold text-ink-inverse"
          >
            {t('pwa.installAction')}
          </button>
        ) : null}
      </div>
    </div>
  )
}
