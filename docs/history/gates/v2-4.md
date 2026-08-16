# Phase v2-4 — Multi-Currency Billing — Integration Gate Result

Run date: 2026-07-17. Executed **solo + sequential** per Decision 25 / the v2
execution plan, in four reviewable chunks (schema+ledger / flows / display+
reports / gate) as the phase brief itself asked for, given the phase's size.

Verification environment: a throwaway **TimescaleDB (pg16) + Redis 7** pair
brought up specifically for this phase's DB-gated legs, dropped and recreated
to a fresh schema before every full-suite run (this repo's own documented
trap — a DB-gated legs suite proves nothing running against a database
another run already migrated). Docker Desktop was manually paused partway
through this session (see "Test-harness notes" below); all DB-gated legs were
re-run to completion, from a fresh database, after it was resumed.

`scripts/gate-v2-phase-4.sh`: **all scripted legs PASS, 0 FAIL.**

## Gate items 1–10

| # | Item | Result | Evidence |
|---|---|---|---|
| 1 | **Migration backfill on a seeded pre-phase-4 dataset** — zero row loss; every renamed column's value unchanged; every row `currency='IQD'`; `manager_balances`' new composite PK holds the same balance under `(manager_id,'IQD')` as the old PK held under `manager_id` alone | **PASS** | `TestCurrencyMigrationLossless` (`internal/billing/migration_v2p4_db_test.go`), mirroring v2 phase 1's own scratch-DB pattern: migrates to 0520 (last pre-phase-4 migration), writes v1-shaped rows (`profiles.price_iqd`, `ledger_transactions.amount_iqd`, `manager_balances.balance_iqd`, `voucher_batches.unit_price_iqd`, `payments.amount_iqd` — no currency column anywhere), migrates to 0538, asserts every value survived under its new name with `currency='IQD'`, every renamed column is gone, and the `currencies` catalog is seeded (IQD/USD/EUR). Forward-only per the migration-range note — no down leg tested (none exist). |
| 2 | **Per-currency reconciliation invariant (AC-69c)** — `balance(M,C) = sum(ledger.amount WHERE actor_manager_id=M AND currency=C)` holds for a manager with both an IQD and a USD balance; a deliberately-mutated `recomputeBalance` that sums across currencies must make this test fail | **PASS** | `TestPerCurrencyReconciliationInvariant` (`internal/billing/currency_gate_test.go`). **Mutation-checked for real, not just claimed**: during this gate run, `recomputeBalance`'s WHERE clause in `internal/billing/ledger.go` was temporarily changed to drop `AND currency = $2` from the inner sum — the test failed immediately and specifically (`IQD: cached balance 130000 != ledger sum 125000`, `USD: cached balance 105000 != ledger sum 5000`, both balances showing the two currencies' totals bled into each other), confirming the test actually detects the exact bug class AC-69c exists to catch. The mutation was then reverted (`git diff` confirmed byte-identical to before) and the full gate re-run green from a fresh database. |
| 3 | **Exchange pair (AC-69b)** — exactly two new ledger rows per exchange, correct signs, correct `currency_rate_id` stamped on both, correct minor-unit-adjusted `ToAmount`, both balances update by exactly the exchanged amounts, no other manager or currency is touched | **PASS** | `TestExchangePairCorrectness`: 100,000 IQD (0 minor-unit digits) exchanged at rate 0.00075 into USD (2 minor-unit digits) produces exactly 7,500 minor units (75.00 USD) via `pow10`-adjusted rounding; both legs share one `"EXG-"` reference and the same `currency_rate_id`; a EUR balance seeded on the same manager is asserted unchanged before and after. |
| 4 | **Non-IQD renewal (AC-69a)** — a USD-priced profile renewal debits only the agent's USD balance; their independently-seeded non-zero IQD balance is provably untouched; the receipt/payment row records `currency='USD'` | **PASS** | `TestNonIQDRenewalDebitsOnlyThatCurrency`: agent topped up $50.00 USD and 100,000 IQD independently; renewing a $25.00 profile leaves USD at $25.00 and IQD at exactly 100,000; `payments.currency='USD'` for the receipt. |
| 5 | **Refund reverses in the original currency, no re-resolution (AC-69d)** — a USD renewal's refund is a USD reversing entry at the original amount, even when the profile's price or the rate table has since changed | **PASS** | `TestRefundReversesOriginalCurrencyNoReResolution`: after a USD renewal, the profile's `price` AND `currency` are both mutated directly in the DB (to a nonsense price, currency EUR) before the refund is submitted; the reversing ledger row is still exactly `(2500, USD)` — the value read from the original entry via `reverses_id`, confirming `refund.go` never re-touches `profiles` for this. |
| 6 | **No online rate feed (AC-68b)** — grep leg asserting no HTTP client construction (`http.Client`, `http.Get`, `http.NewRequest` etc.) appears in the `currency_rates`-owning file; rate creation is reachable only through the authenticated `POST /currency-rates` handler | **PASS** | Dedicated grep leg in the gate script against `internal/billing/balance_api.go` (the file owning `currenciesHandler`/`listCurrencyRatesHandler`/`createCurrencyRateHandler`/`exchangeHandler`) — zero hits. |
| 7 | **formatMoney regression lock (AC-70a)** — `formatMoney(amount, 'IQD', locale, opts)` output byte-identical to `formatIQD`'s current test-suite expectations, for every case `format.test.ts` already covered, plus new USD/EUR cases | **PASS** | `frontend/shared/src/format/format.test.ts`'s new `describe('formatMoney (AC-70a regression lock + multi-currency)')` block asserts `formatMoney(amount, 'IQD', ...)` equals `formatIQD(amount, ...)` for all four of the pre-existing IQD cases (en, ar/arab-numerals, ar/latn-numerals, default-locale), plus new USD/EUR 2-decimal cases and an Eastern-Arabic-numerals USD case. 43/43 shared-package tests green. |
| 8 | **Migration lossless + build** — `go build`/`go vet` clean; the full pre-existing `internal/billing` test suite passes (every `*_iqd`-shaped assertion updated to the new field names, same values expected) | **PASS** | `go build ./...` / `go vet ./...` clean across the whole backend. Full `internal/billing` DB-gated suite green (`TestRenewWritesLedgerAndExtendsExpiry`, `TestBalanceBlockingAndTopup`, `TestBalanceEqualsLedgerProperty`, `TestVoucherDoubleRedeemRace`, `TestRefundReversalMath`, `TestLedgerImmutability`, `TestIdempotentRenew`, plus every other DB-gated package: `internal/auth`, `internal/profiles`, `internal/subscribers`, `internal/reports`, `internal/portalapi`, `internal/importer`, `cmd/hikrad-api`). The full backend suite (`go test ./...`) is green except one **pre-existing, unrelated** `internal/monitorsvc` flake (see "Bugs found" below), reconfirmed identical against the commit immediately before this phase's rename. |
| 9 | **Panel/portal** — build + lint + vitest green; `i18n:check` green (0 missing keys, 0 hardcoded strings) covering the currency selector(s), the exchange screen, and the rate-table admin screen | **PASS** | `frontend/shared`/`panel`/`portal` all build clean. ESLint 0 errors (pre-existing unrelated fast-refresh warnings only) + prettier clean across all three. **70 panel + 17 portal + 43 shared vitest** all green (0 new failures from the currency rework; `RenewModal.test.tsx` and `ledgerPairing.test.ts` updated for the renamed response fields). `i18n:check` green across en/ar/ku — 0 missing keys, 0 hardcoded strings — covering the new `/currency-rates` screen (rate-history table + exchange widget), the currency `<select>`s added to Profiles/Managers-topup/Settlement-report/Ledger-filter, and every other touched panel string. |
| 10 | **Docs accuracy** — PRD/sub-PRD 05/index reflect FR-68–70; `docs/ops/known-issues.md` carries any bug found while building, before or alongside its fix | **PASS** | `docs/PRD.md` carries FR-68/69/70 (added in the Step-1 docs commit, before any code, per the phase's own required sequencing). `known-issues.md` gained two **FIXED** rows (dashboard revenue tile, `agent_balance_low` alert — both found while making their queries currency-aware) and one reconfirmation of the pre-existing `internal/monitorsvc` WhatsApp-test row (see below). |

## GREEN / RED verdict

**GREEN — 10/10.**

## Bugs found and fixed (in `docs/ops/known-issues.md`)

**1. Dashboard "revenue today" tile always showed 0, on every install, since
it was built.** `revenueToday`'s candidate-column probing loop filtered on a
column named `day`; `revenue_daily` (migration 0201) has always named that
column `date`. Every candidate failed identically with the same
`isUndefinedTable`-shaped error the loop is designed to tolerate for a
genuinely missing amount column, so the bug never surfaced as an error — only
as a tile that was always exactly 0. Found while making the view
currency-aware (grouping it by `currency` too, migration 0537). Fixed:
`WHERE date = ...` + scoped to `currency = 'IQD'` (a blended-currency
dashboard number would be meaningless).

**2. The `agent_balance_low` alert rule has likely never fired on any
install.** `agentBalanceLow`'s query selected `name` from `manager_balances`
with no `JOIN` to `managers`, even though `manager_balances` has never had a
`name` column. The function's own "degrade cleanly while D's schema lands"
error handling silently no-oped on the resulting query error exactly as it
would for a genuinely-missing table, so nothing ever logged or alerted that
the rule itself was broken. Found while renaming `manager_balances.balance_iqd`.
Fixed: proper `JOIN` to `managers`, scoped to `currency = 'IQD'` (non-IQD
low-balance alerting is a future enhancement, not attempted here).

**3. A response-shape gap found during the panel call-site audit, not a
runtime bug**: the void-voucher-batch endpoint (`POST /vouchers/batches/{id}
/void`) returned `{"credit_iqd": N}` unconditionally, missing the `currency`
field every other money-bearing response in this phase gained. Fixed to
`{"credit": N, "currency": "..."}`, matching the batch's own `unit_price`/
`currency`. No test asserted the old key name, so this shipped with zero
observable symptom until the panel's `VouchersPage.tsx` was updated to read
it — caught before that mismatch could reach a build.

**4. `RenewModal`'s success screen read a `result.price_iqd` field the renew
endpoint has never actually returned** (the backend's `renewResult` struct has
only ever serialized `ledger_tx_id`/`receipt_no`/`new_expires_at`/`coa_result`
— `price`/`priceIQD` was always an internal, non-JSON field, even before this
phase). This is a pre-existing dead-code bug, not a regression, but the
currency rename forced fixing it in the same pass (the field was being
renamed anyway). Fixed by capturing the charged amount+currency **client-side**
at submit time, from the same price preview the operator already confirmed
before clicking Renew, rather than expecting it from a response that was
never going to carry it.

**5. `TestSubscriberEvents_RenewedDeliversWhatsAppReceipt` (unrelated
package, `internal/monitorsvc`) fails consistently** — reconfirmed during this
phase's full-suite pass, first flagged during v2 phase 3's gate. **Newly
confirmed this phase**: the failure is **not** caused by the `AmountIQD` →
`Amount`+`Currency` struct rename this phase makes to `renewedEvent`. Verified
by temporarily checking out the pre-phase-4 version of
`subscriber_events.go`/`subscriber_events_test.go` into the working tree and
re-running the test 3× — identical timeout, identical message, both before
and after the rename. Restored to the current (phase-4) version afterward
(`git diff` clean). Still not root-caused; the existing hypothesis (a fixed
`150ms` sleep racing a Redis pub/sub subscribe under load) stands unconfirmed.
Out of scope for this phase; belongs with whoever next touches
`internal/monitorsvc` or the WhatsApp/push path.

## Test-harness notes (for whoever runs this next)

- Same traps prior phases already documented: run `go test -p 1` against a
  **fresh** database, every time — this session hit the exact "stale
  `TestBalanceBlockingAndTopup` failure" false-positive the repo's own trap
  doc warns about, and it turned out to be exactly what the doc predicts: a
  test helper (`createProfile` in `internal/billing/db_test.go`) still posting
  the pre-rename JSON field `price_iqd`, which the server silently ignored
  (defaulting the profile's price to 0), making a balance-enforcement test
  pass for the wrong reason. Fixed alongside every other stale `*_iqd`
  reference found by actually running the suite rather than trusting
  `go build`/`go vet` (which cannot see inside raw SQL strings or JSON
  literals).
- **Docker Desktop was manually paused mid-session** (via the Whale
  menu/Dashboard "pause" feature, not stopped) — the CLI (`docker desktop
  start`) reports "already running" but cannot resume a manual pause; only
  the GUI can. All DB-gated gate legs that had not yet been verified when this
  happened were re-run to completion, from a freshly recreated database,
  after the user resumed it. No result reported in this document was
  fabricated or extrapolated while Docker was unavailable — the gate script
  itself refuses to report a DB-gated leg as PASS when it self-skips (a
  `SKIP` in the test output is reported as `FAIL` by `check_go_test`), so a
  premature run would have been visibly RED, not silently wrong.
- The `internal/billing` package now has its own scratch-database migration
  test (`migration_v2p4_db_test.go`), duplicating (not sharing) the
  `withScratchDB`/`migrator` helpers from `internal/subscribers/
  migration_v2p1_db_test.go` — Go test helpers do not cross package
  boundaries between `_test` packages, so this is the second copy of that
  pattern in the repo. Worth factoring into a shared internal test-helper
  package if a third phase needs it.

## Deviations from the brief

- **The exchange + rate-table screen is one combined page**
  (`frontend/panel/src/pages/billing/CurrencyRatesPage.tsx` at
  `/currency-rates`), not two separate screens. The phase brief's gate item 9
  says "the exchange screen, and the rate-table admin screen" without
  specifying separate routes; combining them lets an operator see the rate
  they're about to use right next to the exchange form, and the rate-history
  table right below where a new rate is entered — judged clearer than forcing
  a navigation round-trip between the two for what is, in practice, one
  workflow (an admin/agent occasionally reconciling a foreign-currency
  balance). Gated by `PERM_TOPUP` to view/exchange (a manager's-own-money
  operation, matching C4's stated permission) and the new
  `PERM_CURRENCY_RATES_MANAGE` (`currency_rates.manage`) to submit a new rate.
- **Not every one of the ~39 `IQDAmount`/`formatIQD` call sites was migrated
  to a real, row-sourced currency.** Per the phase brief's own explicit
  instruction ("do not blanket-replace without checking each call site's data
  source"), several were deliberately left on the `currency='IQD'` default
  because their underlying data genuinely has no currency yet in this phase's
  scope: the header `BalanceWidget` (C7 sanctions this explicitly — "omitted
  currency defaults to IQD for the header-widget call sites that don't yet
  carry currency context"), the plain-text daily digest headline
  (`digest.go`, commented in-code as deliberately IQD-scoped — a blended
  figure would be meaningless in a plain-text summary), and the dashboard
  revenue tile (same reasoning, same file as bug #1 above). Every call site
  whose backing row actually gained a `currency` column — profiles, ledger,
  vouchers, card payments, manager balances (outside the header widget),
  revenue/settlement reports — was migrated.
- **`subscribers.price_override` stays a bare integer, per C6's explicit
  freeze** ("an override's currency is always the profile's current
  currency; overriding into a different currency is out of scope for this
  phase"). The panel label was changed from the now-inaccurate "Price
  override (IQD)" to "Price override" with a hint ("In the profile's own
  currency") rather than leaving a misleading currency claim in the UI.

## Human/hardware legs (documented-pending)

None — per the phase brief's own note: "this phase has no router/device
dependency." The only "documented-pending" candidate the brief itself names
is a live pilot ISP actually pricing a plan in USD/EUR end-to-end, which is
an operational rollout step, not a gate item this brief can script.
