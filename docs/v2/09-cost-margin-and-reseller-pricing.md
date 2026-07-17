# v2-09 — Cost, margin and reseller pricing

> Owner request 2026-07-16. Owner answers on record: cost is **per-plan buy price AND fixed monthly overheads** (both); resellers pay by **prepaid balance** (they top up, renewals deduct). Scheduled with v2-4 because both rework the same money core — doing them separately means reworking the ledger twice.
>
> **Kickoff blockers resolved 2026-07-17 (owner):** (1) sub-resellers — **no, flat 2-level only** (owner → reseller → subscriber; no N-level tree). (2) Overheads — **per-NAS/site as well as global** (an ISP with two towers can see which tower is profitable). (3) Reseller wholesale pricing — **needs per-subscriber overrides too**, layered under the per-plan wholesale price. Requirements renumbered **FR-71–76** below (the brief's original `FR-A`.."renumber as FR-6x at kickoff" note predates v2-4, which has since taken FR-68–70 — see PRD Decision 36).

## 1. Problem

HikRAD knows what a subscriber is **charged** and nothing about what they **cost**. `profiles.price_iqd` is the sell price; there is no buy price, no overhead model, and no margin anywhere in the reports. The owner's actual question — *"am I making money on this plan, and how much?"* — is unanswerable from the product today.

The second half is structural. The owner is himself a reseller (buys upstream capacity, resells it) **and** has resellers beneath him who buy from him. v1 models the second relationship only as far as it needed to for FR-27: an agent has a balance, renewals debit it, and the debit is **the subscriber's price**. So today:

- every reseller pays the same price for the same plan — there is no wholesale price;
- the owner's margin on a reseller's renewal is invisible (it is `sell − cost`, and cost does not exist);
- a reseller cannot see their own margin, because their sell price *is* the only price in the system.

This is the difference between a billing system and a reseller platform, and it is why the owner is currently doing the arithmetic outside HikRAD.

## 2. Scope decisions (owner-confirmed 2026-07-16, kickoff blockers resolved 2026-07-17)

| Question | Answer | Consequence |
|---|---|---|
| What is your upstream cost attached to? | **Both** per-plan buy price and fixed monthly overheads | Two cost sources that must combine without double-counting — see FR-73 |
| How do resellers pay you? | **Prepaid balance**, renewals deduct | Builds on v1's existing agent balance + ledger; no credit/invoicing engine needed |
| Sub-resellers (resellers under resellers)? | **No — flat 2-level only** (owner → reseller → subscriber) | Pricing is a flat override table keyed on `(manager_id, profile_id[, subscriber_id])`, never a recursive tree. No manager-hierarchy/ancestry model needed — FR-27's existing scoping (a manager sees only their own subscribers) is sufficient, unchanged. |
| Ship with or after multi-currency? | Together (v2-4 adjacent) | A cost price without a currency is the same bug as a sell price without one |
| Per-site overheads? | **Yes — per-NAS/site as well as global** | `overheads` gets an optional `nas_id`; a null `nas_id` row is a global (whole-business) overhead. "Which tower is profitable" becomes answerable. |
| Per-subscriber wholesale overrides? | **Yes**, layered under the per-plan wholesale price | `reseller_prices` needs a `subscriber_id` column (nullable — null means "this reseller's plan-wide price"); resolution tries subscriber-specific before plan-wide (FR-76). |

## 3. Requirements (FR-71–76 — see PRD Decision 36; the master PRD is the numbering authority, not this brief)

### FR-71 — Plan cost price
- `profiles` gains `cost_price` (+ currency, per v2-4 FR-68) — what **you** pay upstream for one subscriber-month of this plan.
- Nullable: a plan with no cost recorded reports margin as **unknown**, never as 100% profit. A missing cost must never silently render as zero — that is the difference between "I don't know" and "it's free", and only one of them is safe to put in a revenue report.
- Cost is **versioned, not overwritten**: `profile_cost_history (profile_id, cost, currency, effective_from)`. A renewal's margin is computed against the cost in force **on the renewal date** and stamped onto the ledger row, so re-pricing a plan next month cannot retroactively rewrite last month's reported profit. Same rule the ledger already follows for exchange rates (v2-4 FR-68.3/68.4).

### FR-72 — Margin on the ledger
- Every renewal ledger row stamps `cost_at_sale` alongside the amount charged. Margin = `amount − cost_at_sale`, derived, never stored as its own editable number (the FR-19 append-only rule: balances and totals are always derived from the ledger).
- Reports gain per-plan and per-period **revenue / cost / margin**, reconciling to the ledger exactly as FR-40 requires today.

### FR-73 — Fixed monthly overheads (global and per-site)
- `overheads (name, amount, currency, nas_id NULL, period_start, period_end|null, notes)` — uplink, staff, power, rent. Entered by an admin; not derived. `nas_id NULL` = a whole-business (global) overhead; a non-null `nas_id` scopes it to that site/tower.
- Overheads are **reported separately from per-plan margin, and never allocated onto a plan's margin by default.** Allocating a shared uplink bill across subscribers requires choosing a rule (per head? per Mbps sold? per revenue?), every rule is arguable, and once allocated the number looks like a fact. So: *gross margin* (revenue − plan cost) is per-plan and exact; *net* (gross − overheads for the period) is a period-level figure, computed both globally and — where every subscriber in the period can be attributed to one NAS — per-site. An optional per-subscriber allocation view may be added later behind an explicit, labelled rule; the unallocated numbers stay the source of truth.
- A per-site overhead report sums that NAS's own `overheads` rows plus a pro-rata share of NULL-`nas_id` (global) rows is explicitly **not attempted** here — see the same "every rule is arguable" reasoning above. Per-site net margin only ever nets a site's *own* tagged overheads against that site's revenue; global overheads stay a separate whole-business figure, never silently blended in.

### FR-74 — Reseller (wholesale) pricing, with optional per-subscriber override
- `reseller_prices (manager_id, profile_id, subscriber_id NULL, price, currency, effective_from)` — what THIS reseller pays for this plan, optionally narrowed to one specific subscriber. `subscriber_id NULL` = the reseller's plan-wide wholesale price; a non-null `subscriber_id` overrides it for that one subscriber only (e.g. a reseller's VIP customer they've agreed a special wholesale rate for). Falls back to the plan's retail price when no row matches at all, which is exactly v1's behaviour, so every existing agent is unaffected on day one.
- At renewal the reseller's **balance is debited their resolved wholesale price** (FR-76's resolution order), while the **subscriber is charged the retail price** — two different numbers that v1 conflated into one. The owner's margin on that renewal is `wholesale − cost`; the reseller's is `retail − wholesale`.
- Both legs land on the ledger as an explicit pair, never as one row with two meanings. The append-only ledger must be able to answer "who paid what to whom" without inference.
- Versioned per FR-71's reasoning: changing a reseller's price (plan-wide or per-subscriber) must not rewrite what they already paid. No sub-reseller tree (2-level only, per the resolved kickoff blocker) — `manager_id` here is always a direct reseller of the owner, never a reseller-of-a-reseller.

