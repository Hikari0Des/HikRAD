# v2-02 — Manual payment providers (transfer-proof payments)

> Owner request 2026-07-17, replacing the withdrawn AsiaHawala/Areeba gateway adapters (PRD Decision 29): **no gateway API integrations at all**. Payments work like FR-59 scratch cards, generalized: the owner adds payment providers **by name** (AsiaHawala, ZainCash, FIB, a bank — anything), the subscriber transfers money to their manager's account at that provider and submits proof from the portal, and the owning manager/agent/reseller reviews it. Fully offline-capable (NFR-7) — with this, **nothing in HikRAD requires internet for payments**.

## 1. Problem

1. v1's online-payment story (Phase-4 `PaymentGateway` interface) is blocked forever on merchant accounts/API docs that don't exist in practice. What Iraqi ISPs actually do: the subscriber sends money over a wallet/bank app to the agent's personal/business account and sends a screenshot.
2. FR-59 already encodes the right trust model (submit → 1-day provisional service → human verification → full renewal or rollback) but is hardwired to telecom airtime cards: no named providers, no file attachments, no per-agent receiving accounts.
3. Receiving accounts are per-human: the agent who owns the subscriber is the one whose wallet receives the transfer and the one who can check it arrived. A global account list is wrong.

## 2. Requirements (draft — renumber as FR-6x/7x at kickoff)

### FR-A — Provider catalog, per-manager accounts, per-manager method enablement
- `payment_providers`: owner-managed catalog — name (trilingual-capable label), optional logo asset (local file, NFR-7), free-text transfer instructions template, enabled. No API fields; a provider is just a *name* subscribers recognize.
- `manager_provider_accounts`: per manager (admin/operator/agent/reseller), per provider — the receiving account details shown to the subscriber (account number / phone / IBAN / exact recipient name, plus optional per-manager instruction override). CRUD by the manager themselves (`me/...`) and by admins for anyone; encrypted at rest like other secrets is NOT required (these are deliberately shown to subscribers) but audit-logged.
- **Per-manager method enablement (owner clarification 2026-07-17)**: each manager chooses which payment methods their subscribers can use and specifies their details — every catalog provider they have an account for, plus the built-in methods (scratch card FR-59, voucher redeem FR-23) as toggles. A method a manager disables never appears to their subscribers.
- **Visibility rule**: a subscriber sees exactly the methods their **owning manager** (`subscribers.owner_manager_id`) has enabled (for providers: enabled + account details present). Open at kickoff: fallback when the owner has none (inherit admin/global accounts vs. show nothing) — owner decides in the phase brief.

### FR-B — One unified portal payment screen, submissions with attachments
- **One combined "Pay" surface (owner clarification 2026-07-17)**: the portal shows all enabled methods together — transfer providers, scratch card, voucher — as one picker; scratch-card entry stops being a separate screen and becomes a method tile in the same flow. Per-method form after picking.
- Provider (transfer-proof) form: amount, transfer reference/date, note, **file attachments** (receipt screenshot/PDF; local disk storage under the data dir, size/type limits, virus-free by construction: never executed, served back only to authorized reviewers with `Content-Disposition: attachment`).
- **Trial-day rule (owner clarification 2026-07-17, amends FR-59's cooldown)**: the **first** submission in a payment cycle grants the **1-day provisional renewal immediately** ("test internet", CoA-applied). If a request is **rejected, the subscriber may retry right away — but retries get NO free day** (they just go `pending` until reviewed). Trial eligibility resets when a request is approved (the next cycle's first attempt earns a day again). One pending request per subscriber at a time; every state change audit-logged and notified (portal + FR-55 channels).

