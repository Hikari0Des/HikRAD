# Phase 4 — Portal, Payments & PWA

> Goal: master P5 — Noor (subscriber) self-serves from her phone: checks status/consumed-data/usage, redeems a voucher or pays via an Iraqi e-wallet at midnight, with the portal and panel installable as branded PWAs (the v1 "mobile app" per Decision 5). Plus ROS-version hardening of CoA. Requires Phase 3 gate green (renewal path + alerts exist). Agent E (panel) is **not staffed** this phase — the one cross-boundary exception (PWA assets in `frontend/panel/public/`) is assigned to F and safe.

## Agent roster & path ownership (verified disjoint)

| Agent | Task file | Exclusive paths this phase |
|---|---|---|
| B — RADIUS & NAS | [agent-1-radius-nas.md](agent-1-radius-nas.md) | `backend/internal/radius/**`, `deploy/freeradius/**`, `backend/test/harness/**`, `docs/ops/ros-matrix.md`, migrations `0320–0329` |
| C — Accounting & Monitoring | [agent-2-accounting-monitoring.md](agent-2-accounting-monitoring.md) | `backend/internal/monitorsvc/**`, `backend/internal/live/**`, `backend/internal/push/**`, migrations `0330–0339` |
| D — Backend Business | [agent-3-backend-business.md](agent-3-backend-business.md) | `backend/internal/portalapi/**`, `backend/internal/billing/**` (gateways), migrations `0300–0309` |
| F — Frontend Portal & Localization | [agent-4-frontend-portal.md](agent-4-frontend-portal.md) | `frontend/portal/**`, `frontend/shared/**`, `frontend/panel/public/**` + `frontend/panel/src/pwa/**` (exception, E unstaffed) |

## Frozen contracts

