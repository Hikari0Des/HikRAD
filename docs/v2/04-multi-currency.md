# v2-04 — Full multi-currency billing (IQD / USD / EUR)

> Owner request 2026-07-16 (item 16); owner explicitly chose **full multi-currency billing** over a display-only dropdown. This reworks the money core (append-only ledger, balances, receipts, reports) that Phases 3/5 froze around a single implicit IQD, so it is v2 by definition.

## 1. Problem

Every amount in v1 is an implicit-IQD integer (`*_iqd` columns, `formatIQD`, IQD-only receipts/reports). Iraqi ISPs routinely price premium plans in USD and settle with agents in mixed currencies; EUR appears in border regions. There is no way to record which currency a transaction happened in, let alone hold balances or report per currency.

## 2. Requirements (draft — renumber as FR-6x at kickoff)

### FR-A — Currency model
- Supported currency enum: `IQD`, `USD`, `EUR` (extensible table, not hardcoded — but only these three ship enabled).
- `ledger_transactions` gains `currency` (NOT NULL, backfilled `IQD`) and amounts stay integer **minor units** (IQD has no minor unit in practice — store as-is; USD/EUR store cents).
- Exchange rates: manual admin-maintained rate table (`currency_rates`: from, to, rate, effective_from) — **no online rate feed** (NFR-7 offline rule). Every conversion stores the rate used on the ledger row so history never re-values.

### FR-B — Money flows
- Profiles: price gains a currency; renewals charge in the profile's currency.
- Manager balances become **per-currency balances** (a manager can hold IQD and USD simultaneously; no implicit conversion — topups/renewals/refunds must match currency or record an explicit exchange ledger pair using the rate table).
- Vouchers: batch carries the profile's currency; charging at generation stays frozen (C3) but in that currency.
- Receipts print amount + currency; refunds reverse in the original currency at the original rate.

### FR-C — Display & reports
- Currency dropdown in Settings (default display currency) and per-manager preference (v2-06); `formatIQD` generalizes to `formatMoney(amount, currency)` (shared package, all three locales, IQD keeps Arabic-Indic digit rules).
- Reports gain per-currency columns + an optional "converted to display currency" view that labels the rate date; ledger reconciliation (M-series metrics) runs **per currency** — never across a conversion.

## 3. Impact map (why this is v2)

| Touched | Built in | Change |
|---|---|---|
| `ledger_transactions` schema + insertLedger + balance recompute | Phase 3 (D) | currency column, per-currency balances, exchange pairs |
| Renewal/voucher/refund/receipt paths | Phase 3 (D) | currency threading end to end |
| Reports + reconciliation | Phase 5 (D) | per-currency grouping; conversion view |
| `formatIQD` + every amount render (panel, portal, receipts, WhatsApp msgs) | Phases 2–5 (E/F) | formatMoney(currency) |
| Payment gateways/card payments | Phase 4 (D) | gateway settlements record their currency |

## 4. Acceptance sketch

- A USD-priced profile renews a subscriber from an agent's USD balance; the receipt shows USD; the IQD balance is untouched.
- An explicit exchange (agent converts 100 USD → IQD at a stored rate) produces a balanced ledger pair; reconciliation per currency still nets to zero.
- Historical IQD rows are untouched by the migration (backfill `IQD`, amounts unchanged); all Phase-3 money tests pass with currency=IQD defaults.
- Changing the display currency never rewrites stored amounts — only labels/conversion views.

## 5. AI kickoff prompt (paste into a fresh Claude Code session at repo root)

```text
You are working in the HikRAD repo. v1 is complete; we are starting v2 phase 3: full multi-currency billing (IQD/USD/EUR). You work SOLO — no parallel agents; execute sequentially (schema/ledger core → flows → display/reports), committing in reviewable chunks.

Read, in this order and nothing else yet: CLAUDE.md, docs/v2/phases/00-v2-execution-plan.md, docs/v2/04-multi-currency.md, docs/prd/05-billing-money.md, backend/internal/billing/ledger.go, backend/internal/billing/renew.go.

Step 1 — Amend the docs (single commit): new FR rows + a Decisions Log row in docs/PRD.md (this supersedes the implicit-IQD assumption; cite the owner's 2026-07-16 choice of full multi-currency), update sub-PRD 05 and docs/prd/00-index.md.

Step 2 — Create docs/v2/phases/phase-v2-3-multi-currency/00-phase.md with frozen contracts (currency enum + minor-unit rules, ledger schema delta + backfill, per-currency balance shape, rate-table schema, formatMoney signature, API response deltas) and the integration gate (per-currency reconciliation invariant, exchange-pair test, migration backfill test on a seeded Phase-3 dataset; migration range 0530–0549). Scriptable gate items → scripts/gate-v2-phase-3.sh.

Step 3 — Stop and present the phase brief for my confirmation before writing feature code.

Constraints: money/audit tables stay append-only (DB-level REVOKE); balances always derive from the ledger; NO online rate feeds (NFR-7) — rates are admin-entered; reconciliation runs per currency, never across conversions. Amount rendering everywhere goes through the shared formatter (i18n:check + trilingual). Update every doc invalidated; record bugs in docs/ops/known-issues.md.
```
