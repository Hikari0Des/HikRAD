# Phase v2-2 — Manual Payment Providers (Transfer-Proof Payments) — Integration Gate Result

Run date: 2026-07-18. Executed **solo + sequential** per Decision 25 / the v2
execution plan, re-ordered ahead of v2-6/v2-5/v2-7 by explicit owner choice
(this phase's own kickoff prioritized it over "preferences next, per
documented build order") once v2-4 (multi-currency) and v2-9 (cost/margin/
reseller pricing) — both hard dependencies of C7's wholesale-aware approval —
were complete. Two kickoff blockers (no-account fallback = show nothing; the
Phase-4 `PaymentGateway` interface removed entirely, not quarantined) were
resolved by the owner before any code — see PRD Decision 37 and this phase's
own `00-phase.md` header.

Verification environment: a long-lived **TimescaleDB (pg16) + Redis 7**
docker pair (`hikrad-test-pg` / `hikrad-test-redis`), migrated fresh at the
start of each DB-gated test via `platform.Migrate`; the migration-losslessness
test additionally spins up its own throwaway scratch database per run
(`ticketsWithScratchDB`, same pattern as v2 phase 1's `TestServiceType-
MigrationLossless`).

`scripts/gate-v2-phase-2.sh`: **all scripted legs PASS, 0 FAIL.**

## Gate items 1–12

| # | Item | Result | Evidence |
|---|---|---|---|
| 1 | **Lossless `card_payments` → `payment_tickets` migration** — every pre-migration row (pending/approved/rejected) survives with the right `method_key`/`method_detail`/state/decision fields, one synthesized `submitted` event per row, `card_payments` gone | **PASS** | `TestCardPaymentsMigrationLossless` (`internal/billing/tickets_migration_v2p2_db_test.go`): migrates a scratch DB to 0585, writes three v1-shaped `card_payments` rows (one per state) each with a real trial ledger row **and** its linked `payments` row (the source 0586 actually reads amount/currency from — not the 0-amount ledger row), migrates to 0588, and asserts all three survived with correct `method_detail` (card_type + base64 `card_code_enc`), correct amount/currency (from the `payments` row, proving the join direction is right), correct `reject_reason` presence, and exactly one `submitted` timeline event each. |
| 2 | **Trial granted on first attempt (AC-78a)** | **PASS** | `TestTrialGrantedOnFirstAttempt`: a fresh subscriber's first scratch-card submission grants a trial within the existing ~1-day FR-59.1 tolerance and extends `subscribers.expires_at` in the DB. |
| 3 | **No trial on post-rejection retry, reset on approval (AC-78b)** | **PASS** | `TestNoTrialOnRetryResetOnApproval`: submit → reject → resubmit (accepted as `pending`, `TrialGranted=false`, no expiry change) → approve → resubmit again (`TrialGranted=true`) — the full three-submission cycle the AC describes, not just the two halves separately. |
| 4 | **Owner-scoping + admin-sees-all (AC-79a)** | **PASS** | `TestOwnerScopingAdminSeesAll`: two scoped agents (each granted `payment_tickets.verify` via a permission override — the builtin agent role never carries it) each own one pending ticket; agent A's `scope=mine` and `scope=all` both return only their own ticket (proving the downgrade is silent, not a 403); admin's `scope=all` sees both agents' tickets. |
| 5 | **Wholesale-aware approval (AC-79b)** | **PASS** | `TestWholesaleAwareApprovalTicket`: a reseller-priced subscriber's ticket, approved by the owning (verifier-granted) agent, debits the resolved wholesale price (20,000) from the agent's balance while the subscriber's own `payments` row still shows full retail (25,000) — the exact same `renew()` split v2-9's own gate already proved, now reached through `approveTicket` instead of a direct panel renewal. |
| 6 | **Both-sides notifications (AC-80a)** | **PASS** | `TestBothSidesTicketNotifications`: subscribes to `billing.payment_ticket` before acting, then asserts the `submitted` message carries `owner_manager_id` (so `monitorsvc` can notify the owning agent without a second DB round-trip) and the `approved` message (decided by admin, a different manager than the owner) carries `decided_by` — exercising the exact "decided by someone else" branch `notifyManagerTicket` gates on. Also asserts `payment_ticket_events` carries exactly the `submitted`+`approved` rows the notifications traced to (FR-80.3). |
| 7 | **Attachment authorization** | **PASS** | `TestAttachmentAuthorization`: submits a ticket with one real-PNG-signature attachment (`net/http`'s sniffer must independently agree it's `image/png` — the upload path never trusts a client-declared content type); the owning scoped manager and an unscoped admin both fetch it (200), a *different* scoped manager gets 403, and the response always carries `Content-Disposition: attachment; filename="proof.png"` (checked via a raw header read, since the shared `e.do` test helper discards headers by design). |
| 8 | **No-account fallback never leaks a method (AC-77a, kickoff blocker 1)** | **PASS** | `TestNoAccountFallbackNeverLeaksMethod`: a manager enables a provider (`manager_method_settings.enabled=true`) but has no `manager_provider_accounts` row for it — the provider never appears in `billing.ResolvePayMethods`'s result for that manager's subscriber; configuring the account afterward makes it appear, proving the absence really was the account gate and not some other reason. **Mutation-checked for real**: the guarding `JOIN manager_provider_accounts` in `method_settings.go` was temporarily changed to a `LEFT JOIN` — the test failed immediately, printing the leaked provider id — confirming the test detects the exact kickoff-blocker-1 violation it exists to prevent. Reverted and the full gate re-run green from the live database. |
| 9 | **Gateway surface fully removed (C12)** | **PASS** | Four grep legs in the gate script: no `PaymentGateway` symbol anywhere in the tree; no live SQL against `card_payments`/`payment_intents`/`gateway_configs` outside the one deliberate migration-losslessness test (excluded by name, since it must recreate the pre-migration schema to prove the migration); `internal/billing/gateways/` (including the mock/zaincash packages **and** their README stubs, caught during cleanup — `git rm` had only removed the `.go` files, leaving orphaned doc files behind) is gone entirely; no panel gateway-config screen or API client reference survives. |
| 10 | **Build + full regression** | **PASS** | `go build`/`go vet` clean throughout. Full pre-existing `internal/billing` DB-gated suite (17 tests, including v2-4's and v2-9's own gate tests) green unchanged. Full pre-existing `internal/reports` DB-gated suite green **when run in isolation**; one test (`TestRevenueReportReconcilesWithPayments`) is flaky when run immediately back-to-back after the full billing suite on the shared, never-reset test database — diagnosed as a pre-existing narrow-wall-clock-window fragility unrelated to this phase (v2-2 touches neither `payments`, `revenue_daily`, nor the reports package; the same test passes in isolation both before and after this phase's changes) and logged in `docs/ops/known-issues.md`, not silently worked around. |
| 11 | **Panel/portal** | **PASS** | `frontend/shared`/`panel`/`portal` all build clean. Panel ESLint 0 errors (10 pre-existing unrelated fast-refresh warnings only) + prettier clean; **70 panel + 14 portal vitest** green, including a rewritten `renewFlow.test.tsx` (portal) covering the unified Pay screen's voucher/provider/empty-state paths. `i18n:check` green across en/ar/ku — 0 missing keys, 0 hardcoded strings (one caught and fixed mid-gate: a hardcoded `"PDF"` fallback label in the panel's attachment thumbnail) — covering the provider catalog, my-accounts/methods, the tickets queue/log + detail/timeline/attachment viewer, and the portal's unified Pay screen. |
| 12 | **Docs accuracy** | **PASS** | `docs/PRD.md` carries FR-77–80, Decision 37, and FR-23 marked retired in place (done in this phase's Step-1 docs commit, before any code). Sub-PRDs 05/07 and the index updated to 80/80 FR coverage. `docs/ops/known-issues.md` carries two new rows: the reports-suite narrow-window flake (item 10, above) and this phase's own bug — see "Bugs found" below. |

## GREEN / RED verdict

**GREEN — 12/12.**

## Bugs found and fixed

- **`revealCardPaymentHandler` had no C1–C14 equivalent in the frozen
  contracts.** The phase brief's C7 says approve/reject are "`cardpay.go`'s
  `approveCard`/`rejectCard` generalized... same... logic, unchanged," but
  never mentions the *third* admin action `cardpay_api.go` shipped: decrypting
  and revealing a scratch-card's code so an agent can check it against the
  physical card. Deleting `cardpay.go`/`cardpay_api.go` wholesale (per C12's
  gateway-removal instruction, which does NOT distinguish "gateway code" from
  "scratch-card code" within that file) would have silently dropped this
  capability with no test anywhere failing, since nothing in the frozen
  contracts names it. Caught by re-reading the deleted file's full contents
  before finalizing the delete, not by any test. Fixed: `POST /payment-
  tickets/{id}/reveal` (`internal/billing/tickets_api.go`), decrypting
  `method_detail.card_code_enc` the same way the retired handler decrypted
  `card_payments.card_code_enc`, audited per-call exactly as before. Wired
  into the panel's ticket detail modal.
- **`internal/billing/gateways/mock/README.md` and `.../zaincash/README.md`
  survived their packages' deletion.** `git rm` on `mock.go`/`zaincash.go`
  left the two README files behind (a plain `rm *.go` misses non-`.go`
  files in the same directories); the C12 grep leg would have reported a
  false "removed" if it had only grepped for Go symbols. Caught by explicitly
  listing the directory contents after the code deletion, before trusting the
  grep leg. Fixed: `git rm -r internal/billing/gateways`.
- **`manager_method_settings`'s "absence is off" default (C3, by design)
  would have silently disabled voucher redemption and scratch-card payments
  for every existing manager on upgrade day.** Pre-v2-2, voucher redemption
  had no manager-level gate at all and scratch-card payments were accepted
  globally (`card_payments.types` was one settings row, not per-manager).
  Migration 0582 correctly has no backfill per its own doc comment (the
  frozen "no row = off" rule), but applying that rule retroactively to
  managers who never opted into anything is a real functional regression on
  `hikrad update`, not a neutral default — every subscriber on every existing
  install would see an empty Pay screen the moment this migration ran, until
  their manager manually visited the new My Payment Methods screen. Caught
  while writing the frontend and reasoning about what a fresh v1.1 → v2
  upgrade actually looks like, before any deployment. Fixed: migration 0588
  backfills `enabled=true` for `voucher` and `scratch_card` for every
  existing manager row, preserving pre-migration behavior exactly; only a
  manager created *after* this migration sees the absence-is-off default in
  practice.

## Test-harness notes (for whoever runs this next)

- Same fresh-DB discipline as every prior phase: `go test -p 1`, real
  Postgres/Redis. This phase's migration-losslessness test additionally needs
  `CREATEDB` privilege on the test role (same requirement v2 phase 1's own
  scratch-DB test already established) — already granted on the shared
  `hikrad-test-pg` container.
- **New trap this phase**: running `internal/billing`'s full DB-gated suite
  immediately before `internal/reports`'s in the same invocation can flake
  `TestRevenueReportReconcilesWithPayments` (see gate item 10, and
  `docs/ops/known-issues.md`). Run `internal/reports` on its own, or accept a
  brief pause between packages, if this reproduces.
- `HIKRAD_PAYMENT_ATTACHMENTS_DIR` defaults to `data/payment-attachments`
  relative to the process's working directory (mirrors `hikrad-acct`'s
  `HIKRAD_ACCT_SPILL_DIR` pattern, resolved once via `sync.Once`) — the
  attachment-authorization gate test cleans this directory up itself
  (`t.Cleanup`) since it runs from `backend/internal/billing` and would
  otherwise leave a stray directory in the working tree.

## Deviations from the brief

- **`0587_drop_gateway_surface` also drops `gateway_configs`**, not just
  `payment_intents` as the brief's migration table literally named — folded
  in because both fall under "the gateway surface, fully removed" (C12) and
  `gateway_configs` (migration 0303) has no FK dependents; the brief itself
  anticipated this class of implementer's-call scope broadening ("document
  it"). Renamed from the brief's suggested `0587_drop_payment_intents` to
  reflect the broader scope.
- **The reveal endpoint (`POST /payment-tickets/{id}/reveal`)** — see "Bugs
  found" above; not in C1–C14 but required to avoid a silent functionality
  regression, documented here as the deviation it is.
- **Migration 0588 (`manager_method_settings_backfill`)** — see "Bugs found"
  above; outside the brief's 0580–0589 budget table but within the reserved
  0588–0589 tail the brief itself set aside for "follow-ups discovered
  during build."
- **"My accounts + methods" is an ungated top-level route
  (`/my-payment-methods`), not nested under Settings.** C14 says this screen
  is for "every manager, not just admins," but the existing `/settings/*`
  route is gated behind `settings.view` (an admin-class permission most
  agents don't hold). Placed as a sibling of the existing ungated
  `/account`/`/license` self-service routes instead, which is the repo's own
  precedent for "every authenticated manager, regardless of role" screens.
- **The provider catalog lives under `/payment-providers` (billing area),
  not Settings** — mirrors `CurrencyRatesPage`'s existing precedent (a
  `payment_providers.manage`-gated admin screen reachable from the sidebar's
  Billing group, not folded into the Settings tab strip the retired
  `GatewaySettings` tab used to occupy).
- **The portal's unified Pay screen mixes all three `PayMethod.Kind` values
  (`provider`/`scratch_card`/`voucher`) into ONE tile list** — re-reading C4's
  `PayMethod.Kind` enumeration and C13's "one picker, all enabled methods as
  tiles" together, rather than keeping voucher on its own separate top-level
  tab (the v1 shape `RenewPage` had). Tapping the voucher tile still runs the
  pre-existing instant-redeem flow unchanged (`VoucherPanel`/`RedeemVoucher`)
  — a voucher code is self-verifying and was never a ticket, so `submitTicket`
  itself has no `voucher` case; only the *visibility* of the tile is now
  gated through `manager_method_settings`, consistent with every other
  method.
- **No new panel/portal component test files beyond `renewFlow.test.tsx`'s
  rewrite** (mirrors v2-4's/v2-9's own gate-result posture): the backend's
  DB-gated suite is the primary correctness guarantee for the money-adjacent
  logic (trial timing, scoping, wholesale resolution, notification content),
  with the frontend covered by TypeScript build + ESLint + `i18n:check` +
  the one rewritten test file that specifically locks the new unified-picker
  UI shape (tile selection, provider transfer-proof submission, the
  no-methods empty state).

## Human/hardware legs (documented-pending)

None scriptable — per the phase brief's own note. The closest is real-device
camera-capture behavior for the portal's attachment file input (a subscriber
photographing a transfer receipt on their own phone); worth a manual pass on
real hardware during the pilot, not attempted here.
