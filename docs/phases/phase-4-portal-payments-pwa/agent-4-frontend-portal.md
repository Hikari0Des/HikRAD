# Phase 4 — Agent 4 (Frontend Portal & Localization): portal UI, payment flows, PWA packaging

> Owns FR-41–43 (UI), FR-54, NFR-6 completion push. Depends on contracts in [00-phase.md](00-phase.md) (C2, C3 routes, C4, C5); parallel with Agents 1–3.

## Mission & context
Build Noor's product on the Phase-1 skeleton: a phone-first trilingual portal (status, consumed data, usage, payments, account self-care, renew via voucher or e-wallet — per Decision 21 the portal never displays a quota ceiling or remaining balance), and package **both** apps as installable PWAs — branded manifests, service workers with honest offline states, install prompts, Web Push. This phase you exceptionally own the panel's PWA assets too (Agent E is unstaffed — recorded in the phase brief). Detail source: sub-PRD [07-subscriber-portal-pwa](../../prd/07-subscriber-portal-pwa.md).

## File ownership
- **Exclusive:** `frontend/portal/**`, `frontend/shared/**`, `frontend/panel/public/**` + `frontend/panel/src/pwa/**` (manifest link, SW registration, update toast, push opt-in — nothing else in panel).
- **Read-only:** rest of `frontend/panel/**`. **Forbidden:** panel screens/components, all backend paths.

## Tasks
1. **Portal home** (FR-41.2): above-the-fold answer card — status (active/expired + online now), days remaining, data consumed this cycle (plain figure/trend — no quota bar, total, or remaining, per Decision 21), current speed (profile; live when online); Renew as floating primary action. Skeleton loading, pull-to-refresh.
1b. **Account self-care** (FR-44): settings screen for password change (with the "your PPPoE login password changes too" warning) and editable contact details, on `PUT /api/v1/portal/me`.
2. **Usage & payments** (FR-41.3): daily/monthly charts (LTR inside RTL, shared chart convention), payment history list from the portal API.
3. **Renew flows** (FR-42): voucher entry (format hints, clear used/expired/invalid errors) → success screen with new expiry + "reconnection may take a moment" note; e-wallet: enabled-gateway list → create intent → redirect/app handoff → return route polling intent status → success/pending/failed screens (pending state reassuring, reference number visible, retry affordance); all gateways down → explanatory message + voucher path prominent (NFR-7). Test against D's mock simulator.
3b. **Scratch-card flow** (FR-59, C8, amendment 2026-07-11): card-type picker (from settings list) + code entry → "1-day test internet active, pending ISP verification" state prominently on home until decided; approved → normal renewed state + notification; rejected → clear what-next message (reason shown, voucher path offered); cooldown/one-pending errors rendered helpfully; state-change notifications surface in the portal (and arrive via WhatsApp per C7/C8).
4. **Login & branding** (FR-41.1, FR-43): branded login (ISP logo/name/colors from the branding endpoint), language switcher on login, per-subscriber language persisted via API; friendly errors for rate-limited/disabled accounts.
5. **PWA — portal** (FR-54): manifest endpoint wiring (branded, maskable icons), SW (precache shell, network-first API, explicit offline screen with "no connection — showing last data" where cached, honest no-offline-mutations), install prompt flow (`beforeinstallprompt` + iOS education component), SW update toast.
6. **PWA — panel** (exception scope): same manifest/SW/update-toast/install treatment under `frontend/panel/public/` + `src/pwa/`; push opt-in UI in the existing notification center slot (E left the slot in Phase 3), wiring C4 subscribe endpoints; notification click-through routes (alert → relevant page).
7. **Localization completion push** (NFR-6): full ar + en portal strings; ku translation pass across portal **and** panel key gaps (i18n:check untranslated report driven to a tracked list; 0 untranslated is the Phase-5/v1 cut criterion, minimize now); Eastern-Arabic numerals honored in consumed-data/amount displays.

Edge cases: SW caching must never serve stale API data as fresh (network-first + cache labeling); payment return route when the user closed the tab mid-payment (intent poll on next open, deep-link safe); Android/iOS install differences; push permission denial handled quietly.

## Contracts consumed/exposed
- **Consumes:** C2 portal API, C3 payment routes + mock simulator, C4 push subscribe + payload keys, C5 manifest/branding — all frozen.
- **Exposes:** the shipped portal + both PWA shells; the ku-gap tracked list Phase 5 closes for the v1 cut.

## Definition of done
- Gate items 1, 2, 4 pass (real-phone Arabic run-through; mock payment lifecycle; installs + offline screens + update toast on both apps); gate 5's client half (push opt-in → notification received).
- Component tests: renew flow states (all gateway outcomes), intent polling reducer, offline screen trigger, SW update prompt, language persistence.
- `i18n:check` green; untranslated-ku report attached to the PR; Lighthouse PWA checks pass for both apps.

## Handoff
Phase 5 receives: portal feature-complete (Noor's flows), PWA packaging done, and the ku string gap list to close at v1 cut; E resumes panel ownership with the PWA shell in place (documented in `frontend/panel/src/pwa/README.md`).