### FR-75 — Reseller-facing margin view
- A reseller sees their own margin (`retail − their wholesale`) and their balance. They **must not** see the owner's cost price or any other reseller's wholesale price — this is manager scoping (FR-27.2) applied to money, and a leak here is commercially serious, not just a privacy nit.
- The owner sees the full chain.

### FR-76 — Pricing algorithm
The renewal price resolution order, most specific first:
1. Subscriber `price_override` (FR-7, exists today) — the subscriber's own charge, unaffected by any of this phase's reseller mechanics.
2. Reseller wholesale price for `(owner_manager_id, profile_id, subscriber_id)` if a per-subscriber row exists (FR-74) — for the **balance debit** leg only.
3. Reseller wholesale price for `(owner_manager_id, profile_id)`, `subscriber_id IS NULL` (FR-74) — for the **balance debit** leg only.
4. Profile retail price — for the **subscriber charge** leg.

Cost resolution for margin: `profile_cost_history` in force at the renewal date, else unknown.

## 4. Why this is v2, not a patch

It changes what a ledger row **means**. Today one renewal = one debit at one price. After this, one renewal = a retail charge and a wholesale debit with a cost stamp, and the reports derive three different margins from them. Phase 3 froze the ledger contract and Phase 5's reports reconcile against it; both must move together, with the same append-only guarantees. That is a phase, not an edit.

## 5. Explicitly NOT in scope

- Credit limits / postpaid settlement (owner uses prepaid).
- Automatic upstream cost import (no online dependency — NFR-7).
- Commission schemes beyond price differences (percentage commissions, volume tiers). If wanted, they are a later brief; price-difference margin is the model the owner described.

## 6. Kickoff blockers — RESOLVED 2026-07-17

1. **Sub-resellers: no.** Flat 2-level only (owner → reseller → subscriber). See §2/§3 FR-74.
2. **Per-site overheads: yes**, in addition to global. See §2/§3 FR-73.
3. **Per-subscriber wholesale overrides: yes**, layered under the per-plan wholesale price. See §2/§3 FR-74/FR-76.
