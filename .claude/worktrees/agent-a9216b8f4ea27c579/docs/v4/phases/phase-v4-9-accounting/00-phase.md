# Phase v4-9 — Double-entry accounting, undo & approvals

> **FRs:** FR-153 … FR-159 · **Migrations:** 0615–0622 · **Depends on:** v4-1 · **Blocks:** v4-11 (invoices), v4-12 (reconciliation)
> **Owner sub-PRD:** 05 (billing) · **Decisions committed at kickoff:** 63
> **This phase touches money. Read §3.1's staging rule before writing any code.**

## 1. Intent

HikRAD's ledger is append-only, derived-balance, and correct — but it is a *single-entry* ledger. It can answer "what did this manager's balance do" and cannot answer "what is this business worth", "what did it earn last quarter", or "who owes whom". SAMM runs full double-entry underneath a plain-language surface (invoices, expenses, receipts) and gets P&L, balance sheet, cash flow and aging for free. Iraqi ISPs are businesses with real books; this is a genuine capability gap, not a formatting one.

Plus two operational features that the double-entry substrate makes safe: a **five-minute undo window**, and an **agent approval gate** for actions that move money back toward an agent.

## 2. Scope

**In:** the journal substrate, invoices, expenses, receipts/payments, statements, undo, approvals, agent commission and credit limit.
**Not in:** online payment gateways (Decision 67 — deferred until real credentials exist), the portal's invoice surface (v4-11), report screens (v4-12).

## 3. Work

### 3.1 FR-153 — Journal substrate (0615–0617) — **staged, not switched**

The migration rule for this phase, and the reason it is safe:

> The journal is added **beside** the existing ledger, both are written for a full release, and a reconciliation test asserts they agree on every figure. Only after that does the ledger become a projection of the journal. **The existing ledger is never reinterpreted, rewritten, or deleted in this phase.**

- **0615** — `accounts` (chart of accounts: asset, liability, equity, revenue, expense) with a seeded default chart.
- **0616** — `journal_entries` and `journal_lines` (entry id, account, debit, credit, currency IQD per the v3-2 single book, memo, source reference). Append-only at the DB level, matching the existing ledger's discipline. **Balanced or refused** — an unbalanced entry is a bug, enforced by a constraint, not by convention.
- **0617** — backfill: every existing ledger row projected into balanced journal entries, with a reconciliation report written into the gate result.

Every money action posts an entry automatically. Operators never see debits and credits unless they open the Advanced statements.

