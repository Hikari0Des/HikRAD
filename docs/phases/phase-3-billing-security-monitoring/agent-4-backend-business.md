# Phase 3 — Agent 4 (Backend Business): renewals, ledger, balances, receipts, vouchers, refunds

> Owns FR-19, FR-20, FR-21, FR-22, FR-24, FR-25, FR-11 (fields/config). Depends on contracts in [00-phase.md](00-phase.md) (C1-D, C2, C3, C4); parallel with Agents 1–3, 5.

## Mission & context
The money core of sub-PRD [05-billing-payments-vouchers](../../prd/05-billing-payments-vouchers.md): a single atomic renewal path every source converges on (panel now; voucher now; portal/e-wallet in Phase 4), an append-only ledger balances are *derived* from, agent top-ups (persona Hassan's whole workflow), printable Arabic/English receipts, voucher batches with race-proof single use, and refunds as reversing entries. Money invariants are absolute: no edits, only entries.

## File ownership
- **Exclusive:** `backend/internal/billing/**`, `backend/internal/subscribers/**`, `backend/internal/profiles/**`, `backend/migrations/0200_*.sql`–`0209_*.sql`.
- **Read-only:** CoA service (B), auth middleware/audit (A), settings (anchor rule, receipt branding). **Forbidden:** `internal/{monitorsvc,accounting,radius,auth}` internals, `frontend/**`.

## Tasks
1. Migrations 0200–0209 per phase C1-D: ledger (with DB-level REVOKE UPDATE/DELETE), payments + receipt sequence, voucher batches/vouchers, profile burst/TOD fields; plus frozen read-only view `revenue_daily` (date, source, amount) for C's dashboard. [FR-24, FR-11 fields]
2. **Renewal** per C2 exactly — one transactional code path with source tagging; anchor rule from settings (`from_expiry|from_now` when active — sub-PRD 05 FR-19.1); price resolution override→profile; quota-cycle reset; `InvalidatePolicy`; CoA restore call with NAK→Disconnect fallback surfaced in the response. Expiry sweep (your Phase-2 job) now also publishes `enforce.expired` per C4. [FR-19]
3. **Balances** (FR-20): ledger-derived with cached materialization (`balance_iqd` recomputed on each entry, verified by property test); top-up endpoint (permission `topup`, audited); insufficient-balance renewal → 422 with localized error key; admin bypass per settings flag; low-balance thresholds table read by C's `agent_balance_low` rule.
4. **Receipts** (FR-21): sequential receipt_no (settings prefix), print-ready HTML endpoint per C3 (A5 + thermal CSS, branding, ar/en templates, Eastern-Arabic numerals per locale), shareable link token.
5. **Vouchers** (FR-22): batch generation (≤ 10k, crypto-random unambiguous alphabet, ≥ 10 chars, prefix; plaintext CSV only in the generation response; hashes stored), redeem endpoint with `SELECT … FOR UPDATE` single-use guarantee, charging **at generation** per the frozen C3 decision (batch creation debits creator's balance; void-batch of unused codes credits back), batch list with used/unused/expired drill-down, internal redeem API for B's hotspot voucher login. [FR-22]
6. **Refunds** (FR-25): reversing entry linked via `reverses_id`, expiry rollback (floor now), reason required, permission-gated, audited; re-applies expiry behavior via CoA if now expired.
7. Profile burst/TOD config CRUD (fields consumed by B's adapter/sweeps). [FR-11]
8. Subscriber counts endpoint (active/expired/expiring-N) for C's dashboard/digests — frozen shape `GET /internal/stats/subscribers`.

Edge cases: renewal during concurrent profile archive (reject cleanly); double-submit renewal (idempotency key header honored); voucher redeemed for a subscriber of another owner (scoping: redeeming manager needs subscriber visibility); refund of an already-refunded tx rejected; ledger export streaming (10k+ rows) under `export` permission; receipt reprint must not create new entries.

## Contracts consumed/exposed
- **Consumes:** C5 CoA (B), C2/C3 auth+crypto (A), settings; C8-Phase-2 quota flag (cycle reset clears it — coordinate via a `quota:reset:<id>` publish C consumes, frozen here).
- **Exposes:** C2 renewal + C3 balance/voucher/receipt APIs (E now, F in Phase 4), `revenue_daily` + stats (C), internal redeem (B), `enforce.expired` events (B).

## Definition of done
- Gate items 1–3 pass end to end (renew-with-CoA-restore, balance blocking, race-proof voucher).
- Property tests: balance ≡ ledger sum under randomized concurrent renewals/topups/refunds; ledger immutability at DB level; single-use under 50-goroutine redeem storm.
- Unit tests: anchor-rule matrix (active/expired × from_expiry/from_now), price resolution, refund rollback math, receipt rendering both locales, batch generation entropy/format.

## Handoff
Phase 4 (same role) plugs portal + payment gateways into the same C2 renewal path; Phase 5 reports read only your ledger/views; Hassan's settlement report (Phase 5) is a ledger slice.
