/**
 * Portal service worker — contract C5 (FR-54.2/54.3). Plain hand-written SW
 * (no build-time generation): precaches the app shell, network-first for
 * `/api/*` GETs with a cache fallback (the app's own OfflineBanner labels
 * cached data as stale — this SW only supplies it), never caches or replays
 * mutations. Updates never auto-activate: a new SW parks in `waiting` until
 * the app posts SKIP_WAITING after the user accepts the update toast, so a
 * server deploy never silently pins a stale shell either.
 */
const VERSION = 'v1'
const SHELL_CACHE = `hikrad-portal-shell-${VERSION}`
const API_CACHE = `hikrad-portal-api-${VERSION}`
const BASE = '/portal/'
const SHELL_URLS = [BASE, `${BASE}index.html`, `${BASE}offline.html`]

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches
      .open(SHELL_CACHE)
      .then((cache) => cache.addAll(SHELL_URLS))
      .catch(() => {
        // Precache is best-effort — a fresh install still works online.
      }),
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
  if (req.method !== 'GET') return // no offline mutations (FR-54.2)

  const url = new URL(req.url)

  if (url.pathname.startsWith('/api/')) {
    event.respondWith(networkFirst(req))
    return
  }

  if (url.pathname.startsWith(BASE)) {
    event.respondWith(shellFirst(req))
  }
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
    const offline = await caches.match(`${BASE}offline.html`)
    if (offline) return offline
    throw err
  }
}