### FR-C — Ticket-style review with hierarchy-wide visibility
- A payment request behaves like a **P2P ticket**: a timeline/log of every event (submitted, attachments added, provisional granted, approved/rejected with the reviewer's note, who acted, when).
- The ticket lands in the **owning manager's** queue (falls back to any holder of the verify permission). **Managers above see everything (owner clarification 2026-07-17)**: admins/global managers get the full payment log across all agents — every ticket, searchable/filterable (by agent, provider, state, date), not just their own queue; scoped agents see only their own subscribers' tickets (normal `auth.ScopeFilter` semantics).
- Reviewer sees the submission + attachments, checks their own wallet/bank statement out-of-band, then **approves** (→ full renewal from the trial's start, standard FR-19 path, ledger entries booked per the v2-9 wholesale/retail model against the owning agent) or **rejects** with a required note (→ reversing entries, expiry rolled back per FR-9; subscriber may resubmit, without a new free day).
- Panel: queue screen (badge count), the all-payments log view for global managers, per-ticket detail with attachment viewer + timeline.

### FR-D — Notifications on both sides (owner clarification 2026-07-17)
- **Subscriber**: notified at every ticket state change — submitted/pending received (with the provisional-day status), approved (renewed until X), rejected (with the reviewer's note, and that a retry earns no free day) — via portal notifications + the FR-55 channels (WhatsApp etc.).
- **Manager**: the **owning manager** is notified the moment a new ticket lands in their queue (in-app + panel web push; badge count), and on any action another manager takes on their ticket (e.g. an admin approves it). Above-managers can opt into all-tickets notifications via their v2-6 `notification_prefs`.
- Same delivery machinery as existing alerts/notifications (FR-55 + panel push) — no new channel infrastructure; every notification corresponds to a timeline event, never invented separately.

### FR-E — Retire the gateway surface
- The Phase-4 `PaymentGateway` interface and any gateway config UI are removed or clearly quarantined (owner's call at kickoff — Decision 29 kept the interface, but with this feature there is no consumer left). E-wallet references in docs/NFR-7's "only online-dependent feature" caveat are cleaned up: after this phase HikRAD is 100% offline-capable.

## 3. Impact map

| Touched | Built in | Change |
|---|---|---|
| `internal/billing` (card_payments, FR-59 flow) | Phase 3/4 (D) | generalize to provider-based requests + attachments + owner-scoped review |
| `internal/portalapi` | Phase 4 (D) | provider list (owner-scoped), submission endpoint, upload handling |
| Panel billing screens | Phase 3/5 (E) | provider catalog CRUD, my-accounts screen, review queue upgrade |
| Portal pay flow | Phase 4 (F) | unified method picker + transfer form + attachment upload + state tracking |
| Notifications (FR-55 channels + panel push) | Phases 3/4 (C/E) | ticket-event notification matrix, both sides; manager prefs via v2-6 |
| `docs/prd/05-billing-payments-vouchers.md`, 07-portal | — | own the new FRs; amend FR-59's scope note |

## 4. Acceptance sketch

- Owner creates provider "AsiaHawala"; agent Hassan adds his AsiaHawala wallet number and enables it + scratch cards, disables vouchers. His subscribers' portal shows one Pay screen with exactly those two tiles (Hassan's wallet number under AsiaHawala); a subscriber owned by agent Zainab sees Zainab's method set, not Hassan's.
- Subscriber submits a transfer proof (photo) → is online within seconds on a 1-day provisional window → Hassan approves next morning → subscriber is renewed a full month **from the trial's start**, ledger consistent with a normal Hassan renewal.
- Hassan rejects a fake proof with a note → expiry rolls back; the subscriber resubmits a real proof immediately — allowed, but **no second free day** — and gets the full month on approval. After that approval, next cycle's first attempt earns the trial day again.
- An admin opens the payments log and sees every ticket across all agents with its full timeline (who submitted, who granted the provisional, who approved/rejected and why); Hassan sees only his own subscribers' tickets.
- Every state change notifies both sides: the subscriber sees pending/approved/rejected (with the note) in the portal and over the FR-55 channels; Hassan gets an in-app + push notification the moment the ticket arrives, and another if an admin acts on it instead of him.
- Attachments are stored locally, only reviewers can fetch them, and nothing about the flow touches the internet.

## 5. AI kickoff prompt (paste into a fresh Claude Code session at repo root)

```text
You are working in the HikRAD repo. We are starting v2 phase 2: manual payment providers (transfer-proof payments replacing gateway adapters — PRD Decisions 29/30). You work SOLO — no parallel agents; execute sequentially (schema → billing core → portal → panel queue), committing in reviewable chunks.

Read, in this order and nothing else yet: CLAUDE.md, docs/v2/phases/00-v2-execution-plan.md, docs/v2/02-manual-payment-providers.md, docs/prd/05-billing-payments-vouchers.md (FR-59 + FR-19), docs/prd/07-subscriber-portal-pwa.md, backend/internal/billing/ (card-payment/trial-window flow), and the v2-9 phase result (wholesale/retail ledger semantics).

Step 1 — Amend the docs (single commit): new FR rows + Decisions Log row in docs/PRD.md, update sub-PRDs 05 and 07, docs/prd/00-index.md. Resolve the open question (owner-has-no-account fallback) with me before freezing.

Step 2 — Create docs/v2/phases/phase-v2-2-manual-payments/00-phase.md with frozen contracts (provider + manager-account + per-manager method-enablement schemas, unified portal Pay contract listing all enabled methods incl. scratch/voucher tiles, submission request/response with upload limits, ticket states + timeline events, trial-eligibility rule — first attempt per cycle only, reset on approval — queue/log scoping rules, notification matrix — which timeline event notifies whom on which channel — ledger booking on approve/reject) and the integration gate (trial grant on first attempt + NO trial on post-rejection retry + reset-on-approval tests, owner-scoping + admin-sees-all-log tests, both-sides notification tests per state change, attachment authz test, unified-Pay-screen test; migration budget 0580–0589 — but numbers are linear, take the next free ones). Scriptable gate items → scripts/gate-v2-phase-2.sh.

Step 3 — Stop and present the phase brief for my confirmation before writing feature code.

Constraints: reuse the FR-59 trial-window machinery — do not invent a second provisional-renewal path — but amend its rejection rule: retries are allowed immediately and simply carry no free day (the old cooldown-blocks-submissions behavior is superseded); the portal has ONE payment surface (scratch cards fold into it, no separate screen); money always flows through the ledger (append-only, derived balances); attachments never leave local disk (NFR-7); portal/panel strings trilingual. Update every doc invalidated; record bugs in docs/ops/known-issues.md.
```
