# Phase v2-9 — Cost, Margin & Reseller Pricing

Source brief: [docs/v2/09-cost-margin-and-reseller-pricing.md](../../09-cost-margin-and-reseller-pricing.md). Requirements FR-71–76 (PRD Decision 36; sub-PRD [05-billing-payments-vouchers.md](../../../prd/05-billing-payments-vouchers.md)). Builds directly on v2-4's per-currency ledger (FR-68–70) — every new money field carries a currency, and every new resolution/derivation follows the same append-only, never-store-a-derived-number discipline the ledger already enforces.

**Kickoff blockers, resolved by the owner 2026-07-17 (binding, not re-litigated by this brief):**
1. **No sub-reseller tree.** Flat 2-level only: owner → reseller → subscriber. `reseller_prices.manager_id` is always a direct reseller of the owner. No ancestry/hierarchy column, no recursive lookup, anywhere in this phase.
2. **Overheads: per-NAS/site as well as global.** `overheads.nas_id` is nullable; `NULL` = whole-business, non-null = that one site.
3. **Reseller wholesale pricing needs per-subscriber overrides.** `reseller_prices.subscriber_id` is nullable; a non-null row overrides the plan-wide (`subscriber_id IS NULL`) row for that one subscriber.

## 1. Problem (restated from the source brief)

HikRAD knows what a subscriber is charged and nothing about what they cost, and models every reseller as paying the same price as every other reseller for the same plan. This phase adds: a versioned plan cost price, margin derived on the ledger (never stored), global and per-site overheads reported separately from per-plan margin, and flat 2-level reseller wholesale pricing (optionally per-subscriber) resolved independently from the subscriber's own retail charge.

## 2. Scope for this implementation pass

1. **Schema** — `profile_cost_history`, `overheads`, `reseller_prices`, `ledger_transactions.cost_at_sale` (C1–C4).
2. **Backend** — cost/margin resolution threaded into the single FR-19.3 renewal path (C5), the reseller pricing-resolution algorithm (C6), API deltas (C7), reseller-facing scoping (C8).
3. **Reports** — per-plan/per-period margin, per-site net margin, reseller-facing margin view (C9).
4. **Display** — panel screens for cost entry, overheads, reseller wholesale pricing, and the margin report (no new shared-package contract needed — `formatMoney`/`IQDAmount` from v2-4 already handle every amount here).
5. **Gate** — DB-gated tests for the pricing-resolution algorithm, the two-leg ledger pair, per-site overhead isolation, and reseller-scoping (never leaking cost/wholesale); `scripts/gate-v2-phase-9.sh`; `gate-result.md`.

Commit in reviewable chunks along these boundaries (schema+ledger / pricing resolution+API / reports / panel / gate) — mirrors v2-4's own chunking, for the same reason (this phase is too large for one commit to be reviewable).

## Migration budget 0540–0549

(Verify the repo's actual max migration number hasn't advanced past 0549 before implementing; if so, take the next free number instead per the standing linear-numbering rule. At the time this brief was written, the repo's max was 0538 — v2-4's own budget, 0539 unused/reserved.)

