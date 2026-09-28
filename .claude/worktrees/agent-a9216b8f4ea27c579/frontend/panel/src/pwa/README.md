# Panel PWA shell (Phase 4 cross-boundary exception)

Built by Agent F (Frontend Portal & Localization) during Phase 4 because Agent
E (Frontend Panel) is unstaffed this phase — see
`docs/phases/phase-4-portal-payments-pwa/00-phase.md` and `docs/phases/00-team.md`
role table. Everything here lives in `frontend/panel/public/**` and
`frontend/panel/src/pwa/**`, the two paths the phase brief carves out for F.
Three files outside that scope were touched, each a single mechanical wire-up
with no other changes:

- `frontend/panel/index.html` — manifest link + theme-color + apple-touch-icon tags.
- `frontend/panel/src/main.tsx` — mounts `registerServiceWorker()`,
  `<BrandedManifestLink>`, `<OfflineBanner>`, `<InstallBanner>`,
  `<UpdateToast>`, `<NotificationClickRouter>`. No other lines changed.
- `frontend/panel/src/shell/notifications.tsx` — one `<PushOptIn />` mount
  inside the existing notification-bell dropdown (task brief: "push opt-in UI
  in the existing notification center slot").

## What's here

- `registerServiceWorker.ts` — registers `/sw.js` (site root scope), parks
  updates in `waiting` until the user accepts `UpdateToast` (never
  auto-activates — FR-54.3).
- `useOnlineStatus.ts` / `OfflineBanner.tsx` — honest offline state (FR-54.2).
- `useInstallPrompt.ts` / `InstallBanner.tsx` — Android `beforeinstallprompt`
  capture + iOS Safari "Add to Home Screen" education (FR-54.5).
- `BrandedManifestLink.tsx` — swaps the manifest to a per-ISP one once
  `GET /api/v1/branding` resolves (contract C5); the static
  `public/manifest.webmanifest` is the generic-branding fallback so the app
  installs even before/without that endpoint (NFR-7).
- `pushApi.ts` / `PushOptIn.tsx` — Web Push subscribe/unsubscribe (contract
  C4). Permission denial is handled quietly (no error banner).
- `NotificationClickRouter.tsx` — routes an already-open tab to the alert's
  page when the user taps an OS notification (`public/sw.js`'s
  `notificationclick` handler postMessages the url here).
- `../../public/sw.js` — precache + network-first `/api/*` + push/notificationclick.
  Push payload localization (`{title_key, body_key, params, url}`, contract
  C4) only fully resolves when a client tab is open (it gets the raw payload
  via postMessage and can use `useT()`); the bare-SW notification text falls
  back to a small hardcoded English dictionary — **known seam**, see the
  Phase-4 status note for detail.

## For Agent E (Phase 5, resuming panel ownership)

Nothing here should need touching to keep working. If you want richer
localized OS notification text with no client tab open, the SW would need a
generated locale bundle (a build step, not a runtime import) — flagged as a
gap, not started.
