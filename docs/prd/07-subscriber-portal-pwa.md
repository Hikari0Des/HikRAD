# HikRAD — Sub-PRD 07: Subscriber Portal, Localization & PWA

> Derived from [docs/PRD.md](../PRD.md) v1.0 on 2026-07-08. Owns: FR-41, FR-42, FR-43, FR-44, FR-54 · NFR-6 · Risk: RTL/trilingual UI effort
> Depends on: [04-subscribers-profiles](04-subscribers-profiles.md) (subscriber state, quota), [03-lossless-accounting-live-monitoring](03-lossless-accounting-live-monitoring.md) (usage graph data), [05-billing-payments-vouchers](05-billing-payments-vouchers.md) (voucher redeem + e-wallet payment APIs, payment history), [06-managers-roles-security](06-managers-roles-security.md) (password storage + rate-limit policy), [01-platform-install-licensing](01-platform-install-licensing.md) (branding settings, Caddy/HTTPS) · Depended on by: none (leaf module), though the **panel PWA packaging** in FR-54 wraps the panel built by modules 02–06/08.

## 1. Scope & context

The subscriber-facing surface (**Noor**'s product) and two cross-cutting frontend concerns the whole product inherits: trilingual localization with true RTL (NFR-6) and PWA packaging of *both* portal and panel (FR-54 — this is the v1 "mobile app", a user-confirmed decision replacing native apps; a TWA store wrapper is post-v1 only if needed). The portal lets a subscriber check status, quota, speed and usage, see payments, and renew via voucher or e-wallet at midnight without calling anyone (Noor's user story) — branded per ISP.

## 2. Owned requirements — elaborated

### FR-41 (M) — Subscriber login & self-care portal
**Master:** Subscriber login (username/password) to a mobile-responsive portal: status, expiry, remaining quota, current speed, usage graphs, payment history.

*Elaboration:*
- **FR-41.1** — Login with RADIUS username/password (same credential as PPPoE; verified against the NFR-4.2 encrypted store via a server-side check — cleartext never leaves the backend). Rate-limited per NFR-4.6 using [06](06-managers-roles-security.md) FR-28.2's mechanism. Portal sessions are separate from panel sessions (long-lived refresh appropriate for a phone app).
- **FR-41.2** — Home screen answers Noor's questions at a glance: status (active/expired + online now), days remaining, quota remaining (progress bar), current speed (profile rate, live session rate when online — from [03](03-lossless-accounting-live-monitoring.md)), with Renew as the primary action.
- **FR-41.3** — Usage: daily/monthly graphs ([03](03-lossless-accounting-live-monitoring.md) FR-33 API, scoped to self); payment history ([05](05-billing-payments-vouchers.md) ledger, scoped to self). All portal endpoints are subscriber-scoped server-side — a subscriber token can never read another subscriber's data.

### FR-42 (M) — Portal renewal
**Master:** Redeem voucher code; pay via enabled e-wallet gateways.

*Elaboration:* voucher entry (client-side format hints, server redeem via [05](05-billing-payments-vouchers.md) FR-22.2, clear already-used/expired errors); e-wallet flow lists only gateways enabled in settings → create payment → gateway redirect/app handoff → return/callback → success screen when the intent confirms ([05](05-billing-payments-vouchers.md) FR-23.2), with a "payment pending" state and pull-to-refresh while reconciliation runs. Gateway unreachable → graceful message + voucher path remains (NFR-7). Renewal success reflects new expiry immediately and reminds the user reconnection may take a moment (CoA restore, [02](02-radius-nas-aaa.md)).

### FR-43 (M) — Localization & ISP branding
**Master:** Portal fully localized (Arabic RTL, Kurdish Sorani RTL, English) with ISP branding (logo, name, colors) set in admin settings.

*Elaboration:* branding (logo, name, colors from [01](01-platform-install-licensing.md) FR-53.2) applied at runtime — no rebuild per ISP; per-subscriber language preference persisted; language switcher on the login page itself.

### FR-44 (C) — Password self-change & phone confirmation
**Master:** Password self-change and phone-number confirmation.

*Elaboration (Could):* password change re-encrypts per NFR-4.2 and calls `InvalidatePolicy` ([02](02-radius-nas-aaa.md)) — the PPPoE credential changes too, which the UI must warn about; phone confirmation = operator-verified or code-based confirmation flag on the subscriber record. Build only if v1 schedule allows.

### FR-54 (M) — PWA packaging of portal and panel
**Master:** Both the subscriber portal and the admin/manager panel ship as installable PWAs: web app manifest (per-ISP icon/name from branding settings), service worker with app-shell caching and an offline "no connection" state, HTTPS-served, "Add to Home Screen" install prompt. Push notifications via Web Push where the platform allows (Android fully; iOS after home-screen install). This replaces native mobile apps; an optional TWA wrapper for Play Store distribution is a post-v1 item.

