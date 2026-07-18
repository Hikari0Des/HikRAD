# HikRAD — Sub-PRD 07: Subscriber Portal, Localization & PWA

> Derived from [docs/PRD.md](../PRD.md) v1.0 on 2026-07-08; updated 2026-07-10 for master v1.2 (Decision 21: portal shows consumed data only, never quota ceiling/remaining; FR-44 promoted C→S); updated 2026-07-17 for master v1.8 (FR-42 amended, Decision 37, v2-2 — e-wallet gateway list replaced by the unified Pay screen; the payment logic itself is owned by [05](05-billing-payments-vouchers.md) FR-77–80, this file owns only the portal UI surface); updated 2026-07-18 for master Decision 42 — v2 phase 11: FR-92, instance identity threaded through the portal + both apps' PWA manifests. Owns: FR-41, FR-42, FR-43, FR-44, FR-54, FR-92 · NFR-6 · Risk: RTL/trilingual UI effort
> Depends on: [04-subscribers-profiles](04-subscribers-profiles.md) (subscriber state, quota), [03-lossless-accounting-live-monitoring](03-lossless-accounting-live-monitoring.md) (usage graph data), [05-billing-payments-vouchers](05-billing-payments-vouchers.md) (voucher redeem + e-wallet payment APIs, payment history), [06-managers-roles-security](06-managers-roles-security.md) (password storage + rate-limit policy), [01-platform-install-licensing](01-platform-install-licensing.md) (branding settings, Caddy/HTTPS) · Depended on by: none (leaf module), though the **panel PWA packaging** in FR-54 wraps the panel built by modules 02–06/08.

## 1. Scope & context

The subscriber-facing surface (**Noor**'s product) and two cross-cutting frontend concerns the whole product inherits: trilingual localization with true RTL (NFR-6) and PWA packaging of *both* portal and panel (FR-54 — this is the v1 "mobile app", a user-confirmed decision replacing native apps; a TWA store wrapper is post-v1 only if needed). The portal lets a subscriber check status, consumed data, speed and usage, see payments, manage their own details/password, and renew via voucher or e-wallet at midnight without calling anyone (Noor's user story) — branded per ISP. Per Decision 21 the portal shows what the subscriber has *consumed*, never the plan's quota ceiling or remaining balance.

## 2. Owned requirements — elaborated

### FR-41 (M) — Subscriber login & self-care portal
**Master (v1.2):** Subscriber login (username/password) to a mobile-responsive portal: status, expiry, consumed data (never the quota ceiling or remaining balance — Decision 21), current speed, usage graphs, payment history, and the subscriber's own subscription/account details.

*Elaboration:*
- **FR-41.1** — Login with RADIUS username/password (same credential as PPPoE; verified against the NFR-4.2 encrypted store via a server-side check — cleartext never leaves the backend). Rate-limited per NFR-4.6 using [06](06-managers-roles-security.md) FR-28.2's mechanism. Portal sessions are separate from panel sessions (long-lived refresh appropriate for a phone app).
- **FR-41.2** — Home screen answers Noor's questions at a glance: status (active/expired + online now), days remaining, **data consumed this cycle** (a plain figure/trend — never a quota total, remaining balance, or progress-toward-limit bar, per Decision 21), current speed (profile rate, live session rate when online — from [03](03-lossless-accounting-live-monitoring.md)), profile/subscription details, with Renew as the primary action.
- **FR-41.3** — Usage: daily/monthly graphs ([03](03-lossless-accounting-live-monitoring.md) FR-33 API, scoped to self); payment history ([05](05-billing-payments-vouchers.md) ledger, scoped to self). All portal endpoints are subscriber-scoped server-side — a subscriber token can never read another subscriber's data.

### FR-42 (M) — Portal renewal — AMENDED (v2-2, Decision 37): unified Pay screen replaces the e-wallet gateway list
**Master (v2-2):** ~~Redeem voucher code; pay via enabled e-wallet gateways.~~ One combined Pay screen listing every method the subscriber's owning manager enabled — provider transfers, scratch card, voucher — as tiles in a single picker (no separate scratch-card screen); e-wallet gateways no longer exist (FR-23 retired).

