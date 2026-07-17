# v2-09 — Cost, margin and reseller pricing

> Owner request 2026-07-16. Owner answers on record: cost is **per-plan buy price AND fixed monthly overheads** (both); resellers pay by **prepaid balance** (they top up, renewals deduct). Scheduled with v2-4 because both rework the same money core — doing them separately means reworking the ledger twice.

## 1. Problem

HikRAD knows what a subscriber is **charged** and nothing about what they **cost**. `profiles.price_iqd` is the sell price; there is no buy price, no overhead model, and no margin anywhere in the reports. The owner's actual question — *"am I making money on this plan, and how much?"* — is unanswerable from the product today.

The second half is structural. The owner is himself a reseller (buys upstream capacity, resells it) **and** has resellers beneath him who buy from him. v1 models the second relationship only as far as it needed to for FR-27: an agent has a balance, renewals debit it, and the debit is **the subscriber's price**. So today:

- every reseller pays the same price for the same plan — there is no wholesale price;
- the owner's margin on a reseller's renewal is invisible (it is `sell − cost`, and cost does not exist);
- a reseller cannot see their own margin, because their sell price *is* the only price in the system.

This is the difference between a billing system and a reseller platform, and it is why the owner is currently doing the arithmetic outside HikRAD.

## 2. Scope decisions (owner-confirmed 2026-07-16)

| Question | Answer | Consequence |
|---|---|---|
| What is your upstream cost attached to? | **Both** per-plan buy price and fixed monthly overheads | Two cost sources that must combine without double-counting — see FR-C |
| How do resellers pay you? | **Prepaid balance**, renewals deduct | Builds on v1's existing agent balance + ledger; no credit/invoicing engine needed |
| Sub-resellers (resellers under resellers)? | **OPEN — must be answered at kickoff** | Decides whether pricing is a 2-level override or an N-level tree. Do not start until answered; it is the single biggest fork in this brief |
| Ship with or after multi-currency? | Together (v2-4 adjacent) | A cost price without a currency is the same bug as a sell price without one |

## 3. Requirements (draft — renumber as FR-6x at kickoff)

### FR-A — Plan cost price
- `profiles` gains `cost_price` (+ currency, per v2-4) — what **you** pay upstream for one subscriber-month of this plan.
- Nullable: a plan with no cost recorded reports margin as **unknown**, never as 100% profit. A missing cost must never silently render as zero — that is the difference between "I don't know" and "it's free", and only one of them is safe to put in a revenue report.
- Cost is **versioned, not overwritten**: `profile_cost_history (profile_id, cost, currency, effective_from)`. A renewal's margin is computed against the cost in force **on the renewal date** and stamped onto the ledger row, so re-pricing a plan next month cannot retroactively rewrite last month's reported profit. Same rule the ledger already follows for exchange rates (v2-4 FR-A).

### FR-B — Margin on the ledger
- Every renewal ledger row stamps `cost_at_sale` alongside the amount charged. Margin = `amount − cost_at_sale`, derived, never stored as its own editable number (the FR-19 append-only rule: balances and totals are always derived from the ledger).
- Reports gain per-plan and per-period **revenue / cost / margin**, reconciling to the ledger exactly as FR-40 requires today.

### FR-C — Fixed monthly overheads
- `overheads (name, amount, currency, period_start, period_end|null, notes)` — uplink, staff, power, rent. Entered by an admin; not derived.
- Overheads are **reported separately from per-plan margin, and never allocated onto a plan's margin by default.** Allocating a shared uplink bill across subscribers requires choosing a rule (per head? per Mbps sold? per revenue?), every rule is arguable, and once allocated the number looks like a fact. So: *gross margin* (revenue − plan cost) is per-plan and exact; *net* (gross − overheads for the period) is a period-level figure. An optional allocation view may be added later behind an explicit, labelled rule — but the unallocated numbers stay the source of truth.
- **Open question for kickoff:** should overheads be per-NAS/site as well as global? An ISP with two towers has per-site uplink costs, and "which tower is profitable" is the obvious next question.

### FR-D — Reseller (wholesale) pricing
- `reseller_prices (manager_id, profile_id, price, currency, effective_from)` — what THIS reseller pays for this plan. Falls back to the plan's retail price when unset, which is exactly v1's behaviour, so every existing agent is unaffected on day one.
- At renewal the reseller's **balance is debited their wholesale price**, while the **subscriber is charged the retail price** — two different numbers that v1 conflated into one. The owner's margin on that renewal is `wholesale − cost`; the reseller's is `retail − wholesale`.
- Both legs land on the ledger as an explicit pair, never as one row with two meanings. The append-only ledger must be able to answer "who paid what to whom" without inference.
- Versioned per FR-A's reasoning: changing a reseller's price must not rewrite what they already paid.

### FR-E — Reseller-facing margin view
- A reseller sees their own margin (`retail − their wholesale`) and their balance. They **must not** see the owner's cost price or any other reseller's wholesale price — this is manager scoping (FR-27.2) applied to money, and a leak here is commercially serious, not just a privacy nit.
- The owner sees the full chain.

### FR-F — Pricing algorithm
The renewal price resolution order, most specific first:
1. Subscriber `price_override` (FR-7, exists today).
2. Reseller wholesale price for `(owner_manager_id, profile_id)` — for the **balance debit** leg only.
3. Profile retail price — for the **subscriber charge** leg.

Cost resolution for margin: `profile_cost_history` in force at the renewal date, else unknown.

## 4. Why this is v2, not a patch

It changes what a ledger row **means**. Today one renewal = one debit at one price. After this, one renewal = a retail charge and a wholesale debit with a cost stamp, and the reports derive three different margins from them. Phase 3 froze the ledger contract and Phase 5's reports reconcile against it; both must move together, with the same append-only guarantees. That is a phase, not an edit.

## 5. Explicitly NOT in scope

- Credit limits / postpaid settlement (owner uses prepaid).
- Automatic upstream cost import (no online dependency — NFR-7).
- Commission schemes beyond price differences (percentage commissions, volume tiers). If wanted, they are a later brief; price-difference margin is the model the owner described.

## 6. Kickoff blockers

1. **Sub-resellers: yes or no.** 2-level override vs. N-level tree changes the schema, the scoping rules and the margin math. Everything else here is stable under either answer; this is not.
2. Per-site overheads: global only, or per NAS?
3. Does a reseller's wholesale price need per-subscriber overrides, or is per-plan enough?