*Elaboration:*
- **FR-54.1** — Two installable apps: portal PWA (Noor) and panel PWA (Hassan's field-agent tool per his persona; Omar's dashboard-on-phone). Manifests generated server-side from branding settings (name, theme color, icon rendered from the uploaded logo with maskable variants).
- **FR-54.2** — Service worker: precached app shell; runtime network-first for API data; an explicit offline screen ("no connection — showing last data" or a friendly retry) rather than a browser error. **No offline mutations in v1** — renewals/edits require connectivity; the offline state must make that honest.
- **FR-54.3** — Update flow: new deploys activate on next launch with a "refresh for update" toast (stale service workers must not pin users to old app shells across server updates, [01](01-platform-install-licensing.md) FR-51.4).
- **FR-54.4** — Web Push where the platform allows (Android fully; iOS after home-screen install): panel push carries alert-engine notifications ([03](03-lossless-accounting-live-monitoring.md) FR-36.2's in-app channel extended to push); portal push (expiry reminders) only if trivially enabled by the same plumbing — otherwise post-v1. Push requires no third-party service beyond standard Web Push endpoints (degrades gracefully offline, NFR-7).
- **FR-54.5** — Install prompt: contextual "Add to Home Screen" education for iOS Safari (no native prompt) and the `beforeinstallprompt` flow on Android/Chrome.

### NFR-6 (owned) — Localization (product-wide)
**Master:** All UI strings externalized; Arabic and Kurdish Sorani with true RTL layout (mirrored navigation, charts LTR inside RTL pages); English as development baseline; numerals and currency per locale.

*Elaboration (ownership note — applies to panel and portal; other modules build to these rules):*
- **NFR-6.1** — All strings in locale files from day one; hardcoded UI strings are CI-flagged. English is the development baseline; Arabic ships with it; **Kurdish Sorani strings complete before v1** (per the master risk mitigation's sequencing).
- **NFR-6.2** — True RTL: layout via CSS logical properties; navigation, icons with direction semantics, and steppers mirror; charts, code/config snippets, usernames, MACs, IPs, and phone numbers render LTR embedded in RTL text (explicit bidi isolation).
- **NFR-6.3** — Numerals (Eastern Arabic option), IQD currency formatting, and date formats follow the locale + settings ([01](01-platform-install-licensing.md) FR-53.2); the component library must be RTL-capable per the master's architecture note (MUI/Ant RTL, or Tailwind + Radix with logical properties).

## 3. Acceptance criteria

- **AC-41a** — Given Noor's PPPoE credentials, when she logs into the portal on a phone, then status, days remaining, quota bar, and current speed are visible without scrolling past the fold.
- **AC-41b** — Given a valid subscriber token, when it requests another subscriber's usage or payments by ID manipulation, then the API returns 403/404 (scoping is server-side).
- **AC-42a** — Given a valid voucher at 00:00 with no staff awake, when Noor redeems it, then her expiry extends and (if online in the expired pool) full speed resumes without a call (her user story, end-to-end).
- **AC-42b** — Given all gateways unreachable, when Noor opens Renew, then she sees an explanatory message and can still redeem a voucher.
- **AC-43a** — Given the language set to Arabic or Kurdish, then the entire portal renders RTL-mirrored with zero untranslated strings, and usage charts remain LTR.
- **AC-54a** — Given Chrome on Android over HTTPS, when Hassan installs the panel PWA, then it launches standalone with the ISP's icon/name, and with airplane mode on it shows the designed offline screen — not a browser error.
- **AC-54b** — Given a NAS-down alert with panel push enabled on Android, then Hassan's installed panel PWA receives a push notification.
- **AC-54c** — Given a server update deploying new frontend assets, when an installed PWA next launches, then it offers/loads the new version (no permanently stale shell).
- **AC-NFR6a** — Given the CI string-extraction check, then no user-visible hardcoded string passes; given the Kurdish locale file at v1 cut, then it is 100% complete.

## 4. Data & interfaces

**Owned entities:** `portal_sessions` (subscriber tokens), subscriber language preference, Web Push subscriptions (endpoint, keys, surface panel/portal), locale string catalogs.

**Exposes:**
- Portal API surface (all subscriber-scoped): `POST /api/v1/portal/login`, `GET /api/v1/portal/me` (status/expiry/quota/speed), `GET /api/v1/portal/usage`, `GET /api/v1/portal/payments`, `POST /api/v1/portal/vouchers/redeem`, `POST /api/v1/portal/payments/{gateway}/create`.
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