*Elaboration:*
- **FR-42.1** *(v2-2, supersedes the pre-v2-2 e-wallet-flow text below)* — The Pay screen renders [05](05-billing-payments-vouchers.md) FR-78.1's resolved tile list. Selecting a provider tile opens FR-78.2's transfer-proof form (amount/reference/date/note/attachments); selecting scratch-card or voucher opens their existing per-method forms, now sharing the same ticket/timeline machinery ([05](05-billing-payments-vouchers.md) FR-79) instead of separate flows. A manager who enabled nothing shows an explanatory empty state — never a blank Pay screen or an error.
- **FR-42.2** — Voucher entry (unchanged): client-side format hints, server redeem via [05](05-billing-payments-vouchers.md) FR-22.2, clear already-used/expired errors.
- **FR-42.3** — Ticket status while pending: a "payment pending — under review" state with the trial's remaining time visible (reusing the provisional-renewal indicator FR-59.1's UI already had), pull-to-refresh, and a push/in-app update the moment the ticket is decided ([05](05-billing-payments-vouchers.md) FR-80.1) — no gateway-callback polling exists anymore, since there is no gateway.
- *(Historical, pre-v2-2, retired):* ~~e-wallet flow lists only gateways enabled in settings → create payment → gateway redirect/app handoff → return/callback → success screen when the intent confirms ([05](05-billing-payments-vouchers.md) FR-23.2). Gateway unreachable → graceful message + voucher path remains.~~ Renewal success still reflects new expiry immediately and reminds the user reconnection may take a moment (CoA restore, [02](02-radius-nas-aaa.md)) — that part of the flow is unchanged by this amendment.

### FR-43 (M) — Localization & ISP branding
**Master:** Portal fully localized (Arabic RTL, Kurdish Sorani RTL, English) with ISP branding (logo, name, colors) set in admin settings.

*Elaboration:* branding (logo, name, colors from [01](01-platform-install-licensing.md) FR-53.2) applied at runtime — no rebuild per ISP; per-subscriber language preference persisted; language switcher on the login page itself.

### FR-44 (S) — Self-service account maintenance (password & details)
**Master (v1.2):** Self-service account maintenance: the subscriber can log in and update their own password and contact details (phone confirmation flow) — promoted from Could per Decision 21.

*Elaboration:* password change re-encrypts per NFR-4.2 and calls `InvalidatePolicy` ([02](02-radius-nas-aaa.md)) — the PPPoE credential changes too, which the UI must warn about; phone confirmation = operator-verified or code-based confirmation flag on the subscriber record. Detail edits are limited to subscriber-safe fields (phone, contact info, language) — never profile, expiry, MAC, or status; every change is audit-logged.

### FR-54 (M) — PWA packaging of portal and panel
**Master:** Both the subscriber portal and the admin/manager panel ship as installable PWAs: web app manifest (per-ISP icon/name from branding settings), service worker with app-shell caching and an offline "no connection" state, HTTPS-served, "Add to Home Screen" install prompt. Push notifications via Web Push where the platform allows (Android fully; iOS after home-screen install). This replaces native mobile apps; an optional TWA wrapper for Play Store distribution is a post-v1 item.