### 3.2 FR-154 / FR-155 — Invoices, expenses, receipts, payments (0618–0620)
- **Invoices**: customer, revenue category, issue and due dates, tax rate, line items; **full period / pro-rated (segmented) / no charge** — with no-charge gated by a dedicated permission (SAMM's "allow no invoice" right). Auto-generated on priced renewals. PDF via the branded template.
- **Expenses**: category, vendor, amount, incurred and due dates.
- **Receipts** (money in) and **payments** (money out) settle invoices and expenses through the unpaid → partial → due → paid lifecycle.
- The existing manual payment-ticket flow (FR-77–80) keeps working unchanged and now posts a journal entry on approval.

### 3.3 FR-156 / FR-157 — Statements and accounts (0621)
P&L (accrual, over a range) · Balance Sheet (as at a date) · Cash Flow · Aging (receivables and payables by bucket) · Advanced (General Ledger, Trial Balance, Journal). All exportable. Plus cash and bank accounts, capital accounts (owner contributions and withdrawals), and fixed assets with depreciation.

**Every statement is computed from the journal only.** No statement may read the ledger projection, or the two can disagree and there is no way to tell which is right.

### 3.4 FR-158 — Undo window (0622)
Actions that move money or state get a five-minute **Undo** in the top bar, with redo, and refusal with a specific reason when: the referenced record no longer exists · it was recreated · it changed after the action · the window closed.

**An undo is a reversing journal entry, never a delete.** Both the original action and its reversal remain in the audit log. This is why undo is safe to offer at all, and it is the one property that must not be traded away for a simpler implementation.

### 3.5 FR-159 — Agent approval gate
Split by money direction:
- Actions **costing** the agent (activate, renew, plan change, collect payment) execute immediately.
- Actions **returning** money (undo, delete subscriber, void invoice) park as approval requests.

Requests queue for a manager holding the approvals right; approve or reject with an optional reason. **Approval replays the exact same code path the agent would have run** — one implementation, never a second "approved" variant that can drift from the original. Agents see and may withdraw their own pending requests; decided requests stay in history.

Plus per-agent **commission percentage** and **credit limit** (how far a prepaid balance may go negative), threaded into the existing v2-9 wholesale/retail resolution rather than replacing it.

## 4. Frozen contracts

- Journal entries are **balanced or refused**, append-only, IQD single-book (v3-2 stands).
- Statements read the journal only, never the projection.
- Undo writes a reversing entry; it never deletes, and both sides stay in the audit log.
- Approval replays the original code path. A separate approved-action implementation is forbidden.
- The existing ledger, receipts, refunds and payment-ticket contracts are unchanged in this phase — they gain a journal posting, nothing else.

## 5. Acceptance gate

| # | Item | Proof |
|---|---|---|
| 1 | Entries always balance | Constraint test: an unbalanced entry is refused at the DB level |
| 2 | Backfill reconciles | Every existing ledger row projected; reconciliation report shows zero variance on a restored realistic dataset |
| 3 | Dual-write agrees | Ledger and journal agree on every balance and every total, asserted by test |
| 4 | Invoice modes | full / pro-rated / no-charge each verified; no-charge refused without the permission |
| 5 | Renewal auto-invoices | Priced renewal produces an invoice and a balanced entry |
| 6 | Statements tie to the journal | P&L, balance sheet, cash flow, aging, trial balance all reconcile; trial balance nets to zero |
| 7 | Undo reverses | Money action undone within the window produces a reversing entry; original and reversal both in the audit log |
| 8 | Undo refuses correctly | Each of the four refusal conditions verified with its specific message |
| 9 | Approval gate splits correctly | Costing actions execute; returning actions park |
| 10 | Approval replays one path | Code review + test proving approve calls the same function the agent would have |
| 11 | Credit limit and commission | Agent blocked at the limit; commission split lands as explicit journal lines |
| 12 | Reseller isolation intact | A reseller still cannot see owner cost or another reseller's wholesale — fields omitted, not nulled |
| 13 | Migrations linear | 0615–0622 clean on a restored dataset |
| 14 | Security review | Money paths reviewed and recorded in the gate result |

## 6. Commit plan

1. `feat(billing): chart of accounts and balanced journal entries (FR-153, 0615-0616)`
2. `feat(billing): backfill journal from the existing ledger with reconciliation report (FR-153, 0617)`
3. `feat(billing): every money action posts a journal entry alongside the ledger (FR-153)`
4. `feat(billing): invoices with full/pro-rated/no-charge modes (FR-154, 0618)`
5. `feat(billing): expenses, receipts, payments and their lifecycle (FR-155, 0619-0620)`
6. `feat(billing): P&L, balance sheet, cash flow, aging, advanced statements (FR-156, 0621)`
7. `feat(billing): cash, bank, capital accounts and fixed assets (FR-157)`
8. `feat(billing): five-minute undo window with reversing entries (FR-158, 0622)`
9. `feat(billing): agent approval gate replaying the original code path (FR-159)`
10. `feat(billing): agent commission percentage and credit limit (FR-159)`
11. `docs: v4-9 gate result`

## 7. Kickoff prompt

```
Execute phase v4-9 (Double-entry accounting, undo & approvals) of the HikRAD v4 program,
solo and sequentially, start to finish, in this repo.

Read first:
  - docs/v4/00-v4-master-plan.md                 (Decision 63)
  - docs/v4/phases/phase-v4-9-accounting/00-phase.md   (this brief — §3.1's staging rule
    and §4 are both binding)
  - docs/prd/05-billing-payments-vouchers.md     (FR-19-26, FR-59, FR-68-80, FR-100-102)
  - docs/v3/phases/phase-v3-2-currency-single-book/  (the single-book IQD model that stands)
  - backend/internal/billing/

Deliver FR-153 through FR-159. Migrations 0615-0622; confirm the repo maximum first.

THIS PHASE TOUCHES MONEY. The staging rule in §3.1 is not optional:

  The journal is added BESIDE the existing ledger. Both are written. A reconciliation test
  asserts they agree on every figure. The ledger becomes a projection only in a LATER
  release, after that has held. The existing ledger is never reinterpreted, rewritten, or
  deleted in this phase.

HikRAD has already been burned once by a money-model rework (v2-4 built per-currency
wallets; v3-2 reversed them with an irreversible migration). Stage this one.

Binding invariants, each with a test:
  - Entries BALANCE OR ARE REFUSED, enforced by a DB constraint, not by convention.
  - Statements read the JOURNAL ONLY, never the ledger projection. If both are readable,
    they will eventually disagree and no one will know which is right.
  - Undo writes a REVERSING ENTRY. It never deletes. Both the action and its reversal stay
    in the audit log. This property is why undo is safe to offer at all — do not trade it
    for a simpler implementation.
  - Approval REPLAYS THE ORIGINAL CODE PATH. One implementation. A separate "approved
    action" variant will drift from the agent's own path and the divergence will be a money
    bug (gate item 10 proves this by review and by test).
  - The single-book IQD model (v3-2) stands. Do not reintroduce a currency dimension.
  - Reseller isolation is a commercial-severity contract: cost and wholesale are OMITTED
    from a reseller's responses, not nulled (item 12).

Online payment gateways are OUT (Decision 67) — deferred until real merchant credentials
exist. The FR-23 mistake (a full gateway layer built for accounts that never existed, then
deleted) is not repeated. Do not build a gateway abstraction "ready for later".

Finish with docs/history/gates/v4-9.md, including the backfill reconciliation report.
```