### C1. Schema (D 0300–0309; C 0330–0339; B 0320–0329)
- **D:** `portal_sessions` (subscriber refresh tokens), `payment_intents` (id, subscriber_id, profile_id, gateway, amount_iqd, state `pending|confirmed|renewed|failed|expired`, gateway_ref, created/updated), `gateway_configs` (gateway, enabled, creds_enc, mode `live|mock`), subscriber `language` pref, `card_payments` (FR-59, amendment 2026-07-11: subscriber_id, profile_id, card_type, card_code_enc, state `pending|approved|rejected`, trial_ledger_tx_id, decided_by/at, reject_reason).
- **C:** `push_subscriptions` (surface `panel|portal`, manager_id?/subscriber_id?, endpoint, keys, created).
- **B:** `nas` += api_port (default 8728), api_user, api_password_enc (FR-56.2 auto-setup credentials, sealed with A's crypto; amendment 2026-07-09).

### C2. Portal API (D) — all subscriber-scoped server-side
`POST /api/v1/portal/login {username,password}` → tokens + `{subscriber:{id,username,name,language}}` (verify against sealed password via internal call — decryption stays in the radius path's module boundary: D adds a narrow `VerifyPassword(username, password) bool` in subscribers using A's crypto; rate-limited via A's mechanism per NFR-4.6).
`GET /api/v1/portal/me` → `{status, online_now, expires_at, days_left, usage:{used_down, used_up, used_total}, speed:{profile_down/up, live_down/up?}, profile_name}` — **no quota total/remaining fields** (Decision 21; the backend still enforces quota, the portal just never sees the ceiling).
`PUT /api/v1/portal/me {phone?, password?}` → FR-44 self-update (password re-encrypts per NFR-4.2 + `InvalidatePolicy`; audit-logged; subscriber-safe fields only).
`GET /api/v1/portal/usage?granularity=daily|monthly&from&to`; `GET /api/v1/portal/payments` (own ledger slice).
`POST /api/v1/portal/vouchers/redeem {code}` → renewal result. `PUT /api/v1/portal/language {language}`.
IDOR rule: subscriber identity comes only from the token; no subscriber_id params anywhere.

### C3. PaymentGateway interface (D) — sub-PRD 05 FR-23.1
```go
type PaymentGateway interface {
  Name() string
  CreatePayment(ctx, Intent) (redirectURL string, gatewayRef string, err error)
  VerifyCallback(ctx, *http.Request) (CallbackResult, error) // signature-verified, idempotent
  QueryStatus(ctx, gatewayRef string) (State, error)
}
```
Routes: `POST /api/v1/portal/payments/{gateway}/create {profile_id}` → `{redirect_url, intent_id}`; `POST /api/v1/payments/{gateway}/callback` (public, unauthenticated, signature-verified, rate-limited); `GET /api/v1/portal/payments/intents/{id}` (poll for the pending screen). Confirmed → Phase-3 C2 renewal with source `portal-<gateway>`; reconciliation worker polls stuck-pending via QueryStatus. **v1 adapters: `mock` (always ships, full lifecycle incl. simulated callback for CI/demo) + ZainCash-first for live** (per master OQ-1 default; swap per merchant-account reality — the interface makes it a config change).

### C8. Scratch-card payments (D) — FR-59 (amendment 2026-07-11; sub-PRD 05 FR-59)
`POST /api/v1/portal/card-payments {card_type, code}` → `{state:"pending", trial_expires_at}` — atomically creates the pending row (code sealed with A's crypto) **and** runs a 1-day provisional renewal (Phase-3 C2 path, source `card-trial`; CoA restore included). Guards: one pending per subscriber; post-rejection cooldown (settings, default 7 d); card types from settings. Admin side: `GET /api/v1/card-payments?state=` (codes never in payloads), `POST /{id}/reveal` (returns code once, audited), `POST /{id}/approve` (full renewal anchored at trial start — trial day included, not added; source `card-<type>`), `POST /{id}/reject {reason}` (reversing entry, expiry rollback floor-now, FR-9 via CoA). All decisions publish `billing.card_payment {subscriber_id, state, reason?}` → C delivers portal push + FR-55 WhatsApp status messages. Panel queue **UI** is Phase 5 (E); this phase ships API + portal UI (F) and is gate-testable via API.

### C4. Web Push (C) — FR-54.4
`POST /api/v1/push/subscribe {surface, subscription}` / `DELETE …`; VAPID keys generated into settings on first boot; `push` becomes a 4th alert channel (panel surface) in the Phase-3 rules engine; portal expiring-reminder uses the existing `expiring_digest` rule with per-subscriber targeting (only if trivial — else recorded as deferred). Payload shape: `{title_key, body_key, params, url}` — localization client-side via i18n keys.

### C5. PWA (F) — FR-54
Both apps: `manifest.webmanifest` served from a small endpoint reading branding settings (name, colors, maskable icons derived from uploaded logo — D exposes `GET /api/v1/branding` public read); service worker: precache app shell, network-first API, explicit offline screen, no offline mutations; update toast on new SW (`skipWaiting` on user consent); iOS install education component. HTTPS already via Caddy.

### C6. NAS API auto-setup (B) — FR-56.2–56.4 (amendment 2026-07-09)
`POST /api/v1/nas/{id}/auto-setup/preview` → `{items:[{action:"add", path, command, current_state}], conflicts:[{path, existing, reason}]}` (connects read-only); `POST /api/v1/nas/{id}/auto-setup/apply {preview_hash}` → per-item results (refuses if router state changed since preview — hash mismatch). Additive-only, HikRAD-scoped; any conflict aborts the whole apply. RouterOS API client code inside the vendor adapter only (FR-17 grep applies). Validated per ROS version as part of this phase's matrix — apply is disabled for a version until its matrix leg is green.

### C7. Subscriber WhatsApp messaging (C ← D event) — FR-55 (amendment 2026-07-09)
Redis pub/sub `billing.renewed {subscriber_id, receipt_no, amount_iqd, new_expires_at, source}` — D publishes on every completed renewal (all sources: panel, voucher, portal-gateway); C consumes and sends the `payment_receipt` template (subscriber language, opt-in + valid phone required). Expiry reminder: per-subscriber targeting on the `expiring_digest` machinery (same mechanism as C4's portal push targeting). Templates/creds from settings (sub-PRD 01 FR-53.2); delivery isolation per FR-36.2.

## Cross-assignments (deliberate)
FR-23: interface+adapters D, portal payment UI F. FR-41/42: API D, UI F. FR-54: SW/manifest/install F, push backend C. FR-22 portal redemption: API existing (D Phase 3), UI F. FR-56 auto-setup: B end to end (UI is a panel slot E wires in Phase 5 — B ships the endpoints + harness proof this phase). FR-55: event D, delivery C.

## Integration gate
1. Noor's flow on a real phone: portal login → status/consumed-data/speed/usage visible (no quota ceiling anywhere, Decision 21) → voucher redeem at "midnight" (no staff) → expiry extends, expired-pool session restored via CoA (Phase-3 path) — all in Arabic RTL. Plus FR-44: she changes her password, is warned the PPPoE credential changes, and the next auth uses the new password.
2. Mock-gateway e-wallet flow end to end: create → redirect → simulated callback (replayed 3× = one renewal) → success screen; stuck-pending intent reconciled by QueryStatus poll; gateway disabled/unreachable → graceful message, voucher path still offered (NFR-7).
3. Live-adapter checklist done for whichever gateway has credentials (else documented as pending merchant account — explicitly acceptable per sub-PRD 05 FR-23.5).
4. Both PWAs install on Android Chrome (standalone, branded icon/name) and show the offline screen in airplane mode; iOS Safari shows the install education; SW update toast works across a redeploy.
5. Panel push: NAS-down alert arrives as a push notification on an installed panel PWA (Android).
6. IDOR pass: scripted attempt to read another subscriber's data via portal endpoints fails; portal login rate-limit verified.
7. B's ROS matrix: CoA scenario suite green on ROS 6.49 and 7.x targets; quirk table published at `docs/ops/ros-matrix.md`.
8. Auto-setup (C6): preview → apply against a CHR creates only additive HikRAD entries and a subsequent test auth succeeds; a planted conflicting `/radius` entry aborts the apply with the router unchanged; verified on both ROS matrix targets.
9. WhatsApp subscriber messaging (C7): a completed renewal delivers the receipt template to an opted-in test number in the subscriber's language, and the expiry reminder fires for a threshold-crossing subscriber — or, if Meta onboarding is pending, the full path is proven against a request-capture fake and documented as pending (voucher/panel flows unaffected either way, NFR-7).
10. Scratch-card flow (C8): expired subscriber submits a card code → 1-day trial active within 5 s (CoA-restored if online) + pending item via API; approve → expiry = trial start + profile duration; reject → net-zero ledger, subscriber expired again, status notification delivered; double-submit while pending rejected; codes absent from list payloads.

---
*Amended 2026-07-09 (pre-start, Decisions 16–17): C6 (FR-56 auto-setup, B + migrations 0320–0329), C7 (FR-55 subscriber WhatsApp, D event → C delivery), gate items 8–9. See master PRD Decisions Log.*
*Amended 2026-07-10 (pre-start, Decision 21): C2 `/me` reshaped — consumed data only, no quota total/remaining; `PUT /api/v1/portal/me` added (FR-44 promoted C→S: password/detail self-update); gate item 1 extended. See master PRD Decisions Log.*
*Amended 2026-07-11 (pre-start, Decision 22): C8 scratch-card payments (FR-59) — D backend + F portal UI + C status notifications; gate item 10; panel verification-queue UI deferred to Phase 5 (E). Execution-efficiency protocol applies (see [00-team.md](../00-team.md)); scriptable gate items (2, 6, 9-fake, 10) belong in `scripts/gate-phase-4.sh`.*
