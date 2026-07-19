# v3-2 — Single-Book IQD Currency Redesign

> Owner request 2026-07-19 (review item 12; PRD Decision 47b). Proposed FR numbers: **FR-100 (single-book ledger), FR-101 (entry-time conversion + display), FR-102 (upgrade conversion)** — committed to the master PRD at kickoff. Owner: sub-PRD 05 (money core), the same owner as FR-68–70 which this reverses in part.

## 1. Problem (owner's words, paraphrased)

v2-4 gave every `(manager, currency)` pair its own wallet — "why does each currency have its own bank?" A manager holding 100,000 IQD and 50 USD has two balances, must run an explicit exchange to move value between them, and every report/threshold/alert had to pick a currency or blend. The owner wants **one bank**: a single balance per manager, kept in IQD; other currencies exist only as *ways of expressing* an amount at the admin-maintained rate — "when changing currency view or whatsoever it directly exchanges with same value but with the rates."

## 2. Target model

- **Ledger and balances are IQD-only.** `manager_balances` collapses to one row per manager (IQD). The ledger's `amount` column is IQD for every new row.
- **Currency at entry is a conversion, not a denomination**: any money form (topup, renewal price display, payment ticket, voucher batch, refund view) may accept/display USD/EUR, converted at the **current admin rate at entry time**; the row stores the IQD result **plus** the original `entered_amount`, `entered_currency`, and `currency_rate_id` used — receipts and the audit trail always show both figures.
- **Rates stay admin-entered only** (AC-68b survives: no online feed, NFR-7). The `currencies` catalog and `currency_rates` table survive unchanged; `manager.exchange` (wallet-to-wallet) is retired — there is nothing to exchange between.
- **Refunds** reverse the original entry's stored IQD amount exactly (never re-converted at today's rate); the receipt shows the original foreign figure with its original rate. This preserves FR-69.5's intent in the new model.
- **Profiles/pricing**: `profiles.currency` remains as a *display* preference (a plan "priced in USD" shows its USD figure and converts at charge time), or is retired to IQD-only — **open question 1**, owner decides at kickoff.
- **Display toggle**: money surfaces (balances, ledger, reports) get a view-currency switcher that converts on the fly at the current rate, clearly marked as approximate ("≈ $76.30 at 1310").

## 3. Upgrade migration (the dangerous part — owner chose auto-convert)

- One migration (next free number at build time) converts every non-IQD `manager_balances` row into IQD **by writing a pair of ledger entries** (debit foreign wallet to zero at the current admin rate for that currency, credit the IQD balance with the result), stamped with the rate row used — the ledger stays append-only, history is never edited.
- **Refuses to run** (aborting the whole upgrade with a clear message) if any non-IQD balance exists for a currency with **no admin rate defined** — silent 1:1 conversion is forbidden.
- Existing historical ledger rows keep their original currency columns untouched (read-only history); reports over historical ranges keep reporting what actually happened, converted for display only.
- A pre-migration backup note in the release checklist; the migration logs a per-manager conversion summary into the audit log.

## 4. Blast radius (why this is a phase, not a fix)

`billing` (renewals, vouchers, topups, refunds, receipts, payment tickets — v2-2 threads currency through tickets), `reports` (revenue/margin reconciliation — v2-9's cost/wholesale stamps are already IQD-based via rates, re-verify), `monitorsvc` (my-balance widget, agent_balance_low alert — both currently per-currency), panel screens (`/currency-rates` exchange form retired, balance displays, topup form), portal (Pay screen amounts), `formatMoney` call sites. The v2-4 gate's regression locks (byte-identical `formatIQD`) must survive.

## 5. Non-goals
- No online rate feeds, no automatic rate updates, no rate history UI changes.
- No multi-book accounting (cost centers etc.).
- No change to reseller wholesale mechanics (v2-9) beyond re-verifying their IQD math.

## 6. Acceptance sketch
- A USD-entered topup lands as one IQD ledger row carrying `entered_amount/entered_currency/currency_rate_id`; its receipt shows both figures and the rate.
- Refund of that row returns exactly its IQD amount regardless of any rate change since; receipt shows the original USD figure.
- After upgrade on a database with USD/EUR balances: every manager has exactly one IQD balance; the conversion pair is visible in their ledger and audit log; totals reconcile (sum of converted IQD == sum of old wallets × rates, exactly, integer math documented).
- Upgrade refuses cleanly when a rate is missing.
- All money invariants (append-only, derived balances) re-verified; v2-4/v2-9 gate regression tests updated deliberately, never deleted silently.

## 7. Open questions (owner, at kickoff)
1. `profiles.currency`: keep as display-denomination (convert at charge time) or force IQD-only plans?
2. Rounding rule for entry-time conversion (IQD has no fraction): round-half-up per entry, or floor with remainder note? Must be fixed and documented before any code.
3. Does the portal show foreign-currency price equivalents to subscribers, or IQD only?

---

## AI kickoff prompt (paste into a fresh session to start v3-2)

```
Read CLAUDE.md, docs/PRD.md §Decisions (28, 45–47 and the v2-4 rows), docs/v2/04-multi-currency.md, docs/v2/phases/phase-v2-4-multi-currency/ (00-phase.md + gate-result.md), docs/v3/00-v3-index.md, and docs/v3/02-currency-single-book.md, then run the standing kickoff protocol for phase v3-2:

1. Amend the master PRD (FR-100/101/102 + Decision row) and sub-PRD 05; mark which v2-4 FR clauses are superseded — explicitly, clause by clause, never by implication.
2. Research pass BEFORE freezing contracts: enumerate every reader/writer of manager_balances, ledger currency columns, currency_rates, and formatMoney; resolve the brief's §7 open questions with the owner via AskUserQuestion.
3. Write docs/v3/phases/phase-v3-2-currency-single-book/00-phase.md with frozen contracts: the new ledger row shape, the conversion-stamp fields, the upgrade migration's exact algorithm + refusal conditions + reconciliation proof, the API/UI changes, and the integration gate (including an upgrade rehearsal on a copy of realistic data).
4. Present the brief and STOP for owner confirmation before writing feature code — this phase performs an irreversible money migration; the owner must see the migration plan verbatim.
5. Implement in reviewable chunks (schema/migration → billing core → API/panel → portal/reports → gate); every bug to docs/ops/known-issues.md; finish with gate-result.md.

Constraints you may not relax: ledger append-only (conversion = new entries, never edits), refunds reverse stored IQD amounts, no online rate feed (NFR-7), migration refuses on missing rates, migrations linear-next-number, SOLO sequential execution.
```
