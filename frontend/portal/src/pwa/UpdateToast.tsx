import { useEffect, useState } from 'react'

import { useT } from '@hikrad/shared'

import { applyServiceWorkerUpdate, SW_UPDATE_EVENT } from './registerServiceWorker'

/** "Refresh for update" toast (FR-54.3) — a new deploy parks its service
 * worker in `waiting` until the subscriber consents here, so an installed
 * PWA is never silently pinned to a stale app shell. */
export function UpdateToast() {
  const t = useT()
  const [visible, setVisible] = useState(false)

  useEffect(() => {
    const onUpdate = () => setVisible(true)
    window.addEventListener(SW_UPDATE_EVENT, onUpdate)
    return () => window.removeEventListener(SW_UPDATE_EVENT, onUpdate)
  }, [])

  if (!visible) return null

  return (
    <div
      role="status"
      className="fixed inset-x-4 bottom-40 z-40 mx-auto flex max-w-md items-center justify-between gap-3 rounded-xl bg-ink p-3 text-sm text-ink-inverse shadow-lg"
    >
      <span>{t('portal.pwa.updateAvailable')}</span>
      <button
        type="button"
        onClick={() => {
          applyServiceWorkerUpdate()
          setVisible(false)
        }}
        className="shrink-0 rounded-md bg-brand px-3 py-1.5 font-semibold"
      >
        {t('portal.pwa.updateAction')}
      </button>
    </div>
  )
}
