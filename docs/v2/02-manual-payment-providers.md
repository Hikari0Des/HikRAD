# v2-02 — Manual payment providers (transfer-proof payments)

> Owner request 2026-07-17, replacing the withdrawn AsiaHawala/Areeba gateway adapters (PRD Decision 29): **no gateway API integrations at all**. Payments work like FR-59 scratch cards, generalized: the owner adds payment providers **by name** (AsiaHawala, ZainCash, FIB, a bank — anything), the subscriber transfers money to their manager's account at that provider and submits proof from the portal, and the owning manager/agent/reseller reviews it. Fully offline-capable (NFR-7) — with this, **nothing in HikRAD requires internet for payments**.

## 1. Problem

1. v1's online-payment story (Phase-4 `PaymentGateway` interface) is blocked forever on merchant accounts/API docs that don't exist in practice. What Iraqi ISPs actually do: the subscriber sends money over a wallet/bank app to the agent's personal/business account and sends a screenshot.
2. FR-59 already encodes the right trust model (submit → 1-day provisional service → human verification → full renewal or rollback) but is hardwired to telecom airtime cards: no named providers, no file attachments, no per-agent receiving accounts.
3. Receiving accounts are per-human: the agent who owns the subscriber is the one whose wallet receives the transfer and the one who can check it arrived. A global account list is wrong.

## 2. Requirements (draft — renumber as FR-6x/7x at kickoff)

### FR-A — Provider catalog + per-manager account details
- `payment_providers`: owner-managed catalog — name (trilingual-capable label), optional logo asset (local file, NFR-7), free-text transfer instructions template, enabled. No API fields; a provider is just a *name* subscribers recognize.
- `manager_provider_accounts`: per manager (admin/operator/agent/reseller), per provider — the receiving account details shown to the subscriber (account number / phone / IBAN / exact recipient name, plus optional per-manager instruction override). CRUD by the manager themselves (`me/...`) and by admins for anyone; encrypted at rest like other secrets is NOT required (these are deliberately shown to subscribers) but audit-logged.
- **Visibility rule**: a subscriber sees exactly the providers for which their **owning manager** (`subscribers.owner_manager_id`) has enabled account details. Open at kickoff: fallback when the owner has none (inherit admin/global accounts vs. show nothing) — owner decides in the phase brief.

### FR-B — Portal submission with attachments
- Portal "Pay" flow: pick provider → see the owner's account details + instructions → form: amount, transfer reference/date, note, **file attachments** (receipt screenshot/PDF; local disk storage under the data dir, size/type limits, virus-free by construction: never executed, served back only to authorized reviewers with `Content-Disposition: attachment`).
- On submit: exactly the FR-59 machinery — **1-day provisional renewal immediately** ("test internet", CoA-applied), request goes `pending`, one pending request per subscriber, rejected attempts trigger the configurable cooldown, every step audit-logged and notified (portal + FR-55 channels).

### FR-C — Review queue for the owning manager
- The request lands in the **owning manager's** queue (falls back to any holder of the verify permission; admins see all). Reviewer sees the submission + attachments, checks their own wallet/bank statement out-of-band, then **approves** (→ full renewal from the trial's start, standard FR-19 path, ledger entries booked per the v2-3b wholesale/retail model against the owning agent) or **rejects** (→ reversing entries, expiry rolled back per FR-9, cooldown starts).
- Panel: queue screen (badge count), per-request detail with attachment viewer, approve/reject with required note on reject.

### FR-D — Retire the gateway surface
- The Phase-4 `PaymentGateway` interface and any gateway config UI are removed or clearly quarantined (owner's call at kickoff — Decision 29 kept the interface, but with this feature there is no consumer left). E-wallet references in docs/NFR-7's "only online-dependent feature" caveat are cleaned up: after this phase HikRAD is 100% offline-capable.

## 3. Impact map

| Touched | Built in | Change |
|---|---|---|
| `internal/billing` (card_payments, FR-59 flow) | Phase 3/4 (D) | generalize to provider-based requests + attachments + owner-scoped review |
| `internal/portalapi` | Phase 4 (D) | provider list (owner-scoped), submission endpoint, upload handling |
| Panel billing screens | Phase 3/5 (E) | provider catalog CRUD, my-accounts screen, review queue upgrade |
| Portal pay flow | Phase 4 (F) | provider picker + transfer form + attachment upload + state tracking |
| `docs/prd/05-billing-payments-vouchers.md`, 07-portal | — | own the new FRs; amend FR-59's scope note |

## 4. Acceptance sketch

- Owner creates provider "AsiaHawala"; agent Hassan adds his AsiaHawala wallet number. A subscriber owned by Hassan sees AsiaHawala (with Hassan's number); a subscriber owned by agent Zainab does not, until Zainab adds hers.
- Subscriber submits a transfer proof (photo) → is online within seconds on a 1-day provisional window → Hassan approves next morning → subscriber is renewed a full month **from the trial's start**, ledger consistent with a normal Hassan renewal; rejecting instead rolls expiry back and starts the cooldown.
- Attachments are stored locally, only reviewers can fetch them, and nothing about the flow touches the internet.

## 5. AI kickoff prompt (paste into a fresh Claude Code session at repo root)

```text
You are working in the HikRAD repo. We are starting v2 phase 7: manual payment providers (transfer-proof payments replacing gateway adapters — PRD Decisions 29/30). You work SOLO — no parallel agents; execute sequentially (schema → billing core → portal → panel queue), committing in reviewable chunks.

Read, in this order and nothing else yet: CLAUDE.md, docs/v2/phases/00-v2-execution-plan.md, docs/v2/02-manual-payment-providers.md, docs/prd/05-billing-payments-vouchers.md (FR-59 + FR-19), docs/prd/07-subscriber-portal-pwa.md, backend/internal/billing/ (card-payment/trial-window flow), and the v2-3b phase result (wholesale/retail ledger semantics).

Step 1 — Amend the docs (single commit): new FR rows + Decisions Log row in docs/PRD.md, update sub-PRDs 05 and 07, docs/prd/00-index.md. Resolve the open question (owner-has-no-account fallback) with me before freezing.

Step 2 — Create docs/v2/phases/phase-v2-7-manual-payments/00-phase.md with frozen contracts (provider + manager-account schemas, submission request/response with upload limits, request states + queue scoping rule, ledger booking on approve/reject) and the integration gate (trial-window grant/rollback tests, owner-scoping test, attachment authz test; migration budget 0580–0589 — but numbers are linear, take the next free ones). Scriptable gate items → scripts/gate-v2-phase-7.sh.

Step 3 — Stop and present the phase brief for my confirmation before writing feature code.

Constraints: reuse the FR-59 trial-window machinery — do not invent a second provisional-renewal path; money always flows through the ledger (append-only, derived balances); attachments never leave local disk (NFR-7); portal/panel strings trilingual. Update every doc invalidated; record bugs in docs/ops/known-issues.md.
```
