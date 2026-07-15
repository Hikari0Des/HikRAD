/**
 * Panel service worker registration (contract C5, FR-54.3) — the Phase-4
 * cross-boundary exception (Agent E unstaffed this phase; see
 * docs/phases/phase-4-portal-payments-pwa/00-phase.md). Mirrors
 * frontend/portal/src/pwa/registerServiceWorker.ts: a waiting worker never
 * auto-activates until the user accepts <UpdateToast>.
 */
export const SW_UPDATE_EVENT = 'hikrad:panel:sw-update-available'

let waitingWorker: ServiceWorker | null = null

export function registerServiceWorker(): void {
  if (!('serviceWorker' in navigator)) return
  if (import.meta.env.MODE === 'test') return

  window.addEventListener('load', () => {
    navigator.serviceWorker
      .register('/sw.js', { scope: '/' })
      .then((registration) => {
        if (registration.waiting && registration.active) {
          waitingWorker = registration.waiting
          window.dispatchEvent(new Event(SW_UPDATE_EVENT))
        }

        registration.addEventListener('updatefound', () => {
          const installing = registration.installing
          if (!installing) return
          installing.addEventListener('statechange', () => {
            if (installing.state === 'installed' && navigator.serviceWorker.controller) {
              waitingWorker = installing
              window.dispatchEvent(new Event(SW_UPDATE_EVENT))
            }
          })
        })
      })
      .catch(() => {
        // Offline-first (NFR-7): a failed SW registration must not break the app.
      })

    let reloaded = false
    navigator.serviceWorker.addEventListener('controllerchange', () => {
      if (reloaded) return
      reloaded = true
      window.location.reload()
    })
  })
}

export function applyServiceWorkerUpdate(): void {
  waitingWorker?.postMessage('SKIP_WAITING')
}
