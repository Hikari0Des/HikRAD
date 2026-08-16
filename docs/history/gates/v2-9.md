# Phase v2-9 — Cost, Margin & Reseller Pricing — Integration Gate Result

Run date: 2026-07-17. Executed **solo + sequential** per Decision 25 / the v2
execution plan, immediately after v2-4 (multi-currency), same money core, per
the execution plan's explicit sequencing. Three kickoff blockers (sub-reseller
nesting, per-site overhead scope, per-subscriber wholesale overrides) were
resolved by the owner before any code — see PRD Decision 36 and this phase's
own `00-phase.md` header.

Verification environment: the same throwaway **TimescaleDB (pg16) + Redis 7**
pair used for v2-4, dropped and recreated to a fresh schema before every
full-suite run.

`scripts/gate-v2-phase-9.sh`: **all scripted legs PASS, 0 FAIL.**

## Gate items 1–11

| # | Item | Result | Evidence |
|---|---|---|---|
| 1 | **Cost resolution + unknown-cost safety (AC-71a)** — a profile with no cost ever recorded reports margin as unknown, never zero/100%; a cost changed after a renewal doesn't retroactively change that renewal's already-stamped `cost_at_sale` | **PASS** | `TestUnknownCostStampsNilNeverZero` (nil, never 0, when no cost recorded) + `TestKnownCostStampsCorrectly` (`internal/billing/pricing_v2p9_gate_test.go`) — the latter re-prices the plan *after* the renewal and re-reads the same ledger row, confirming the stamp is immune to the later change. |
| 2 | **Margin reconciliation (AC-72a)** — `sum(margin) = sum(amount) - sum(cost_at_sale)` over rows with a known cost; a row with unknown cost still counts toward revenue, excluded from margin | **PASS** | `TestMarginReconciliation` (`internal/reports/margin_gate_test.go`): two profiles renewed in the same window, one with a recorded cost (asserts `owner_margin = wholesale - cost`, `unknown_cost_count = 0`) and one without (asserts `cost`/`owner_margin` both nil, `unknown_cost_count = 1`, but `revenue` still counted in both). |
| 3 | **Per-site overhead isolation (AC-73a)** — a global overhead and a per-site overhead in the same period; the per-site net-margin figure nets only its own tagged overhead, never a pro-rated share of the global one | **PASS** | `TestSiteMarginNeverBlendsGlobal`: a NAS with a 5,000 IQD site overhead and a 100,000 IQD global overhead in the same period — the site's `net_margin` is `revenue - 5000` exactly, and `global_overheads` is reported as the separate 100,000 figure, never merged in. |
| 4 | **Retail unaffected by wholesale (AC-74a)** — a reseller with no `reseller_prices` row debits exactly the retail price, byte-identical to pre-v2-9 | **PASS** | `TestRetailUnaffectedByNoResellerPrice` (`internal/billing/pricing_v2p9_gate_test.go`). |
| 5 | **Per-subscriber override beats plan-wide (AC-74b)** — a reseller with both a plan-wide and a per-subscriber wholesale price debits the per-subscriber price for that one subscriber and the plan-wide price for every other subscriber; both subscribers are always charged retail | **PASS** | `TestPerSubscriberOverrideBeatsPlanWide`: plan-wide 20,000 + a 15,000 per-subscriber override on one subscriber — that subscriber's renewal debits 15,000, the other subscriber's debits 20,000, and both subscriber receipts show the full 25,000 retail price regardless. |
| 6 | **Reseller-facing scoping never leaks (AC-75a)** — a reseller-scoped call to the margin endpoint never contains `cost`/`owner_margin`/`unknown_cost_count`, verified by response-shape inspection | **PASS** | `TestResellerScopingNeverLeaksCost` (`internal/reports/margin_gate_test.go`) — unmarshals into a raw `map[string]any` and asserts the forbidden keys are absent (not null) from every row. **Mutation-checked for real**: during this gate run, the `!scoped` guard in `margin.go` was temporarily changed to always populate the owner-only fields — the test failed immediately, printing exactly the leaked row (`cost`, `owner_margin`, `unknown_cost_count` all present with real values) — confirming the test detects the exact commercial-severity leak class FR-75 exists to prevent. Reverted (`git diff` confirmed byte-identical) and the full gate re-run green from a fresh database. |
| 7 | **Independent leg resolution (AC-76a)** — a subscriber with an active `price_override` belonging to a reseller with a wholesale price configured is charged their override exactly; the reseller's balance still debits the resolved wholesale price; the two numbers never conflate | **PASS** | `TestPriceOverrideIndependentOfWholesale`: subscriber override 18,000, reseller wholesale 20,000, plan retail 25,000 — the subscriber's receipt shows 18,000 and the reseller's balance drops by exactly 20,000, proving all three numbers stay independently resolved in the same transaction. |
| 8 | **No sub-reseller tree exists in code** — the resolved kickoff blocker (flat 2-level only) never leaked a hierarchy/ancestry column into the schema | **PASS** | Two grep legs in the gate script: no v2-9 migration issues an `ALTER TABLE managers` or names any `parent_manager`/`parent_reseller`/ancestor-shaped column; none of the three new tables carry a self-referential FK. `reseller_prices.manager_id` is a plain FK to `managers(id)` — the reseller themselves, never a link to another reseller. |
| 9 | **Build + full regression** — `go build`/`go vet` clean; the full pre-existing `internal/billing`+`internal/reports` DB-gated suites (including v2-4's own gate tests) pass unchanged — renewal/refund/exchange/topup stay byte-identical when no cost/overhead/reseller-price data exists | **PASS** | Both full pre-existing suites green, run twice: once immediately after the backend implementation (before any test files for this phase existed) and again as part of the final gate script run. No existing test's expected values needed adjustment — the "byte-identical when unconfigured" guarantee C5/C6 promised held on the first attempt, not after a fix. |
| 10 | **Panel/portal** — build + lint + vitest green; `i18n:check` green covering the cost-entry screen, overheads screen, reseller-pricing screen, and margin report | **PASS** | `frontend/shared`/`panel` build clean, ESLint 0 errors (pre-existing unrelated fast-refresh warnings only) + prettier clean. **70 panel + 43 shared vitest** green (0 new failures — this phase added no new component test files; see "Deviations"). `i18n:check` green across en/ar/ku — 0 missing keys, 0 hardcoded strings — covering the new `/pricing-admin` screen (overheads + reseller-pricing sections), `/reports/margin` screen, and the `ProfilesPage` cost modal. |
| 11 | **Docs accuracy** — PRD/sub-PRD 05 reflect FR-71–76; `docs/ops/known-issues.md` carries any bug found while building | **PASS** | `docs/PRD.md` carries FR-71–76 and Decision 36 (added in the Step-1 docs commit, before any code, per this phase's own required sequencing — and before the owner's kickoff blockers were even answered, then updated once they were). Sub-PRD 05 and the index updated to 76/76 FR coverage. **No new bug found while building this phase** — see "Bugs found" below. |

## GREEN / RED verdict

**GREEN — 11/11.**

## Bugs found and fixed

**None.** Unlike v2-4 (which found two real pre-existing bugs while making
`monitorsvc` currency-aware), this phase's build surfaced no new defects in
existing code. The one thing worth recording as a **design decision, not a
bug**: C5's open question (how the reseller wholesale debit lands on the
ledger) was resolved as "thread a different resolved number into the
already-decoupled `ledger_transactions.amount` vs. `payments.amount` split"
rather than adding a second ledger row or a new ledger `type` — see
"Deviations" below for the reasoning, since the phase brief explicitly left
this open for the implementer to decide and document.

