# Phase v2-3 — Full Multi-Currency Billing (IQD / USD / EUR)

> Goal: replace v1's implicit-IQD money core with a real multi-currency one. Every monetary column becomes an integer **minor-unit** amount plus a `currency` code on the same row; manager/agent balances become **per-currency** with **no implicit conversion** — moving value between a manager's currencies is an explicit, ledger-visible **exchange** stamped with the admin-maintained rate used. No online rate feed (NFR-7 holds). This is the single largest rework of the money core since Phase 3 froze it — treat every contract below as touching real financial data, not a cosmetic rename.
>
> Requirements: **FR-68/69/70** (all owned by sub-PRD 05 — no split). Master PRD Decision 34. Owner: **SOLO + sequential** (Decision 25) — the C-numbers below are *contracts*, not agents.
>
> **This phase is immediately followed by v2-3b** (cost/margin/reseller pricing, `docs/v2/09-cost-margin-and-reseller-pricing.md`) against the **same** money core, so the ledger is migrated once, not twice — do not consider this phase "done, move on" until v2-3b's own kickoff has at least been read.

## Execution ordering (one session, by dependency)

1. **Schema + ledger core** — migrations 0530–0539 (below); `internal/billing/ledger.go` (`ledgerEntry.AmountIQD`→`Amount`+`Currency`, `insertLedger`, `lockBalance`, `recomputeBalance` all become currency-aware); the immutability trigger/REVOKE are untouched (already generic).
2. **Money flows** — `renew.go` (profile currency threads through), `voucher.go` (batch currency), `refund.go` (reverses in the original entry's own currency, no re-resolution), `receipt.go` (prints currency), `cardpay.go` (currency flows from `renew()` automatically — verify, don't re-derive), the new **exchange** endpoint (FR-69.3), `balance_api.go`/`ledger_api.go` API deltas (C6/C7).
3. **Display + reports** — `formatMoney` (shared package, C8), the 39 panel/portal call sites currently assuming IQD (audit each: does this field's row actually carry a currency now, or is it still genuinely IQD-only, e.g. a UI label?), `internal/reports` per-currency grouping (C9), the FR-40-style reconciliation invariant extended per-currency.
4. **Gate** — DB-gated migration-backfill test against a seeded Phase-3-shaped dataset + reconciliation/exchange tests; `scripts/gate-v2-phase-3.sh`; `gate-result.md`.

Commit in reviewable chunks along these boundaries (schema+ledger / flows / display+reports / gate) — this phase is big enough that a single chunk would be unreviewable.

## Migration budget 0530–0539

(v2-3b takes 0540–0549 immediately after, same money core — see the header note. Verify the repo's actual max migration number hasn't advanced past 0539 before implementing; if so, take the next free number instead per the standing linear-numbering rule.)

| Migration | Owns |
|---|---|
| `0530_currencies` | `currencies(code PK, minor_unit_digits smallint NOT NULL, symbol text NOT NULL DEFAULT '', enabled boolean NOT NULL DEFAULT true)`; seed `IQD` (0, 'د.ع'), `USD` (2, '$'), `EUR` (2, '€'), all enabled. |
| `0531_ledger_currency` | `ledger_transactions`: rename `amount_iqd`→`amount`; add `currency text NOT NULL DEFAULT 'IQD' REFERENCES currencies(code)`; add `currency_rate_id uuid REFERENCES currency_rates(id)` (nullable — populated only for `type='exchange'` rows, so this migration must run **after** 0537, or the FK target must be created first; order the file list accordingly, or split the column-add into a follow-up migration in this same range if the dependency is awkward — implementer's call, not re-litigated here); widen the `type` CHECK to add `'exchange'`. |
| `0532_manager_balances_currency` | `manager_balances`: add `currency text NOT NULL DEFAULT 'IQD' REFERENCES currencies(code)`; rename `balance_iqd`→`balance`; drop the old `manager_id`-only PK, add composite PK `(manager_id, currency)`. Every pre-migration row keeps its exact `balance` value under `currency='IQD'` — zero value loss. |
| `0533_manager_thresholds_currency` | Same treatment for `manager_low_balance_thresholds` (`threshold_iqd`→`threshold`, `+currency`, composite PK). |
| `0534_profiles_currency` | `profiles`: rename `price_iqd`→`price`; add `currency text NOT NULL DEFAULT 'IQD' REFERENCES currencies(code)`. (Column ownership is [04](../../prd/04-subscribers-profiles.md)'s per the existing FR-8 split, but the migration lands here since it is part of the one ledger-currency rework this phase exists to do once.) |
| `0535_voucher_batches_currency` | `voucher_batches`: rename `unit_price_iqd`→`unit_price`; add `currency` (same pattern), backfilled from the batch's generating profile's currency (which is `'IQD'` for every pre-migration row, so this is equivalent to a flat default). |
| `0536_payments_currency` | `payments`: rename `amount_iqd`→`amount`; add `currency` (same pattern). |
| `0537_currency_rates` | `currency_rates(id uuid PK, from_currency text NOT NULL REFERENCES currencies(code), to_currency text NOT NULL REFERENCES currencies(code), rate numeric(20,8) NOT NULL, effective_from timestamptz NOT NULL DEFAULT now(), created_by uuid REFERENCES managers(id), created_at timestamptz NOT NULL DEFAULT now())`; index `(from_currency, to_currency, effective_from DESC)`. |
| `0538`–`0539` | reserved (follow-ups discovered during build, e.g. the 0531 dependency-ordering resolution above). |

Forward-only, no `.down.sql` (Decision 25's amendment / FR-51.4 — the repo-wide rule; also, several of these renames are **provably lossy in reverse** for the same class of reason 0500 was in v2 phase 1 — a down migration would have to reconstruct `_iqd`-suffixed columns from currency-tagged ones and silently drop non-IQD rows, so it is not attempted).

## Frozen contracts

### C1. Currency catalog (FR-68.1)
```sql
CREATE TABLE currencies (
    code               text PRIMARY KEY,        -- 'IQD' | 'USD' | 'EUR'
    minor_unit_digits  smallint NOT NULL,        -- 0 for IQD, 2 for USD/EUR
    symbol             text NOT NULL DEFAULT '',
    enabled            boolean NOT NULL DEFAULT true
);
```
Every monetary integer column in the schema stores **minor units of its row's own currency** (an IQD amount's minor units == its whole-currency amount, since `minor_unit_digits=0` — this is exactly why the FR-24/FR-68.2 rename changes no stored IQD value). Enabling a fourth currency later is a data-only `INSERT` + a Settings toggle — no schema or Go-type change (mirrors FR-17's "no core changes to add one more vendor" principle, applied to currencies).

### C2. Ledger schema (FR-68.2, FR-69.3)
```sql
-- ledger_transactions (delta from migration 0200):
--   amount_iqd bigint          -> amount bigint          (renamed, semantics: minor units of `currency`)
--   (new) currency text NOT NULL REFERENCES currencies(code)
--   (new) currency_rate_id uuid REFERENCES currency_rates(id)   -- non-null ONLY for type='exchange'
--   type CHECK widened: ('renewal','topup','manual_payment','voucher_redeem','refund','adjustment','discount','exchange')
```
`ledgerEntry` (Go, `internal/billing/ledger.go`):
```go
type ledgerEntry struct {
    Type            string
    Amount          int64  // signed, minor units of Currency (was AmountIQD)
    Currency        string // NEW, required
    ActorManagerID  string
    SubscriberID    string
    Source          string
    Reference       string
    ReversesID      string
    Note            string
    CurrencyRateID  string // NEW, "" -> NULL; set only for type="exchange"
}
```
`insertLedger` inserts `currency`/`currency_rate_id` alongside the existing columns; no other signature change. The append-only trigger/REVOKE (migration 0200) needs no change — they operate on the whole row regardless of column set.

### C3. Per-currency balances (FR-69.2)
```sql
-- manager_balances (delta from migration 0200):
--   balance_iqd bigint -> balance bigint
--   (new) currency text NOT NULL REFERENCES currencies(code)
--   PRIMARY KEY (manager_id) -> PRIMARY KEY (manager_id, currency)
-- manager_low_balance_thresholds: identical treatment (threshold_iqd -> threshold, +currency, composite PK)
```
`lockBalance(ctx, tx, managerID, currency)` and `recomputeBalance(ctx, tx, managerID, currency)` (both gain a `currency` parameter) — `recomputeBalance`'s sum is now `WHERE actor_manager_id = $1 AND currency = $2` (**the single most important line in this phase**: summing across currencies here is the bug AC-69c exists to catch). `lockBalance` upserts the `(manager_id, currency)` row on first touch, identical in spirit to today's per-manager upsert.

### C4. Exchange (FR-69.3) — the only conversion path
```go
// exchangeParams: a manager converts part of their own balance from one
// currency to another, at an explicit currency_rates row.
type exchangeParams struct {
    ManagerID      string
    FromCurrency   string
    ToCurrency     string
    FromAmount     int64  // minor units of FromCurrency, positive
    CurrencyRateID string // required; the admin-entered rate this exchange uses
    ActorManagerID string // who performed it (audit; usually == ManagerID, or an admin acting on their behalf)
}
```
`exchange(ctx, p)` — one transaction: locks both `(ManagerID, FromCurrency)` and `(ManagerID, ToCurrency)` balance rows (consistent lock order — e.g. alphabetical by currency code — to avoid a deadlock against a concurrent reverse exchange), reads `currency_rates` row `CurrencyRateID` and verifies it actually converts `FromCurrency`→`ToCurrency`, computes `ToAmount` from `FromAmount` via the whole-currency `rate` adjusted for each currency's `minor_unit_digits`, inserts **two** `ledger_transactions` rows sharing one generated `reference` (e.g. `"EXG-" + <random>`) — `{Type: "exchange", Amount: -FromAmount, Currency: FromCurrency, CurrencyRateID}` and `{Type: "exchange", Amount: +ToAmount, Currency: ToCurrency, CurrencyRateID}` — recomputes both balance rows, commits. Insufficient `FromCurrency` balance refuses the whole exchange (same enforcement posture as a renewal's balance check).

`POST /api/v1/managers/{id}/exchange {from_currency, to_currency, amount, currency_rate_id}` (permission: same as `top-up`, since it is a manager's-own-money operation an admin can also perform on their behalf) → `{exchange_reference, from_ledger_tx_id, to_ledger_tx_id, from_balance, to_balance}`.

### C5. Currency rates (FR-68.3/68.4) — admin-entered only
```sql
CREATE TABLE currency_rates (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    from_currency  text NOT NULL REFERENCES currencies(code),
    to_currency    text NOT NULL REFERENCES currencies(code),
    rate           numeric(20,8) NOT NULL,   -- 1 from_currency = `rate` to_currency, WHOLE-currency terms
    effective_from timestamptz NOT NULL DEFAULT now(),
    created_by     uuid REFERENCES managers(id),
    created_at     timestamptz NOT NULL DEFAULT now()
);
```
`GET /api/v1/currency-rates?from=&to=` (any authenticated manager, read-only), `POST /api/v1/currency-rates {from_currency, to_currency, rate}` (new permission `currency_rates.manage`, audited) — rates are **append-only from the API's perspective**: a new submission is a new row with `effective_from=now()`, never an update to an existing one (so a `currency_rate_id` stamped on a historical ledger row is permanently the rate that was actually used — FR-68.1's non-negotiable requirement). No code path here ever performs an outbound HTTP request (AC-68b) — this is architectural, not merely policy: there is no HTTP client construction anywhere in the `currency_rates` package/file, verifiable by inspection same as the vendor-isolation grep pattern (a dedicated gate grep is cheap insurance, see Integration gate item 6).

### C6. Renewal/voucher/refund threading (FR-69.1/69.4/69.5)
- `renewParams`/`renewInTx` (`internal/billing/renew.go`): the profile read gains `currency` alongside `price_iqd`→`price`; `resolvePrice` is unchanged in logic (still override→profile) but now also resolves the accompanying currency the same way (a subscriber price override does **not** carry its own currency in v1's schema — freeze: an override's currency is always the profile's current currency; overriding into a *different* currency is out of scope for this phase, and the field stays a bare integer). `lockBalance`/`insertLedger`/`recomputeBalance` calls thread the resolved `currency`. `renewResult` gains `Currency string` (json). CoA restore (`restoreCoA`) is unaffected — currency has no RADIUS meaning, exactly as the phase brief's problem statement says.
- Voucher batch generation (`voucher.go`) stamps `unit_price`/`currency` from the generating profile at creation time — **mechanism unchanged** from today's charge-at-generation model (FR-69.4 is explicit that this phase does not reopen that already-resolved question).
- Refund (`refund.go`) reads the **original** entry's `currency` (via `reverses_id`) and reverses in that currency at that entry's own `amount` — never re-resolves today's profile price or today's rate table (FR-69.5; AC-69d locks this).
- Card payments (`cardpay.go`, FR-59): both the trial and approval renewals already go through `renew()`, so currency flows through automatically once `renew()` itself is currency-aware — verify with a test, do not add a second currency-resolution path here. `cardPaymentSummary.RequestedIQD` renames to `RequestedAmount` + `Currency` (JSON: `requested_amount`/`currency`, was `requested_price_iqd`).

### C7. API deltas (frozen shapes)
- `GET /api/v1/managers/{id}/balances` (**new**, plural) → `{"balances": [{"currency": "IQD", "balance": 25000}, {"currency": "USD", "balance": 5000}]}` — every currency the manager has ever touched (a zero-balance currency they've touched still appears; one they've never touched does not).
- `GET /api/v1/managers/{id}/balance?currency=IQD` (existing route, **query param added**; omitted `currency` defaults to `IQD` for the header-widget call sites that don't yet carry currency context) → `{"currency": "IQD", "balance": 25000}` (was `{"balance_iqd": ...}` — this is a breaking response-shape change to an existing v1 endpoint, called out explicitly per the "amendments are explicit, never silent" rule; every panel/portal caller of this endpoint must be updated in the same phase, not left half-migrated).
- `POST /api/v1/managers/{id}/topup {amount, currency, note}` (was `{amount_iqd, note}` — same breaking-change note as above).
- `GET /api/v1/ledger?...&currency=` (new optional filter; existing filters unchanged); every ledger row in the response gains `currency`, `amount` replaces `amount_iqd`.
- `GET/POST /api/v1/currency-rates`, `POST /api/v1/managers/{id}/exchange` — new, per C4/C5.
- `GET /api/v1/currencies` (new, read-only, panel-facing for building currency `<select>`s) → `{"items": [{"code":"IQD","minor_unit_digits":0,"symbol":"د.ع"}, ...]}`.
- Renewal/refund/voucher/receipt/card-payment responses each gain `currency` alongside their existing `*_amount`/`price` fields (field renames per C6, not additive-only — the whole point of this phase is that "an amount with no currency" stops being a valid API shape anywhere in the billing surface).

### C8. Shared amount formatter (FR-70.1)
```ts
// frontend/shared/src/format/format.ts
export function formatMoney(
  amount: number, currency: CurrencyCode, locale: Locale = 'en', opts: FormatOptions = {},
): string
// formatIQD becomes a thin wrapper — kept so every v1 call site keeps compiling,
// but each of the ~39 panel/portal consumers (grep `IQDAmount`/`useFormatters`)
// must be individually reviewed during the "Display + reports" chunk: does this
// field's underlying row now carry a real currency (migrate to formatMoney +
// that currency), or is it a screen that is legitimately IQD-only even in a
// multi-currency world (e.g. a hardcoded receipt-prefix example)? Do not
// blanket-replace without checking each call site's data source.
export function formatIQD(amount: number, locale?: Locale, opts?: FormatOptions): string {
  return formatMoney(amount, 'IQD', locale, opts)
}
```
`IQDAmount` (the shared component, `frontend/shared/src/ui/IQDAmount.tsx`) gains an optional `currency` prop defaulting to `'IQD'` (source compatible with every existing call site; each is still individually reviewed per the above). `formatMoney(amount, 'IQD', locale, opts)`'s output is byte-identical to today's `formatIQD` for the same arguments (AC-70a) — this is a **regression lock**, not a new design: USD/EUR simply pass a different ISO code into the same `Intl.NumberFormat({style:'currency', currency})` call, which already handles minor-unit digit counts correctly per currency without HikRAD needing to reimplement that logic.

### C9. Reports & reconciliation (FR-70.2)
Every report and reconciliation invariant that touches `ledger_transactions`/`payments`/`manager_balances` groups by `currency` as an additional, mandatory dimension — never sums across it. An explicit "converted to display currency" view is additive: it applies the latest applicable `currency_rates` row to each currency's authoritative per-currency figure and labels the result with the rate and its `effective_from` date; it is never the only figure shown, and it is never itself summed back into a single number that looks authoritative.

## Integration gate

Green when all pass (scriptable legs in `scripts/gate-v2-phase-3.sh`; DB-gated legs require `HIKRAD_TEST_DB_URL`/`HIKRAD_TEST_REDIS_URL`, self-skip otherwise — same convention as every prior phase):

1. **Migration backfill on a seeded Phase-3-shaped dataset** — mirrors v2 phase 1's gate item 1 pattern exactly: a scratch database migrated to the last pre-v2-3 migration, seeded with v1-shaped rows (bare `*_iqd` integers, no currency concept), then migrated to head. Assert: zero row loss; every renamed column's value unchanged; every row's `currency = 'IQD'`; `manager_balances`' new composite PK holds the same balances under `(manager_id, 'IQD')` as the old PK held under `manager_id` alone. Forward-only per the migration-range note — no down leg tested.
2. **Per-currency reconciliation invariant (AC-69c)** — `balance(M, C) = sum(ledger.amount WHERE actor_manager_id=M AND currency=C)` holds for a manager with **both** an IQD and a USD balance; a deliberately-mutated `recomputeBalance` that sums across currencies must make this test fail (mutation-checked, mirroring v2 phase 1's own "not passing vacuously" discipline).
3. **Exchange pair (AC-69b)** — exactly two new ledger rows per exchange, correct signs, correct `currency_rate_id` stamped on both, correct minor-unit-adjusted `ToAmount`, both balances update by exactly the exchanged amounts, no other manager or currency is touched.
4. **Non-IQD renewal (AC-69a)** — a USD-priced profile renewal debits only the agent's USD balance; their IQD balance (independently seeded non-zero) is provably untouched; the receipt/payment row records `currency='USD'`.
5. **Refund reverses in the original currency, no re-resolution (AC-69d)** — a USD renewal's refund is a USD reversing entry at the original amount, even when the profile's price or the rate table has since changed.
6. **No online rate feed (AC-68b)** — a grep leg (mirrors the vendor-isolation pattern) asserting no HTTP client construction (`http.Client`, `http.Get`, `http.NewRequest` etc.) appears in the `currency_rates`-owning file(s); rate creation is reachable only through the authenticated `POST /currency-rates` handler.
7. **formatMoney regression lock (AC-70a)** — `formatMoney(amount, 'IQD', locale, opts)` output byte-identical to `formatIQD`'s current test-suite expectations, for every case the existing `format.test.ts` already covers, plus new USD/EUR cases.
8. **Migration lossless + build** — `go build`/`go vet` clean; the full pre-existing `internal/billing` test suite passes (every `*_iqd`-shaped assertion updated to the new field names, same values expected — this phase's version of v2 phase 1's C1 non-invalidation guarantee).
9. **Panel/portal** — build + lint + vitest green; `i18n:check` green (0 missing keys, 0 hardcoded strings) covering the currency selector(s), the exchange screen, and the rate-table admin screen.
10. **Docs accuracy** — PRD/sub-PRD 05/index reflect FR-68–70; `docs/ops/known-issues.md` carries any bug found while building, before or alongside its fix.

Human/hardware legs: none — this phase has no router/device dependency. The only "documented-pending" candidate is a live pilot ISP actually pricing a plan in USD/EUR end-to-end, which is an operational rollout step, not a gate item this brief can script.

## Bugs

Any bug found while building goes in [docs/ops/known-issues.md](../../../ops/known-issues.md) with root cause + fix + commit (Decision 27 / v2 rule 3), before or alongside the fix.