| Migration | Owns |
|---|---|
| `0540_profile_cost_history` | `profile_cost_history(id uuid PK, profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE, cost bigint NOT NULL, currency text NOT NULL REFERENCES currencies(code), effective_from timestamptz NOT NULL DEFAULT now(), created_by uuid REFERENCES managers(id), created_at timestamptz NOT NULL DEFAULT now())`; index `(profile_id, effective_from DESC)`. No row on `profiles` itself — see C1's "query, don't mirror" note. |
| `0541_overheads` | `overheads(id uuid PK, name text NOT NULL, amount bigint NOT NULL, currency text NOT NULL REFERENCES currencies(code), nas_id uuid REFERENCES nas(id) ON DELETE SET NULL, period_start timestamptz NOT NULL, period_end timestamptz, notes text NOT NULL DEFAULT '', created_by uuid REFERENCES managers(id), created_at timestamptz NOT NULL DEFAULT now())`; index `(nas_id, period_start)`. |
| `0542_reseller_prices` | `reseller_prices(id uuid PK, manager_id uuid NOT NULL REFERENCES managers(id) ON DELETE CASCADE, profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE, subscriber_id uuid REFERENCES subscribers(id) ON DELETE CASCADE, price bigint NOT NULL, currency text NOT NULL REFERENCES currencies(code), effective_from timestamptz NOT NULL DEFAULT now(), created_by uuid REFERENCES managers(id), created_at timestamptz NOT NULL DEFAULT now())`; index `(manager_id, profile_id, subscriber_id, effective_from DESC)`. |
| `0543_ledger_cost_at_sale` | `ledger_transactions` gains nullable `cost_at_sale bigint` (no currency column of its own — it is always in the ledger row's own existing `currency`, per FR-72.1; a cost recorded in a *different* currency than the sale is a display-only concern for reports, C9, never converted onto this column). |
| `0544`–`0549` | Reserved (follow-ups discovered during build, same convention as every prior phase's tail). |

Forward-only, no `.down.sql` (repo-wide rule, Decision 25's amendment / FR-51.4).

## Frozen contracts

### C1. Plan cost price (FR-71) — query, don't mirror
Deliberate simplification from the source brief's draft wording ("`profiles.cost_price` always mirrors the latest history row"): **there is no `cost_price`/`cost_currency` column on `profiles` at all.** The "current cost" is resolved the same way `currency_rates` already resolves "the rate to use" (v2-4 precedent) — a query against `profile_cost_history` for the latest `effective_from <= now()` (or, for a past renewal's margin, the latest `effective_from <= <that renewal's `at`>`). Mirroring a value onto `profiles` would be a second, always-derivable source of truth to keep in sync — the exact class of bug the ledger's own append-only philosophy exists to rule out. A profile with **zero** `profile_cost_history` rows has **unknown** cost (FR-71.1's "null, not zero" requirement is satisfied by "no matching row", not by a nullable column).
```go
// resolveCost returns the cost in force at `at` (a renewal's own timestamp,
// so a past renewal's margin recomputes identically no matter when it's
// re-queried), and (0, "", false) when the profile has no cost recorded yet.
func resolveCost(ctx context.Context, tx pgx.Tx, profileID string, at time.Time) (cost int64, currency string, ok bool)
```

### C2. Margin on the ledger (FR-72)
```sql
-- ledger_transactions (delta from migration 0532):
--   (new) cost_at_sale bigint  -- nullable; NULL = cost was unknown at sale time
```
Stamped by the FR-19.3 renewal path in the same transaction that stamps `amount`/`currency` (v2-4), via `resolveCost` (C1) at the renewal's own timestamp. `insertLedger`'s `ledgerEntry` struct gains `CostAtSale *int64` (pointer so `NULL` is representable; every existing caller that doesn't resolve a cost passes `nil`, unchanged behavior). Margin is **never stored** — every report/API response computes `amount - cost_at_sale` on read, and only for rows where `cost_at_sale IS NOT NULL` (a row with unknown cost contributes to revenue sums but is excluded from margin sums — the report never treats "unknown" as "zero").

### C3. Overheads (FR-73)
```sql
CREATE TABLE overheads (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name         text NOT NULL,
    amount       bigint NOT NULL,
    currency     text NOT NULL REFERENCES currencies(code),
    nas_id       uuid REFERENCES nas(id) ON DELETE SET NULL,  -- NULL = global/whole-business
    period_start timestamptz NOT NULL,
    period_end   timestamptz,                                  -- NULL = open-ended
    notes        text NOT NULL DEFAULT '',
    created_by   uuid REFERENCES managers(id),
    created_at   timestamptz NOT NULL DEFAULT now()
);
```
`POST /api/v1/overheads` (permission `overheads.manage`, new — admin-only, matching `currency_rates.manage`'s posture: this is business-cost data, not a reseller-visible figure) creates a new row (edits happen by superseding: set the old row's `period_end`, insert a new row — never an in-place UPDATE of `amount`, so a past period's reported net margin never silently changes). `GET /api/v1/overheads?nas_id=&as_of=` lists.

**Per-site net margin never pro-rates global overheads onto a site** (FR-73.3): the per-site margin report sums exactly that NAS's own `overheads` rows against that NAS's own attributed revenue for the period; global (`nas_id IS NULL`) overheads are reported as a separate whole-business figure, on the same page but never merged into the per-site number. A subscriber with no NAS session history in the report period is excluded from every site's revenue (never guessed into one, per FR-73.4) — attribution uses the subscriber's live/last-known NAS from `internal/live`/session history, the same source [03](../../../prd/03-lossless-accounting-live-monitoring.md) already uses for other per-NAS figures.

### C4. Reseller (wholesale) pricing, flat 2-level, optional per-subscriber override (FR-74)
```sql
CREATE TABLE reseller_prices (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    manager_id     uuid NOT NULL REFERENCES managers(id) ON DELETE CASCADE,
    profile_id     uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    subscriber_id  uuid REFERENCES subscribers(id) ON DELETE CASCADE,  -- NULL = plan-wide for this reseller
    price          bigint NOT NULL,
    currency       text NOT NULL REFERENCES currencies(code),
    effective_from timestamptz NOT NULL DEFAULT now(),
    created_by     uuid REFERENCES managers(id),
    created_at     timestamptz NOT NULL DEFAULT now()
);
```
Append-only versioned exactly like `profile_cost_history`/`currency_rates` — a price change is a new row, never an UPDATE, so a past renewal's resolved wholesale price is always re-derivable from history. `manager_id` is always a direct reseller of the owner (no sub-reseller tree, per the resolved kickoff blocker) — this phase adds **no** ancestry/parent-manager column anywhere; `managers` is unchanged in shape.

`POST /api/v1/reseller-prices {manager_id, profile_id, subscriber_id?, price, currency}` (permission `reseller_prices.manage`, admin-only — a reseller never sets their own wholesale price, that would let them set it to zero) inserts a new version. `GET /api/v1/reseller-prices?manager_id=&profile_id=` lists (admin-only; see C8 for why a reseller cannot call this for another reseller, or at all for the wholesale-price value itself).

### C5. Renewal threading (FR-72.1, FR-74.3, FR-76) — one transaction, three independently-resolved numbers
`internal/billing/renew.go`'s single FR-19.3 transaction gains two more resolutions alongside its existing price/currency resolution, all inside the same `tx`:
- **Subscriber charge** (existing, unaffected): `resolvePrice` (override → promo → profile retail), unchanged by this phase (FR-76.1).
- **Cost** (new, C1): `resolveCost(ctx, tx, profileID, now)` → stamped as `cost_at_sale` on the renewal's own ledger row (C2). Independent of whether a reseller relationship exists at all.
- **Reseller wholesale debit** (new, C4/C6): only when the acting/owning manager is *itself* being resold-through — i.e., **when the renewal's actor is a reseller with a resolvable wholesale price** (C6's resolution finds a `reseller_prices` row). When no row resolves, behavior is **byte-identical to pre-v2-9**: one debit at the subscriber's retail price, exactly like today.

When a wholesale price *does* resolve, the renewal writes **two** ledger rows in the one transaction (mirroring v2-4's exchange-pair pattern, C4 of that phase): the existing renewal-type row unchanged in shape (now additionally carrying `cost_at_sale`) plus a companion row that captures the reseller's actual wholesale debit when it differs from the retail charge already recorded. **Implementer's call, not re-litigated here**: whether this is modeled as a second `ledger_transactions` row of a new type (`'reseller_debit'`, widening the existing CHECK per the v2-4 precedent) or as the *existing* renewal row's `amount` itself becoming the wholesale figure with the retail-vs-wholesale delta captured on `payments` (the customer-facing gross record, which already exists precisely to record what the *subscriber* paid, separate from the ledger's balance-effect amount) — both satisfy FR-74.3's "an explicit pair, never one row with two meanings" as long as **the subscriber's payment/receipt always shows retail** (unaffected by any reseller relationship) **and the manager's ledger-derived balance always reflects the wholesale debit**. Pick one, document the choice and why in the phase's own implementation notes, and lock it with a DB-gated test proving both numbers are independently correct and independently queryable — this is the single most consequential design decision in this phase (parallel to v2-4's "recomputeBalance must filter by currency" being flagged as its own most-consequential line).

### C6. Pricing resolution algorithm (FR-76)
```go
// resolveWholesale returns the wholesale price a reseller pays for a
// subscriber's renewal, most-specific-first, or (0, "", false) when no
// reseller_prices row exists at all — meaning "use retail" (FR-74.1's
// fallback, byte-identical to pre-v2-9 behavior).
func resolveWholesale(ctx context.Context, tx pgx.Tx, resellerManagerID, profileID, subscriberID string, at time.Time) (price int64, currency string, ok bool)
// Tries, in order: reseller_prices WHERE manager_id, profile_id, subscriber_id = $3 (per-subscriber override)
//               -> reseller_prices WHERE manager_id, profile_id, subscriber_id IS NULL (plan-wide)
//               -> not found
// both queried at "the latest effective_from <= at", same pattern as resolveCost (C1).
```
The subscriber-charge leg (`resolvePrice`, existing) is **entirely unaffected** — FR-76.1 is explicit that a reseller's wholesale arrangement never changes what their subscriber is charged, and this brief adds no code path that could make it do so.

### C7. API deltas (frozen shapes, additive — no existing v1/v2-4 response shape changes)
- `POST /api/v1/profiles/{id}/cost {cost, currency}` (permission `profiles.edit` — cost is plan data, same gate as editing the plan itself) → new `profile_cost_history` row; `GET /api/v1/profiles/{id}/cost-history` lists.
- `GET/POST /api/v1/overheads` — per C3.
- `GET/POST /api/v1/reseller-prices` — per C4, admin-only.
- `GET /api/v1/reports/margin?from=&to=&nas_id=` (permission `reports.view`, response shape scoped per C8) — per-plan and per-period revenue/cost/margin (FR-72.3), optionally narrowed to one site's net margin (FR-73).
- Renewal response (`renewResult`, unchanged JSON shape from v2-4) gains no new field — the wholesale debit, if any, is a fact about the *manager's* ledger, not the renewal caller's own result; a reseller checking what they were charged reads their own ledger/balance (existing `GET /managers/{id}/balance`), not the renewal response.

### C8. Reseller-facing scoping (FR-75) — a commercial-severity leak, not a UX nit
`GET /api/v1/reports/margin` and any other endpoint touching cost/wholesale data applies `auth.ScopeFilter` (existing FR-27.2 machinery) with an **additional** field-level cut, not just a row-level one: a scoped (reseller) caller's response **must never contain** `profile_cost_history` values, any *other* reseller's `reseller_prices` row, or `ledger_transactions.cost_at_sale` — even for their own subscribers' renewals. An admin (unscoped) caller sees the full chain. This is enforced at the handler/response-shape level (separate DTOs for scoped vs. unscoped, or explicit field omission — implementer's call), never by trusting the panel to hide a field the API already sent; AC-75a (sub-PRD 05) is explicit that this is verified by response-shape inspection, not UI hiding, and the gate (item covering AC-75a below) tests exactly that.

### C9. Reports & reconciliation (FR-72.3, FR-73, FR-75)
- Per-plan/per-period margin report: `sum(margin over rows with cost_at_sale IS NOT NULL) = sum(amount) - sum(cost_at_sale)` for the same filtered slice — the FR-40-style reconciliation invariant, restated for margin. A DB-gated test asserts this holds and that a row with `cost_at_sale IS NULL` still counts toward revenue.
- Per-site net margin (FR-73.3): tested with a global overhead and a per-site overhead in the same period, asserting the per-site figure never includes any share of the global one.
- Reseller-facing margin view: `their margin = retail - resolved wholesale`, aggregated the same way, gated by C8's scoping.

## Integration gate

Green when all pass (scriptable legs in `scripts/gate-v2-phase-9.sh`; DB-gated legs require `HIKRAD_TEST_DB_URL`/`HIKRAD_TEST_REDIS_URL`, self-skip otherwise — same convention as every prior phase):

1. **Cost resolution + unknown-cost safety (AC-71a)** — a profile with no cost ever recorded reports margin as unknown, never zero/100%; a cost changed after a renewal doesn't retroactively change that renewal's already-stamped `cost_at_sale` or its recomputed margin.
2. **Margin reconciliation (AC-72a)** — `sum(margin) = sum(amount) - sum(cost_at_sale)` over rows with non-null `cost_at_sale`; null-cost rows count toward revenue, excluded from margin.
3. **Per-site overhead isolation (AC-73a)** — a global overhead and a per-site overhead in the same period; the per-site net-margin figure nets only its own tagged overhead, never a pro-rated share of the global one.
4. **Retail unaffected by wholesale (AC-74a)** — a reseller with no `reseller_prices` row: their subscriber's renewal debits the reseller exactly the retail price, byte-identical to pre-v2-9.
5. **Per-subscriber override beats plan-wide (AC-74b)** — a reseller with both a plan-wide and a per-subscriber wholesale price: the specific subscriber's renewal debits the per-subscriber price; every other subscriber on the same plan debits the plan-wide price; the subscriber themselves is always charged retail in both cases.
6. **Reseller-facing scoping never leaks (AC-75a)** — a reseller-scoped call to the margin/reporting endpoints never contains `cost_price`/`profile_cost_history`/another reseller's `reseller_prices`/`cost_at_sale`, verified by response-shape inspection (parse the JSON, assert the fields are absent — not "the panel doesn't render them").
7. **Independent leg resolution (AC-76a)** — a subscriber with an active `price_override` belonging to a reseller with a wholesale price configured: the subscriber is charged their override exactly (unaffected by the reseller relationship); the reseller's balance still debits the resolved wholesale price; the two numbers are asserted independently, never conflated.
8. **No sub-reseller tree exists in code** — a grep/schema leg asserting `managers` gained no ancestry/parent column and `reseller_prices` has no self-referential or recursive structure (mirrors v2-4's AC-68b "no HTTP client" grep pattern, applied to "no hierarchy" instead).
9. **Build + full regression** — `go build`/`go vet` clean; the full pre-existing `internal/billing` suite (including v2-4's own gate tests) passes unchanged — this phase adds fields and resolutions, it must not change any existing renewal/refund/exchange/topup outcome when no cost/overhead/reseller-price data exists (the "byte-identical to pre-v2-9 when unconfigured" guarantee threaded through C1/C4/C5/C6 above).
10. **Panel/portal** — build + lint + vitest green; `i18n:check` green covering the cost-entry screen, overheads screen, reseller-pricing screen, and margin report.
11. **Docs accuracy** — PRD/sub-PRD 05 reflect FR-71–76 (already done in this brief's own Step 1 commit, before this file); `docs/ops/known-issues.md` carries any bug found while building.

Human/hardware legs: none — same as v2-4, this phase has no router/device dependency (per-site overhead attribution reads existing session/NAS data, it does not probe a router).

## Open implementation questions for whoever builds this (not blocking, but worth a decision-log entry when resolved)

- **C5's exact reseller-debit ledger shape** (new ledger `type` vs. payments-table delta) is explicitly left to the implementer, per C5's own text — resolve it first, since almost everything else in the backend chunk depends on it.
- Whether `overheads.manage` and `reseller_prices.manage` are two new permissions or one combined "pricing.manage" — the source brief doesn't specify; two separate permissions is recommended (matches the granularity of every other admin-only money permission, e.g. `currency_rates.manage`, `topup`), but is not frozen here.
