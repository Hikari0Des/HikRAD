/**
 * Service worker registration + update detection (contract C5, FR-54.3). A
 * waiting worker never auto-activates — `SW_UPDATE_EVENT` fires so
 * <UpdateToast> can ask the user first; only then do we post SKIP_WAITING and
 * reload once the new worker takes control. Keeps the current PortalLayout
 * unaware of any of this (progressive enhancement — a browser without SW
 * support just runs the SPA normally).
 */
export const SW_UPDATE_EVENT = 'hikrad:sw-update-available'

let waitingWorker: ServiceWorker | null = null

export function registerServiceWorker(): void {
  if (!('serviceWorker' in navigator)) return
  // Never register in the vitest/jsdom test environment.
  if (import.meta.env.MODE === 'test') return

  window.addEventListener('load', () => {
    navigator.serviceWorker
      .register('/portal/sw.js', { scope: '/portal/' })
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

/** Accept the pending update: the new worker activates and the page reloads. */
export function applyServiceWorkerUpdate(): void {
  waitingWorker?.postMessage('SKIP_WAITING')
}

export function hasWaitingServiceWorker(): boolean {
  return waitingWorker !== null
}
