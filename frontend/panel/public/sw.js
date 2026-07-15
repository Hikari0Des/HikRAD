/**
 * Panel service worker — contract C5 (FR-54.2/54.3/54.4). Phase-4
 * cross-boundary exception (Agent F, Agent E unstaffed this phase). Mirrors
 * frontend/portal/public/sw.js (precache shell, network-first `/api/*` GETs,
 * update parked in `waiting` until user consent) plus the panel-only push
 * surface: `push` shows an alert-engine notification (contract C4), and
 * `notificationclick` routes to the alert's page (task 6).
 *
 * Push payload localization: `{title_key, body_key, params, url}` is meant to
 * localize client-side (C4). If a client tab is open we forward the raw
 * payload to it via postMessage so it can render fully localized (the
 * NotificationBell already owns that UI); the notification shown by the SW
 * itself falls back to English key text — seam flagged in the phase status
 * note — since a bare SW has no access to the manager's chosen locale/i18n
 * message tree without a build step this hand-written worker doesn't have.
 */
const VERSION = 'v1'
const SHELL_CACHE = `hikrad-panel-shell-${VERSION}`
const API_CACHE = `hikrad-panel-api-${VERSION}`
const SHELL_URLS = ['/', '/index.html', '/offline.html']

const FALLBACK_TEXT = {
  'alerts.nas_down.title': 'NAS down',
  'alerts.device_down.title': 'Device down',
  'alerts.expiring_digest.title': 'Subscribers expiring soon',
  'alerts.card_payment.title': 'Scratch-card payment update',
}

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches
      .open(SHELL_CACHE)
      .then((cache) => cache.addAll(SHELL_URLS))
      .catch(() => {}),
  )
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) =>
        Promise.all(
          keys.filter((k) => k !== SHELL_CACHE && k !== API_CACHE).map((k) => caches.delete(k)),
        ),
      )
      .then(() => self.clients.claim()),
  )
})

self.addEventListener('message', (event) => {
  if (event.data === 'SKIP_WAITING') self.skipWaiting()
})

self.addEventListener('fetch', (event) => {
  const req = event.request
  if (req.method !== 'GET') return

  const url = new URL(req.url)
  if (url.pathname.startsWith('/api/')) {
    event.respondWith(networkFirst(req))
    return
  }
  event.respondWith(shellFirst(req))
})

async function networkFirst(req) {
  try {
    const res = await fetch(req)
    if (res.ok) {
      const cache = await caches.open(API_CACHE)
      void cache.put(req, res.clone())
    }
    return res
  } catch (err) {
    const cached = await caches.match(req)
    if (cached) return cached
    throw err
  }
}

async function shellFirst(req) {
  const cached = await caches.match(req)
  if (cached) return cached
  try {
    const res = await fetch(req)
    if (res.ok) {
      const cache = await caches.open(SHELL_CACHE)
      void cache.put(req, res.clone())
    }
    return res
  } catch (err) {
    const offline = await caches.match('/offline.html')
    if (offline) return offline
    throw err
  }
}

self.addEventListener('push', (event) => {
  let data = {}
  try {
    data = event.data ? event.data.json() : {}
  } catch {
    data = {}
  }
  const title = FALLBACK_TEXT[data.title_key] || data.title_key || 'HikRAD'
  const body = FALLBACK_TEXT[data.body_key] || data.body_key || ''
  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((clients) => {
      // A focused client renders the fully localized in-app toast itself
      // (NotificationBell); the OS notification still shows so a
      // backgrounded/minimized panel isn't silent.
      for (const client of clients) client.postMessage({ type: 'hikrad:push', payload: data })
      return self.registration.showNotification(title, {
        body,
        data: { url: data.url || '/' },
        icon: '/icons/icon.svg',
      })
    }),
  )
})

self.addEventListener('notificationclick', (event) => {
  event.notification.close()
  const url = event.notification.data?.url || '/'
  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((clientsArr) => {
      const existing = clientsArr.find((c) => 'focus' in c)
      if (existing) {
        existing.postMessage({ type: 'hikrad:notification-click', url })
        return existing.focus()
      }
      return self.clients.openWindow(url)
    }),
  )
})
