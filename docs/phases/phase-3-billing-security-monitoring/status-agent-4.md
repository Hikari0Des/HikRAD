# Phase 3 — Agent 4 (Backend Business) status

**Done.** The money core is built and the scriptable gate legs (1-3, 5) pass on real Postgres/Redis.

Delivered (exclusive paths `internal/billing`, `internal/subscribers`, `internal/profiles`, migrations 0200–0204):
- **Migrations 0200–0204:** append-only `ledger_transactions` (trigger + REVOKE), `manager_balances` cache, `manager_low_balance_thresholds`, `payments` (+ `receipt_seq`, immutable) + frozen read-only `revenue_daily`, `voucher_batches`/`vouchers`, profile burst columns + `profile_tod_windows`, `renewal_idempotency`.
- **Renewal (C2, FR-19)** — one transactional path `renewInTx`; every source converges on it (panel/agent now, voucher redeem now, portal Phase-4). Anchor rule from settings, price override→profile, quota-cycle reset + `quota:reset` publish, `InvalidatePolicy`, CoA restore (MovePool+ApplyRate, NAK→Disconnect fallback surfaced in `coa_result`). Idempotency-Key honored (PK-reserve race-safe).
- **Balances (FR-20)** — ledger-DERIVED, cached & recomputed inside each txn; `GET balance`, `POST topup`; agents hard-enforced, admins per `billing.admin_balance_bypass`; insufficient → 422 `insufficient_balance`.
- **Vouchers (FR-22)** — batch gen (charge-at-generation, unambiguous alphabet, CSV plaintext only at gen, hashes stored), `SELECT…FOR UPDATE` single-use redeem → renewal, void-batch credits unused back, batch drill-down. B's hotspot `VoucherAuthenticator` seam wired (guest-session variant deferred to Phase 4 — safe no-op, never burns a code).
- **Receipts (FR-21)** — print-ready A5/thermal HTML, ar/en/ku, Eastern-Arabic numerals, shareable no-auth token; reprints create no entries.
- **Refunds (FR-25)** — reversing entry via `reverses_id` (unique → double-refund rejected), expiry rollback floored at now, reason required, re-applies FR-9 via CoA when now-expired.
- **Ledger (FR-24)** list+filters+keyset paging, streaming CSV export (`export` perm). Internal `GET /internal/stats/subscribers` for C.
- **Seams for B/C:** burst fields populated into `AuthView`; `radius.SetTODProvider` wired from profiles + window CRUD; expiry sweep publishes `enforce.expired` (C4).

Tests: `go build ./...` + `go vet` clean. Unit tests (anchor matrix, price/refund math, voucher entropy, receipt both locales) always run. DB suite: renew+ledger+receipt, balance-block+topup, **balance≡ledger property under concurrent renew/topup/refund**, **50-goroutine voucher double-redeem storm (exactly one wins)**, refund reversal, ledger immutability, idempotent renew. Gate legs appended to `scripts/gate-phase-3.sh`.

**Note for deploy:** migrations use golang-migrate's single high-water version — on a fresh DB (CI/prod at v135) 0200–0234 apply in numeric order, fine. A dev DB already advanced past 0204 skips them; reset or apply manually.

**Frozen decision recorded:** voucher accounting = charge-at-generation (per 00-phase C3). Handoff: Phase-4 portal/gateways plug into the same C2 path; Phase-5 reports read `revenue_daily`/ledger read-only.
