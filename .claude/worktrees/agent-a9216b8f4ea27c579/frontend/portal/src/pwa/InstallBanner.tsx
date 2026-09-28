import { useState } from 'react'

import { useT } from '@hikrad/shared'

import { useInstallPrompt } from './useInstallPrompt'

const DISMISS_KEY = 'hikrad.portal.install_banner_dismissed'

/** "Add to Home Screen" (FR-54.5): the native `beforeinstallprompt` flow on
 * Android/Chrome, contextual education (no native prompt exists) on iOS
 * Safari. Dismissible; the choice is remembered so it doesn't nag every visit. */
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
      aria-label={t('portal.pwa.installTitle')}
      className="fixed inset-x-4 bottom-40 z-40 mx-auto flex max-w-md flex-col gap-2 rounded-xl bg-surface-raised p-4 text-sm shadow-lg"
    >
      <p className="font-semibold">{t('portal.pwa.installTitle')}</p>
      <p className="text-ink-muted">
        {showIosEducation ? t('portal.pwa.installIosBody') : t('portal.pwa.installAndroidBody')}
      </p>
      <div className="flex justify-end gap-2">
        <button type="button" onClick={dismiss} className="rounded-md px-3 py-1.5 text-ink-muted">
          {t('portal.pwa.installDismiss')}
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
            {t('portal.pwa.installAction')}
          </button>
        ) : null}
      </div>
    </div>
  )
}
