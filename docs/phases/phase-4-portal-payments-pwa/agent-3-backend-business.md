# Phase 4 — Agent 3 (Backend Business): portal API, payment gateway interface & adapters

> Owns FR-23, FR-41/42 (backend), FR-43 (branding/language backend). Depends on contracts in [00-phase.md](00-phase.md) (C1-D, C2, C3, C5 branding endpoint); parallel with Agents 1, 2, 4.

## Mission & context
Give Noor a backend: subscriber-scoped portal APIs (login with her PPPoE credentials, status/quota/speed/usage/payments) and the pluggable payment layer — the `PaymentGateway` interface, a full-lifecycle **mock** adapter (ships forever, powers CI/demo), and the first live Iraqi e-wallet adapter as merchant credentials allow (ZainCash-first default). Every confirmed payment converges on the Phase-3 renewal path. Payments must never block launch: voucher + manual paths already work. Detail sources: sub-PRDs [05](../../prd/05-billing-payments-vouchers.md) FR-23, [07](../../prd/07-subscriber-portal-pwa.md) §4.

## File ownership
- **Exclusive:** `backend/internal/portalapi/**`, `backend/internal/billing/**` (gateway packages under `billing/gateways/`), `backend/migrations/0300_*.sql`–`0309_*.sql`.
- **Read-only:** auth rate-limit mechanism (A), usage APIs (C), crypto (A). **Forbidden:** `internal/{radius,monitorsvc,push}`, `frontend/**`, `deploy/**`.

## Tasks
1. Migrations 0300–0309 per phase C1-D: portal_sessions, payment_intents, gateway_configs (creds sealed via A's crypto), subscriber language.
2. **Portal auth** per C2: login verifying the subscriber's sealed password through a narrow `VerifyPassword` helper in your subscribers package (uses A's crypto; cleartext never leaves the process/response), portal-appropriate token lifetimes (long refresh for phone PWA), rate-limited via A's mechanism (NFR-4.6), separate token audience from panel (a portal token must fail all panel/API-admin endpoints). [FR-41.1]
3. **Portal read APIs** per C2: `/me` composition (status, online-now from live data, days left, quota used/remaining incl. C8 exhaustion, profile + live speed), usage passthrough (C's API, self-scoped), payments history (own ledger slice), language preference. IDOR rule absolute: identity from token only. [FR-41]
4. **Portal renewal**: voucher redeem endpoint (wraps Phase-3 redeem, self-targeted); payment create/poll endpoints per C3. [FR-42]
5. **Gateway layer** per C3: the interface, intent lifecycle state machine (pending→confirmed→renewed / failed / expired with timeout), idempotent callback processing (unique on gateway_ref + state transition guard — replays and races cannot double-renew), reconciliation worker (QueryStatus for intents pending > 10 min, then hourly, expiring after 48 h), confirmed → Phase-3 renewal with source `portal-<gateway>`; per-gateway enable/config admin endpoints (settings UI is Phase 5's settings screen — expose the API now). [FR-23]
5b. **Renewal event** (phase C7, FR-55): publish `billing.renewed {subscriber_id, receipt_no, amount_iqd, new_expires_at, source}` on every completed renewal regardless of source (panel, voucher, portal-gateway) — exactly once per renewal, published after commit; C consumes it for WhatsApp receipts. Publish failure is logged, never blocks the renewal (NFR-7 posture).
6. **Mock adapter**: full lifecycle with a dev-only simulator endpoint (approve/fail/delay an intent) — used by the gate, CI, and F's development. **ZainCash adapter** (or the gateway with real credentials): implement per official spec — signature verification, amount/currency handling (IQD), sandbox notes; behind config, shippable disabled. [FR-23.5]
7. `GET /api/v1/branding` (public read: ISP name, colors, logo URL) per C5 for manifests/login pages.
8. Document each adapter's redirect/callback hosts in `billing/gateways/<gw>/README.md` — frozen input for B's walled-garden task at phase start (mock + ZainCash hosts day one).

Edge cases: payment confirmed for an already-renewed intent (idempotent no-op); subscriber renews via voucher while a payment intent is pending (intent stays valid, renews again on confirm — extend from new expiry; document); gateway callback with tampered amount → reject on signature and amount cross-check; portal token theft mitigations (rotate on password change; short access TTL); disabled subscriber can log in read-only but renewal offers only what policy allows.

## Contracts consumed/exposed
- **Consumes:** Phase-3 C2 renewal (own), A's crypto + rate limiting, C's usage API + quota flag.
- **Exposes:** C2 portal API + C3 payment routes (F), branding endpoint (F's manifests), adapter host docs (B), gateway admin API (Phase-5 settings UI).

## Definition of done
- Gate items 1 (API side), 2, 3, 6 pass: full mock lifecycle incl. replay-proofing and reconciliation; IDOR + rate-limit verified by scripted tests; live adapter checklist done or pending-credentials documented.
- Tests: token audience separation, /me composition against fixtures, intent state machine (all transitions + races via goroutine storm), callback signature/amount verification, reconciliation timing, VerifyPassword never logging/returning cleartext.

## Handoff
Phase 5 (same role) leaves payments alone (config-only adapter additions later); F builds the portal UI purely on these frozen endpoints; the settings screen (Phase 5, E) wires gateway enable/config.