## Test-harness notes (for whoever runs this next)

- Same traps prior phases already documented: run `go test -p 1` against a
  **fresh** database, every time. No new trap discovered this phase — the
  `internal/billing`/`internal/reports` DB-gated suites already had every
  `*_iqd`-shaped stale reference cleaned up during v2-4, so this phase's new
  test files (`pricing_v2p9_gate_test.go`, `margin_gate_test.go`) started
  from a clean baseline with nothing to fix before they could even compile.
- Docker Desktop's earlier mid-session pause (documented in v2-4's own gate
  result) did not recur during this phase — the whole phase, from schema
  through the final gate run, executed against one continuously-available
  Postgres/Redis pair.

## Deviations from the brief

- **C5's reseller-debit ledger shape, resolved**: a single ledger row per
  renewal (unchanged shape from pre-v2-9), with `ledger_transactions.amount`
  now fed the *resolved wholesale price* (when one applies) instead of
  always the retail price, while `payments.amount` — already a separate,
  decoupled table recording the customer-facing gross charge — keeps
  recording retail unconditionally. This satisfies FR-74.3's "explicit pair,
  never one row with two meanings" requirement using the exact
  decoupled-by-design shape the renewal path already had (the code's own
  pre-existing comment calls `payments` "gross revenue recorded here,
  decoupled from balance") — feeding it two different resolved numbers
  instead of one number feeding both, rather than adding a second
  `ledger_transactions` row or a new ledger `type` (which the brief floated
  as the alternative). No new ledger `type` was added; the `type` CHECK
  constraint is unchanged from v2-4. Locked by AC-74a/AC-74b's tests, which
  verify both numbers (reseller balance debit, subscriber receipt amount)
  independently and would fail under either implementation choice if either
  number were wrong — the tests do not assume row count, only correctness of
  each resolved figure.
- **No new panel component test files** — same posture as v2-4's own gate
  result: the existing 70-test panel suite (unchanged) already satisfies
  "vitest green," and this phase's UI (`PricingAdminPage`, `MarginReportPage`,
  the `ProfilesPage` cost modal) is validated by TypeScript build + ESLint +
  `i18n:check` + the backend's own contract tests (which the panel calls
  through unchanged API client functions) rather than new RTL component
  tests — judged lower-value than the backend DB-gated coverage for screens
  whose real risk (wrong resolution order, a scoping leak) is already locked
  server-side. Flag if this should be revisited.
- **Overheads/reseller-pricing combined on one screen** (`PricingAdminPage`),
  and the margin report combined with the per-site report on one screen
  (`MarginReportPage`) — same reasoning as v2-4's `CurrencyRatesPage`: both
  pairs are naturally one workflow for the admin operating them, and the
  phase brief's gate item 10 doesn't mandate separate routes.

## Human/hardware legs (documented-pending)

None — per the phase brief's own note, mirroring v2-4: this phase has no
router/device dependency. Per-site overhead attribution reads existing
session/NAS data already in the database; it never probes a router.
