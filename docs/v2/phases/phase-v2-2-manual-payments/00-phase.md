# Phase v2-2 — Manual Payment Providers (Transfer-Proof Payments)

Source brief: [docs/v2/02-manual-payment-providers.md](../../02-manual-payment-providers.md). Requirements FR-77–80 (PRD Decision 37; sub-PRD [05-billing-payments-vouchers.md](../../../prd/05-billing-payments-vouchers.md), FR-78's portal half cross-owned by sub-PRD [07-subscriber-portal-pwa.md](../../../prd/07-subscriber-portal-pwa.md) FR-42). Retires FR-23 (e-wallet gateways, Decision 37) entirely. Builds on v2-9's wholesale/retail ledger split (FR-74/76) for the approve-side ledger booking, and reuses FR-59's exact trial-window renewal mechanism rather than inventing a second one.

**Kickoff blockers, resolved by the owner 2026-07-17 (binding, not re-litigated by this brief):**
1. **No-account fallback: show nothing.** A subscriber never sees a payment method their owning manager hasn't personally configured a receiving account for. No inheritance from a global/admin account exists anywhere in the resolution (FR-77.4).
2. **The Phase-4 `PaymentGateway` interface is removed entirely**, not quarantined. `internal/billing`'s gateway adapters (including the mock adapter and its dev-simulate endpoint), the panel's gateway-config screen, and the `payment_intents` table/handlers are deleted in this phase, not merely hidden.

## 1. Problem (restated from the source brief)

v1's e-wallet gateway story never had a real merchant account or API docs (Decision 29) and cannot ship. What Iraqi ISPs actually do: the subscriber transfers money to their agent's personal wallet/bank account and sends a screenshot as proof. FR-59 already has the right trust model (submit → 1-day provisional service → human verification → full renewal or rollback) but is hardwired to telecom scratch cards: no named providers, no attachments, no per-agent receiving accounts, and no visibility above the single reviewing manager. This phase generalizes FR-59's machinery into a named-provider, ticket-based, hierarchy-visible system, and removes the gateway surface that never had a path to production.

## 2. Scope for this implementation pass

1. **Schema** — provider catalog, per-manager accounts, per-manager method toggles, the unified `payment_tickets` table (generalizing `card_payments`, migrated forward losslessly), attachments, and the event timeline (C1–C6).
2. **Backend** — submission (trial grant, reused from FR-59.1 verbatim), approve/reject (reused from FR-59.2, now threading FR-74/76's wholesale/retail split), queue/log scoping (C7–C9), attachment storage + authenticated retrieval (C10), the notification matrix (C11).
3. **Gateway removal** — delete `PaymentGateway`, the mock adapter, `payment_intents`, and the panel gateway-config screen (C12).
4. **Portal** — one unified Pay screen replacing the gateway list and the separate scratch-card screen (C13).
5. **Panel** — provider catalog CRUD, my-accounts + method-toggle screen, the upgraded review queue (own queue + admin all-tickets log) with attachment viewer and timeline (C14).
6. **Gate** — DB-gated tests for trial-grant-on-first-attempt, no-trial-on-retry, reset-on-approval, owner-scoping vs. admin-sees-all, both-sides notifications, attachment authorization, and the lossless `card_payments` → `payment_tickets` migration; `scripts/gate-v2-phase-2.sh`; `gate-result.md`.

Commit in reviewable chunks along these boundaries (schema+migration / submission+trial / approve+reject+ledger / queue+scoping / attachments / notifications / gateway removal / portal / panel / gate) — this phase touches more surface area than v2-4 or v2-9 (portal UI, file storage, a notification matrix), so more chunks than either.

## Migration budget 0580–0589

(Verify the repo's actual max migration number hasn't advanced past 0589 before implementing; if so, take the next free number instead per the standing linear-numbering rule. At the time this brief was written, the repo's max was 0543.)

| Migration | Owns |
|---|---|
| `0580_payment_providers` | `payment_providers(id uuid PK, name text NOT NULL, logo_path text, instructions_template text NOT NULL DEFAULT '', enabled boolean NOT NULL DEFAULT true, created_at timestamptz NOT NULL DEFAULT now())`. |
| `0581_manager_provider_accounts` | `manager_provider_accounts(id uuid PK, manager_id uuid NOT NULL REFERENCES managers(id) ON DELETE CASCADE, provider_id uuid NOT NULL REFERENCES payment_providers(id) ON DELETE CASCADE, account_details text NOT NULL, instructions_override text, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now())`; `UNIQUE (manager_id, provider_id)` — one account per manager per provider, edited in place (this row is a current-state fact shown to subscribers, not a versioned ledger-adjacent figure like FR-71/74's cost/wholesale prices, so in-place UPDATE is correct here, not append-only). |
| `0582_manager_method_settings` | `manager_method_settings(manager_id uuid NOT NULL REFERENCES managers(id) ON DELETE CASCADE, method_key text NOT NULL, enabled boolean NOT NULL DEFAULT false, PRIMARY KEY (manager_id, method_key))`. `method_key` is either a `payment_providers.id` (as text) or one of the two literal built-in keys `'scratch_card'` / `'voucher'` — enforced in application code, not a DB FK (a provider row and a built-in key are different id-spaces; a CHECK constraint cannot reference a foreign table conditionally). |
| `0583_payment_tickets` | `payment_tickets(id uuid PK, subscriber_id uuid NOT NULL REFERENCES subscribers(id), profile_id uuid NOT NULL REFERENCES profiles(id), method_key text NOT NULL, provider_id uuid REFERENCES payment_providers(id), amount bigint NOT NULL, currency text NOT NULL REFERENCES currencies(code), transfer_reference text, transfer_date timestamptz, note text NOT NULL DEFAULT '', method_detail jsonb NOT NULL DEFAULT '{}', state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','approved','rejected')), trial_ledger_tx_id uuid REFERENCES ledger_transactions(id), approve_ledger_tx_id uuid REFERENCES ledger_transactions(id), decided_by uuid, decided_at timestamptz, reject_reason text, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now())`. `method_detail` carries the one field scratch cards need that nothing else does (`{"card_type": "...", "card_code_enc": "base64..."}`) — kept as JSONB rather than nullable card-only columns bolted onto a table most rows will never use, the standard shape for "one field set differs per discriminator, everything else is shared." Same partial-unique + index pattern `card_payments` already has: `CREATE UNIQUE INDEX payment_tickets_one_pending_idx ON payment_tickets (subscriber_id) WHERE state = 'pending'`; `payment_tickets_subscriber_idx (subscriber_id, created_at DESC)`; `payment_tickets_state_idx (state, created_at DESC)`; `payment_tickets_owner_idx` via a join to `subscribers.owner_manager_id` (no denormalized owner column — resolved through the existing FK, consistent with how every other scoped list in this codebase resolves ownership). |
| `0584_payment_ticket_attachments` | `payment_ticket_attachments(id uuid PK, ticket_id uuid NOT NULL REFERENCES payment_tickets(id) ON DELETE CASCADE, filename text NOT NULL, stored_path text NOT NULL, content_type text NOT NULL, size_bytes bigint NOT NULL, uploaded_at timestamptz NOT NULL DEFAULT now())`. |
| `0585_payment_ticket_events` | `payment_ticket_events(id uuid PK, ticket_id uuid NOT NULL REFERENCES payment_tickets(id) ON DELETE CASCADE, event_type text NOT NULL, actor_manager_id uuid, note text, at timestamptz NOT NULL DEFAULT now())`; index `(ticket_id, at)`. `event_type` values: `submitted`, `attachment_added`, `trial_granted`, `approved`, `rejected`. |
| `0586_card_payments_migrate_to_tickets` | **The lossless-migration migration** (mirrors v2 phase 1's `TestServiceTypeMigrationLossless` pattern, gate item 1): backfills every existing `card_payments` row into `payment_tickets` — `method_key='scratch_card'`, `provider_id=NULL`, `amount`/`currency` resolved from the linked `trial_ledger_tx_id`'s ledger row (or, if null, the profile's current price/currency — a pre-migration pending card payment with no trial entry yet is not possible given FR-59.1's atomicity, so this fallback is defensive, not expected to fire), `method_detail={"card_type": card_type, "card_code_enc": base64(card_code_enc)}`, every other column copied 1:1 (`state`, `trial_ledger_tx_id`, `approve_ledger_tx_id`, `decided_by`, `decided_at`, `reject_reason`, `created_at`, `updated_at`). One synthesized `submitted` event per migrated row (backdated to `created_at`) so the timeline UI never shows a ticket with zero history. `card_payments` is **dropped** after backfill (one canonical table going forward, not two overlapping ones) — this migration is the one genuinely irreversible step in this phase, hence its own dedicated number and its own dedicated gate test. |
| `0587_drop_payment_intents` | Drops the Phase-4 `payment_intents` table entirely (FR-23 retired, kickoff blocker 2). No backfill needed — a `payment_intents` row was always a superseded-by-webhook-confirmation transient record, never a record of money that actually moved (that's `ledger_transactions`/`payments`, both untouched and outside this migration's scope). |
| `0588`–`0589` | Reserved (follow-ups discovered during build, same convention as every prior phase's tail). |

Forward-only, no `.down.sql` (repo-wide rule, Decision 25's amendment / FR-51.4) — **0586 and 0587 are the two migrations in this repo's history so far where "forward-only" is not just policy but a hard fact**: reversing 0586 would require reconstructing which `payment_tickets` rows were originally `card_payments` (recoverable from `method_key='scratch_card'`, so actually reversible in principle — noted for completeness, not attempted since no other migration in this repo has ever shipped a down leg either) and reversing 0587 would require restoring `payment_intents` rows this migration correctly judged to be worthless transient state.

## Frozen contracts

### C1. Provider catalog (FR-77.1)
```go
type PaymentProvider struct {
    ID                  string
    Name                string
    LogoPath            *string // local disk path under the data dir, NFR-7
    InstructionsTemplate string
    Enabled             bool
}
```
`GET /api/v1/payment-providers` (any authenticated manager, read-only — every manager needs the catalog to configure their own account against it), `POST /api/v1/payment-providers {name, instructions_template}` / `PUT /api/v1/payment-providers/{id}` (permission `payment_providers.manage`, admin-only, audited). Providers are **edited in place** (not append-only-versioned) — a provider's name/template is display metadata, not a money-affecting figure like FR-71's cost or FR-74's wholesale price, so there is no "what was the name at renewal time" question to preserve.

### C2. Per-manager receiving accounts (FR-77.2)
`GET /api/v1/managers/{id}/provider-accounts` (self or admin), `PUT /api/v1/managers/{id}/provider-accounts/{providerId} {account_details, instructions_override?}` (self or admin, upserts the `UNIQUE (manager_id, provider_id)` row, audited — shown to subscribers, not secret, but still a money-relevant fact worth a trail per the source brief). No encryption at rest (source brief is explicit these are deliberately subscriber-visible, unlike NAS secrets or card codes).

### C3. Per-manager method enablement (FR-77.3)
`GET /api/v1/managers/{id}/method-settings` (self or admin) → `{"items": [{"method_key": "...", "enabled": bool}]}` covering every catalog provider plus the two built-ins. `PUT /api/v1/managers/{id}/method-settings {method_key, enabled}` (self or admin, audited) upserts one row. A manager's method list is **not** pre-seeded with rows for every provider — an absent row for a `method_key` means `enabled=false` (the same "no row = off" default every other settings-shaped table in this codebase uses).

### C4. Resolved Pay-method list (FR-77.4 — the kickoff-blocker contract)
```go
// ResolvePayMethods returns exactly what a subscriber's owning manager has
// BOTH enabled AND (for a provider) configured an account for. No fallback
// to a global/admin account exists — kickoff blocker 1's resolution.
func ResolvePayMethods(ctx context.Context, db *pgxpool.Pool, subscriberID string) ([]PayMethod, error)

type PayMethod struct {
    Key         string  // provider id, or "scratch_card" / "voucher"
    Kind        string  // "provider" | "scratch_card" | "voucher"
    ProviderName        *string // provider methods only
    AccountDetails      *string // provider methods only — the owning manager's own account
    InstructionsText     *string // provider's template, overridden per-manager if set
}
```
Resolution: `subscribers.owner_manager_id`'s `manager_method_settings` rows where `enabled=true`; for `kind="provider"`, additionally requires a `manager_provider_accounts` row for `(owner_manager_id, provider_id)` — its absence silently excludes that provider from the list rather than erroring (a manager mid-setup should never break their subscribers' Pay screen, just show it incomplete). A subscriber with no `owner_manager_id` (should not occur given FR-27's model, but defensively) resolves to an empty list, never a fallback to any other manager's methods.

### C5. Unified submission (FR-78.2/78.3) — reuses FR-59.1's trial mechanism verbatim
```go
type submitTicketParams struct {
    SubscriberID string
    MethodKey    string // resolved via C4 first — server re-validates, never trusts the client's claim
    // Provider fields (kind="provider" only):
    Amount, TransferReference, TransferDate, Note string
    Attachments  []uploadedFile
    // Scratch-card fields (kind="scratch_card" only, replaces cardpay.go's submitCard params 1:1):
    CardType, CardCode string
}
func (m *Module) submitTicket(ctx context.Context, p submitTicketParams) (ticketSubmitResult, error)
```
Server-side re-validates `MethodKey` against C4's resolution for `SubscriberID` at submission time (never trusts a client-supplied method the subscriber's manager didn't actually enable — the same "server is authoritative" posture every other permission check in this codebase already takes). Trial grant: **identical code path to today's `cardpay.go:submitCard`** — one `renew()` call with `chargeBalance: false, enforceBalance: false, durationOverrideDays: &one, source: <method-specific>`, reused not reimplemented, so C7's "byte-identical trial timing" gate item has a mechanical reason to hold. Attachments (provider submissions only) are written to local disk under `<data-dir>/payment-attachments/<ticket-id>/` at submission time, inside the same transaction's post-commit step (file I/O never happens inside the DB transaction, mirroring how CoA restore and other post-commit side effects already work in `renew()`) — a file-write failure after a committed ticket does not roll back the ticket (NFR-7: a subscriber's provisional service must never depend on local disk I/O succeeding at the exact same instant), but is logged and surfaced as a ticket event `attachment_failed` for the reviewer to notice.

### C6. Trial-eligibility rule (FR-78.3 — supersedes FR-59.4's cooldown)
```go
// trialEligible reports whether SubscriberID's next submission should grant
// a trial day: true unless their most recent ticket (any method) is
// 'rejected' AND no ticket has been 'approved' since. Approval resets
// eligibility; rejection alone does not block resubmission, only the trial.
func trialEligible(ctx context.Context, tx pgx.Tx, subscriberID string) (bool, error)
```
Replaces FR-59.4's `cardCooldownDays`/`cardCooldownError` mechanism entirely — there is no more "blocked until `RetryAt`" state; a resubmission is always accepted immediately, `trialEligible` only gates whether `submitTicket` also calls `renew()` for the 1-day grant or skips straight to `pending` with no expiry change. `keyCardCooldownDays` setting is removed (dead: nothing reads it once this ships).

### C7. Approve / reject (FR-79.3/79.4) — reuses FR-59.2 verbatim, now wholesale-aware
`approveTicket`/`rejectTicket` are `cardpay.go`'s `approveCard`/`rejectCard` generalized to read `payment_tickets` instead of `card_payments` — **same anchoring-at-trial-start logic, same reversing-entry-nets-to-zero-on-reject logic, unchanged**. The one substantive addition: `approveTicket`'s `renew()` call no longer forces `chargeBalance: false` unconditionally — it now threads through v2-9's C5/C6 (`resolveWholesale`) exactly like a normal panel renewal, so a reseller-owned subscriber's approval debits the reseller's resolved wholesale price while the subscriber's own payment/receipt still shows retail (AC-79b). This is not a new resolution path — it is the *existing* FR-19.3 renewal transaction that `renew()` already runs, called with the same parameters a panel renewal uses, rather than the trial-only `chargeBalance:false` parameters FR-59.2's card approval used exclusively until now.

### C8. Ticket detail + timeline (FR-79.1)
`GET /api/v1/payment-tickets/{id}` → the ticket plus its ordered `payment_ticket_events` rows plus attachment metadata (never attachment bytes — those are C10's separate authenticated endpoint). Every state-changing operation (`submitTicket`, `approveTicket`, `rejectTicket`, attachment write) inserts exactly one `payment_ticket_events` row in the same transaction as the state change it records — the timeline can never drift from the state it describes because nothing writes state without also writing the event.

### C9. Queue + log scoping (FR-79.2)
`GET /api/v1/payment-tickets?scope=mine|all&state=&provider=&agent=&from=&to=` — `scope=mine` (default) applies `auth.ScopeFilter` exactly like every other scoped list (a scoped agent's `scope=all` request is silently downgraded to `scope=mine`, never a 403 — consistent with how the rest of the API degrades rather than errors on an over-broad request from a scoped caller); `scope=all` is available only to an unscoped (admin/global) caller and supports every filter the source brief's "searchable/filterable by agent, provider, state, date" calls for.

### C10. Attachment retrieval (FR-78.2)
`GET /api/v1/payment-tickets/{id}/attachments/{attachmentId}` — permission-checked exactly like the ticket itself (the caller must be able to see the ticket: scoped to their own, or unscoped), served with `Content-Disposition: attachment` and the stored `content_type`/`filename`, **never** `text/html` or any inline-renderable disposition regardless of what the uploader's browser claimed the file was (NFR-7 + basic upload-handling hygiene: an uploaded file is data, never executed, never inline-rendered even if it happens to be an HTML file someone renamed to `.jpg`).

### C11. Notification matrix (FR-80)
| Event | Subscriber notified? | Owning manager notified? | Other managers? |
|---|---|---|---|
| `submitted` | Yes — "pending, N-day trial active" (or "pending, no trial this time" per C6) | Yes — in-app + panel push, ticket landed in their queue | No (unless opted into all-tickets per v2-6 prefs) |
| `approved` | Yes — "renewed until X" | No (they know — they either approved it themselves, or see it in `rejected`/`approved` below if someone else did) | If decided_by ≠ owning manager: owning manager gets a "decided by someone else" notification |
| `rejected` | Yes — "rejected: <reason>, resubmitting now earns no free day" | Same "decided by someone else" rule as approved | — |

Both subscriber and manager channels reuse existing plumbing verbatim: subscriber side is FR-55's channel dispatch (already generalized past WhatsApp-only in v1.1) fed by the same `billing.card_payment`-shaped Redis publish `cardpay.go` already emits (renamed `billing.payment_ticket`, same consumer in `monitorsvc`, extended to also branch on `decided_by != owner_manager_id` for the manager-facing half); manager side is the existing panel web-push + in-app notification store (FR-54.4/FR-36.2's plumbing), never a new channel. Every row in the table above is implemented as "publish an event carrying enough data for the notification layer to read `payment_ticket_events`, never a parallel invented message."

### C12. Gateway removal (FR-23 retired, kickoff blocker 2)
Deleted, not quarantined: `internal/billing/gateways/` (the `PaymentGateway` interface, mock adapter, zaincash stub package), `internal/billing/paymentintents.go` and its handlers, the `payment_intents` table (migration 0587), the panel's gateway-config screen and API client functions, and `portalapi`'s `POST /portal/payments/{gateway}/create` route. `internal/billing/module.go`'s route registrations for these are removed, not commented out. A grep leg in the gate script asserts none of these symbols exist in the tree post-migration.

### C13. Portal Pay screen (FR-78.1/42.1, cross-owned with sub-PRD 07)
`GET /api/v1/portal/pay-methods` (subscriber-scoped, wraps C4's `ResolvePayMethods`) → the tile list. `POST /api/v1/portal/payment-tickets` (multipart form: JSON fields + file parts for attachments) → wraps C5's `submitTicket`. `GET /api/v1/portal/payment-tickets/latest` → the subscriber's own most recent ticket (any method) for the "pending — under review" banner (FR-42.3), generalizing `cardpay.go`'s `latestCardPayment`. The portal's one Pay screen replaces both the old gateway-list screen and the separate scratch-card screen — implementer's call on exact component structure, not frozen here beyond "one picker, all enabled methods as tiles" (source brief FR-78.1).

### C14. Panel screens (frozen scope, not frozen pixel-for-pixel)
- **Provider catalog** (admin, `payment_providers.manage`): CRUD list, mirrors `CurrencyRatesPage`'s rate-table pattern.
- **My accounts + methods** (every manager, `me/...` endpoints from C2/C3): a manager's own provider-account entry form + method-enable toggles — every manager needs this, not just admins, so it is *not* gated behind a `*.manage` permission the way C1/C2's admin-on-behalf-of-another-manager path is.
- **Payment tickets queue/log** (`payment_tickets.verify` permission, generalizing `card_payments.verify`): own-queue view (scoped) and, for unscoped callers, an all-tickets log with C9's filters; per-ticket detail modal with attachment viewer (images inline via a safe `<img>` tag pointed at C10's authenticated endpoint — PDFs open in a new tab, never inline-framed) and the C8 timeline rendered as a vertical event list.

## Integration gate

Green when all pass (scriptable legs in `scripts/gate-v2-phase-2.sh`; DB-gated legs require `HIKRAD_TEST_DB_URL`/`HIKRAD_TEST_REDIS_URL`, self-skip otherwise):

1. **Lossless `card_payments` → `payment_tickets` migration** — mirrors v2 phase 1's scratch-DB pattern: migrate to the last pre-this-phase migration, write `card_payments`-shaped v1/v2-shaped rows (pending, approved, and rejected states), migrate to head, assert every row survived in `payment_tickets` with `method_key='scratch_card'`, correct `method_detail`, identical state/decision fields, one synthesized `submitted` event per row, and `card_payments` gone.
2. **Trial granted on first attempt (AC-78a)** — a fresh subscriber's first submission (any method) grants a 1-day provisional renewal within the existing FR-59.1 timing tolerance.
3. **No trial on post-rejection retry, reset on approval (AC-78b)** — a rejected ticket's immediate resubmission is accepted (`pending`) but grants no expiry change; after that resubmission is approved, the *next* new ticket grants a trial day again.
4. **Owner-scoping + admin-sees-all (AC-79a)** — a scoped agent's queue/log shows only their own subscribers' tickets; an unscoped admin's `scope=all` shows every ticket across every agent.
5. **Wholesale-aware approval (AC-79b)** — approving a reseller-priced subscriber's ticket debits the reseller's resolved wholesale price (v2-9 FR-76) while the subscriber's payment/receipt shows retail — identical split to a normal FR-19 renewal, not a special case for this payment method.
6. **Both-sides notifications (AC-80a)** — submit/approve/reject each produce a `payment_ticket_events` row and a corresponding subscriber notification; submit and "decided by someone else" each produce an owning-manager notification; every notification's content is asserted to trace to a real event row, never a value invented outside it.
7. **Attachment authorization** — a ticket's attachment is fetchable by the ticket's own scoped manager and by an unscoped admin; a *different* scoped manager (not the owner) gets 403/404; the response always carries `Content-Disposition: attachment`.
8. **No-account fallback never leaks a method (AC-77a)** — a manager who enabled a provider but has no `manager_provider_accounts` row for it never has that provider appear in any subscriber's `GET /portal/pay-methods` response — response-shape inspection, not just UI hiding, mirroring v2-9's AC-75a discipline.
9. **Gateway surface fully removed (C12)** — a grep leg asserts `PaymentGateway`, `payment_intents`, and the gateway config panel screen/API functions no longer exist anywhere in the tree.
10. **Build + full regression** — `go build`/`go vet` clean; the full pre-existing `internal/billing`/`internal/reports` DB-gated suites (including v2-4's and v2-9's own gate tests) pass unchanged.
11. **Panel/portal** — build + lint + vitest green; `i18n:check` green covering the provider catalog, my-accounts/methods, the tickets queue/log + detail/timeline, and the portal's unified Pay screen.
12. **Docs accuracy** — PRD/sub-PRDs 05/07 reflect FR-77–80 and FR-23's retirement (already done in this brief's own Step-1 commit, before this file); `docs/ops/known-issues.md` carries any bug found while building.

Human/hardware legs: none — same as v2-4/v2-9, no router/device dependency. The closest thing to a documented-pending item is real-device file-upload behavior (camera capture on a subscriber's phone for the receipt-screenshot attachment) — worth a manual pass on real hardware during the pilot, not scriptable here.

## Open implementation questions for whoever builds this (not blocking, but worth a decision-log entry when resolved)

- **Attachment size/type limits** (source brief says "size/type limits" without a number) — recommend images (jpeg/png/webp) + PDF, capped at ~10MB per file, a handful of files per ticket; not frozen here, pick sane defaults and document them in the phase's own implementation notes.
- **`payment_providers.manage` vs. reusing an existing permission** — a new permission, not `settings.edit`, since provider catalog changes are audited money-adjacent data distinct from general settings; mirrors `currency_rates.manage`/`overheads.manage`'s precedent of one new permission per new admin-only money surface rather than overloading an existing one.
- **Whether `manager_method_settings` needs a "reordering" concept** (tile display order in the portal) — the source brief doesn't ask for it; default to catalog order (providers) then built-ins, revisit only if the owner asks after using it.