*Elaboration:*
- **FR-54.1** — Two installable apps: portal PWA (Noor) and panel PWA (Hassan's field-agent tool per his persona; Omar's dashboard-on-phone). Manifests generated server-side from branding settings (name, theme color, icon rendered from the uploaded logo with maskable variants).
- **FR-54.2** — Service worker: precached app shell; runtime network-first for API data; an explicit offline screen ("no connection — showing last data" or a friendly retry) rather than a browser error. **No offline mutations in v1** — renewals/edits require connectivity; the offline state must make that honest.
- **FR-54.3** — Update flow: new deploys activate on next launch with a "refresh for update" toast (stale service workers must not pin users to old app shells across server updates, [01](01-platform-install-licensing.md) FR-51.4).
- **FR-54.4** — Web Push where the platform allows (Android fully; iOS after home-screen install): panel push carries alert-engine notifications ([03](03-lossless-accounting-live-monitoring.md) FR-36.2's in-app channel extended to push); portal push (expiry reminders) only if trivially enabled by the same plumbing — otherwise post-v1. Push requires no third-party service beyond standard Web Push endpoints (degrades gracefully offline, NFR-7).
- **FR-54.5** — Install prompt: contextual "Add to Home Screen" education for iOS Safari (no native prompt) and the `beforeinstallprompt` flow on Android/Chrome.

### FR-92 (S) — v2: Instance identity threaded everywhere

**Master (Decision 42):** the public identity FR-91 exposes is consumed consistently by the portal and both apps' PWA manifests, instead of the disconnected/broken settings reads these surfaces had before this phase.

*Elaboration:*
- **FR-92.1** — Portal login screen and shell already call `GET /api/v1/branding` (`frontend/portal/src/branding.tsx`, contract C5) — this FR's actual work is fixing the endpoint itself (FR-91.2, owned by 01) so what was always a silent no-op starts rendering the real configured name/logo/color. No portal client code changes are required for this half; it is validated by re-running the existing branded-login test against a configured instance.
- **FR-92.2** — Both apps' PWA manifests (`BrandedManifestLink.tsx` in `frontend/panel/src/pwa/` and `frontend/portal/src/pwa/`, FR-54.1) already swap `name`/`short_name`/`icons`/`theme_color` from the same endpoint at runtime — same fix-not-rebuild as FR-92.1. The static fallback manifest (`public/manifest.webmanifest`) stays the generic-branding install target for the brief pre-fetch window and for an unconfigured instance (NFR-7: installability never depends on the endpoint resolving).
- **FR-92.3** — Icon set: a single uploaded logo (arbitrary aspect ratio, PNG/SVG) is served as-is for `purpose: any` and `purpose: maskable` alike (no server-side safe-zone padding/cropping in this phase — a maskable icon that isn't already roughly square/padded may be clipped by an OS launcher; documented as a known cosmetic limitation, not blocking). A logo change is picked up by an already-open installed PWA on its next service-worker update check (FR-54.3's existing update flow — no new mechanism).
- **FR-92.4** — Panel-side threading (sidebar/header mark, browser `<title>`, panel's own login screen) is in scope for this phase but has no panel-owning sub-PRD to record it against; it is frozen and gated in `docs/v2/phases/phase-v2-11-instance-branding/00-phase.md` under the same contract this FR defines, cross-referenced here for traceability.

**Acceptance:**
- **AC-92a** — Given a configured instance name/logo, when the portal login page loads with no session, then it shows that name/logo (not "HikRAD"), and when the panel PWA manifest is fetched, its `name`/`short_name`/icons match.
- **AC-92b** — Given the server is completely unreachable (airplane mode) on a device that already installed the PWA, then the installed app still launches with its last-known icon/name — no broken icon, no reversion to the generic mark.

### NFR-6 (owned) — Localization (product-wide)
**Master:** All UI strings externalized; Arabic and Kurdish Sorani with true RTL layout (mirrored navigation, charts LTR inside RTL pages); English as development baseline; numerals and currency per locale.

*Elaboration (ownership note — applies to panel and portal; other modules build to these rules):*
- **NFR-6.1** — All strings in locale files from day one; hardcoded UI strings are CI-flagged. English is the development baseline; Arabic ships with it; **Kurdish Sorani strings complete before v1** (per the master risk mitigation's sequencing).
- **NFR-6.2** — True RTL: layout via CSS logical properties; navigation, icons with direction semantics, and steppers mirror; charts, code/config snippets, usernames, MACs, IPs, and phone numbers render LTR embedded in RTL text (explicit bidi isolation).
- **NFR-6.3** — Numerals (Eastern Arabic option), IQD currency formatting, and date formats follow the locale + settings ([01](01-platform-install-licensing.md) FR-53.2); the component library must be RTL-capable per the master's architecture note (MUI/Ant RTL, or Tailwind + Radix with logical properties).

## 3. Acceptance criteria

- **AC-41a** — Given Noor's PPPoE credentials, when she logs into the portal on a phone, then status, days remaining, consumed data, and current speed are visible without scrolling past the fold — and no quota ceiling/remaining figure appears anywhere in the portal (Decision 21).
- **AC-41b** — Given a valid subscriber token, when it requests another subscriber's usage or payments by ID manipulation, then the API returns 403/404 (scoping is server-side).
- **AC-42a** — Given a valid voucher at 00:00 with no staff awake, when Noor redeems it, then her expiry extends and (if online in the expired pool) full speed resumes without a call (her user story, end-to-end).
- **AC-42b** *(v2-2, supersedes the pre-v2-2 gateway-unreachable wording)* — Given a manager who has enabled no payment methods at all for their subscribers, when Noor opens Pay, then she sees an explanatory empty state, not an error — and if voucher redemption is enabled, that tile still works regardless of every other method's state (NFR-7: no single method's absence breaks another).
- **AC-43a** — Given the language set to Arabic or Kurdish, then the entire portal renders RTL-mirrored with zero untranslated strings, and usage charts remain LTR.
- **AC-54a** — Given Chrome on Android over HTTPS, when Hassan installs the panel PWA, then it launches standalone with the ISP's icon/name, and with airplane mode on it shows the designed offline screen — not a browser error.
- **AC-54b** — Given a NAS-down alert with panel push enabled on Android, then Hassan's installed panel PWA receives a push notification.
- **AC-54c** — Given a server update deploying new frontend assets, when an installed PWA next launches, then it offers/loads the new version (no permanently stale shell).
- **AC-NFR6a** — Given the CI string-extraction check, then no user-visible hardcoded string passes; given the Kurdish locale file at v1 cut, then it is 100% complete.

## 4. Data & interfaces

**Owned entities:** `portal_sessions` (subscriber tokens), subscriber language preference, Web Push subscriptions (endpoint, keys, surface panel/portal), locale string catalogs.

**Exposes:**
- Portal API surface (all subscriber-scoped): `POST /api/v1/portal/login`, `GET /api/v1/portal/me` (status/expiry/consumed-data/speed — no quota total/remaining fields, Decision 21), `GET /api/v1/portal/usage`, `GET /api/v1/portal/payments`, `POST /api/v1/portal/vouchers/redeem`, `PUT /api/v1/portal/me` (FR-44 detail/password self-update). ~~`POST /api/v1/portal/payments/{gateway}/create`~~ **removed (v2-2, FR-23 retired)** — replaced by [05](05-billing-payments-vouchers.md) FR-78's `GET /api/v1/portal/pay-methods` + `POST /api/v1/portal/payment-tickets` (owned/frozen there; this file consumes them for the portal UI only).
- `GET /manifest.webmanifest` (per-surface, branded), service workers for both apps, `POST /api/v1/push/subscribe`.
- Localization framework + locale files consumed by **every** UI module (02–06, 08).

**Consumes:** subscriber read model + quota from [04](04-subscribers-profiles.md); usage graphs from [03](03-lossless-accounting-live-monitoring.md); redeem/payment APIs from [05](05-billing-payments-vouchers.md); credential verification + rate limiting from [06](06-managers-roles-security.md); branding + HTTPS from [01](01-platform-install-licensing.md).

## 5. UX notes

Portal is phone-first (Noor may never see it on a desktop): one-column, large touch targets, Renew as a floating primary action. Loading states: skeletons, never spinners over blank pages; payment-pending state must be reassuring (reference number visible). Login page carries ISP branding + language switcher. Panel PWA inherits panel UX from each module; this module owns only shell/manifest/offline/push behaviors. Iraqi context: assume intermittent connectivity — every screen tolerates a dropped request with a retry affordance.

## 6. Out of scope

- Renewal/billing logic → [05](05-billing-payments-vouchers.md); subscriber data rules → [04](04-subscribers-profiles.md); alert rules → [03](03-lossless-accounting-live-monitoring.md); Hotspot login page (a MikroTik-served template, not the portal) → [02](02-radius-nas-aaa.md) FR-18.
- **Deferred by master:** native mobile apps (replaced by FR-54); TWA wrapper for Play Store (post-v1, only if store presence proves necessary — Decision 5); public API docs (post-v1).

## 7. Risks & open questions (owned)

- **Risk (master): RTL/trilingual UI doubles frontend effort.** Likelihood Medium / Impact Medium. Mitigation: RTL-capable component library from day one; logical CSS properties; ship Arabic+English first, Kurdish strings before v1. *Elaboration:* choose and prove the component library's RTL story in P1 (before any panel screens exist); NFR-6.1's CI check keeps the string debt at zero rather than a pre-v1 crunch.
- **NEW:** Kurdish Sorani translation source — who translates and reviews? Budget a native reviewer before v1 cut; pilot ISP choice (open question 2, owned by [02](02-radius-nas-aaa.md)) determines its actual launch priority.
- **NEW:** iOS Web Push requires home-screen install and recent iOS versions — validate on real devices common in Iraq during P5; if coverage is poor, panel-critical alerts must remain reliable via Telegram ([03](03-lossless-accounting-live-monitoring.md) FR-36.2), which is the primary channel anyway.
