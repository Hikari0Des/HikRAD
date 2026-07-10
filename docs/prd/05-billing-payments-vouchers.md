# HikRAD — Sub-PRD 05: Billing, Payments & Vouchers

> Derived from [docs/PRD.md](../PRD.md) v1.0 on 2026-07-08; updated 2026-07-11 for master v1.3 (FR-59 scratch-card payments added — Decision 22). Owns: FR-19, FR-20, FR-21, FR-22, FR-23, FR-24, FR-25, FR-26, FR-59 · Risk: e-wallet gateway availability · Open question 1 (gateway priority)
> Depends on: [04-subscribers-profiles](04-subscribers-profiles.md) (profile price/duration, expiry updates, per-user overrides), [02-radius-nas-aaa](02-radius-nas-aaa.md) (CoA after renewal, Hotspot voucher login), [06-managers-roles-security](06-managers-roles-security.md) (manager accounts, permissions, audit) · Depended on by: [07-subscriber-portal-pwa](07-subscriber-portal-pwa.md) (portal renewal + payment UI), [08-reports](08-reports.md) (financial reports read the ledger), [03-lossless-accounting-live-monitoring](03-lossless-accounting-live-monitoring.md) (revenue tile, low-balance alerts)

## 1. Scope & context

All money movement: prepaid renewals, the immutable transaction ledger, manager/agent balances (how **Hassan** the field agent operates and settles), manual cash payments with printable receipts, one-time voucher batches, and Iraqi e-wallet payments (ZainCash, FastPay, Qi) behind a pluggable gateway interface. Business model is prepaid only (Decision 7); currency is IQD by default with formatting from settings. Payments must never be a launch blocker: manual + voucher paths work fully offline (NFR-7 — e-wallets are the *only* online-dependent feature).

## 2. Owned requirements — elaborated

### FR-19 (M) — Prepaid renewal
**Master:** Renewal charges the profile price, extends expiry by profile duration (from expiry date if still active, from now if already expired — configurable).

*Elaboration:*
- **FR-19.1** — Renewal transaction (atomic): validate actor permission + balance → ledger debit (actor's manager balance, FR-20) + payment record → extend `expires_at` per the anchor rule (settings default, [01](01-platform-install-licensing.md) FR-53.2) → reset quota counters per profile cycle ([04](04-subscribers-profiles.md) FR-8) → set status active → `InvalidatePolicy` → if online in expired pool, CoA restore ([02](02-radius-nas-aaa.md) FR-15.2; key flow 2 step 4).
- **FR-19.2** — Price = subscriber override if set ([04](04-subscribers-profiles.md) FR-7) else profile price; promo price if active (FR-26). One-click repeat-last-profile plus profile-switch option in the same dialog (Sara's user story; profile switch consumes `pending_profile_id` if FR-12 ships).
- **FR-19.3** — Renewal sources all converge on this one code path: operator panel, agent panel, voucher redemption (FR-22), portal e-wallet (FR-23). Each source is recorded on the ledger entry.

### FR-20 (M) — Manager balances
**Master:** Each manager/agent account holds a balance; renewals they perform deduct from it; admins top up agent balances; every movement is a ledger transaction.

*Elaboration:*
- **FR-20.1** — Balance is **derived from the ledger** (sum of that manager's entries), cached for display — never a directly-edited field. Insufficient balance blocks renewal with a clear message.
- **FR-20.2** — Top-up: admin action (permission `top-up`), creates a credit entry with note; negative adjustments are explicit correction entries (FR-24), never edits.
- **FR-20.3** — Admin-role accounts may operate without balance limits (configurable); agent accounts always enforce. Low-balance threshold per agent feeds the alert engine ([03](03-lossless-accounting-live-monitoring.md) FR-36.3).
- **FR-20.4** — Hassan's settlement: his collection report ([08](08-reports.md) FR-45) equals his ledger slice; end-of-week settlement is reading that report — no extra bookkeeping (his user story).

### FR-21 (M) — Manual payments & receipts
**Master:** Record cash payments with receipt number, printable receipt (Arabic/English templates).

*Elaboration:* receipt number = per-install sequential (prefix configurable); printable receipt (A5/thermal-friendly print CSS) with ISP branding, Arabic and English templates selected per settings/locale (Kurdish reuses Arabic RTL layout — strings via [07](07-subscriber-portal-pwa.md) NFR-6); "sendable" (key flow 2 step 5) = shareable link/downloadable PDF-quality print view.

### FR-22 (M) — One-time vouchers
**Master:** Generate batches of codes (profile, count, prefix, expiry-of-code); redeemable by operators, via subscriber portal, or at Hotspot login; single-use; batch list shows used/unused; export CSV.

*Elaboration:*
- **FR-22.1** — Batch = profile + count (≤ 10,000) + optional prefix + code expiry date. Codes: crypto-random, unambiguous alphabet (no 0/O, 1/l), length ≥ 10 incl. prefix; stored hashed-comparable, exportable as CSV plaintext at generation.
- **FR-22.2** — Redemption is atomic single-use (row-locked): valid+unused+unexpired → runs FR-19 renewal for the redeeming subscriber (or authenticates the Hotspot guest per [02](02-radius-nas-aaa.md) FR-18) → marks used (when/by whom/for whom). Double-redeem race must be impossible.
- **FR-22.3** — Redemption paths: operator on the user page; subscriber portal ([07](07-subscriber-portal-pwa.md) FR-42); Hotspot login ([02](02-radius-nas-aaa.md) FR-18.1). Voucher-funded renewals hit the **batch creator's** ledger context (charged when generated or when used — one model chosen in P3 and documented; see NEW question).
- **FR-22.4** — Batch list: used/unused/expired counts, drill-down per code, void-batch action (unused codes only, audit-logged). *(Printable card designer is Phase 2 — out of scope.)*

### FR-23 (M) — Iraqi e-wallet payments, pluggable gateways
**Master:** From the subscriber portal via a pluggable gateway interface; v1 ships adapters for ZainCash, FastPay, and Qi (subset shippable per gateway merchant-account availability — see Open Questions), with webhook/callback verification and automatic renewal on confirmed payment.

*Elaboration:*
- **FR-23.1** — `PaymentGateway` interface (master §8): `CreatePayment(amount, subscriber, profile) → redirect/reference`, `VerifyCallback(request) → confirmed/failed`, `QueryStatus(reference)`. Adapters are isolated packages; enabling/configuring each (merchant creds) is a settings concern per gateway.
- **FR-23.2** — Payment intent lifecycle: `pending → confirmed → renewed` (or `failed`/`expired`); callbacks are signature-verified per gateway spec and idempotent (replayed callbacks don't double-renew); `QueryStatus` polling reconciles intents stuck pending (subscriber paid but callback lost).
- **FR-23.3** — Confirmed payment triggers FR-19 automatically with source `portal-{gateway}`; gateway fees, if surfaced, are recorded on the ledger entry metadata.
- **FR-23.4** — Graceful degradation (NFR-7): gateway down/unreachable shows a clear portal message and leaves voucher redemption available; core system is unaffected.
- **FR-23.5** — Ship-what's-available: each adapter ships independently behind the interface; v1 is sellable with zero live gateways (manual + voucher paths).

### FR-24 (M) — Full transaction ledger
**Master:** Immutable, filterable by manager/user/date/type, exportable; discounts and manual adjustments are explicit ledger entries, never edits.

*Elaboration:* append-only table (no UPDATE/DELETE grants for the app role); entry = id, timestamp, type (renewal, top-up, manual-payment, voucher-redeem, refund, adjustment, discount), amount signed IQD, actor manager, subscriber (nullable), source, reference (receipt no / gateway ref / voucher id), note. Corrections are reversing entries linking the original. Ledger drives balances (FR-20.1), the revenue tile ([03](03-lossless-accounting-live-monitoring.md) FR-32), and financial reports ([08](08-reports.md) FR-45). Export CSV under the `export` permission.

### FR-25 (S) — Refund / cancel-renewal
**Master:** Flow with reason, reversing ledger entries and expiry.

*Elaboration:* permission-gated; reverses the renewal's ledger entry (credit back to the acting manager's balance), rolls `expires_at` back by the granted duration (floor: now), restores prior quota state where determinable, requires a reason, audit-logged; if the user is online and now expired, applies FR-9 behavior via CoA.

### FR-59 (S) — Telecom scratch-card payment (manual verification + trial window)
**Master (v1.3):** A subscriber submits a Zain/Asiacell airtime-card code from the portal and immediately receives a 1-day provisional renewal; the payment sits pending in an admin verification queue until an admin redeems the card value and approves (full renewal) or rejects (reversal + deactivation), with the subscriber notified at each state change. No carrier API — fully offline-capable.

*Elaboration:*
- **FR-59.1** — Submission (portal, [07](07-subscriber-portal-pwa.md) UI): card type (from a settings-configurable list: zain, asiacell, …) + card code → creates a `card_payments` row (state `pending`) **and** runs a provisional FR-19 renewal for **1 day** (source `card-trial`, ledger entry flagged provisional) so the subscriber gets test internet immediately — including CoA restore if online in the expired pool.
- **FR-59.2** — Verification queue (panel): admins with permission `card_payments.verify` see pending items (subscriber, profile, requested plan price, card type, submission time) and can reveal the card code (audit-logged reveal) to load it into the ISP's own telecom account manually. **Approve** → full FR-19 renewal for the target profile anchored at the trial's start (the trial day is part of, not added to, the paid duration), source `card-<type>`, pending → `approved`. **Reject** (reason required) → reversing ledger entry for the trial, expiry rolled back (floor: now → subscriber lands expired/deactivated, FR-9 behavior applied via CoA), pending → `rejected`.
- **FR-59.3** — Notifications: state changes (pending confirmation on submit, approved, rejected-with-reason) delivered via portal in-app/push and the FR-55 WhatsApp receipt infrastructure ([03](03-lossless-accounting-live-monitoring.md)); rejected message must tell the subscriber what to do next (contact the ISP / try a voucher).
- **FR-59.4** — Abuse guards: max one `pending` card payment per subscriber; after a rejection, new card submissions are blocked for a configurable cooldown (default 7 days, settings); trial is granted at most once per pending payment; card codes AES-encrypted at rest (NFR-4 crypto service), never in logs or list payloads — reveal is an explicit audited action.
- **FR-59.5** — Offline posture (NFR-7): the whole flow needs no internet; it is the *manual* counterpart to FR-23 and follows FR-19.3 — both trial and final renewal go through the single renewal path with distinct sources.

### FR-26 (C) — Promo pricing
**Master:** Temporary profile price override with start/end dates.

*Elaboration (Could):* per-profile promo price + date window; FR-19.2 price resolution order becomes user-override → active promo → profile price; ledger entries flag promo-priced renewals for reporting.

## 3. Acceptance criteria

- **AC-19a** — Given an active user expiring in 3 days on a 30-day profile with anchor "from expiry", when renewed, then new expiry = old expiry + 30 days; given an expired user, new expiry = now + 30 days.
- **AC-19b** — Given a user online in the expired pool, when renewed, then within 5 s their session runs at full profile speed with no redial (verified with [02](02-radius-nas-aaa.md)).
- **AC-20a** — Given agent Hassan with balance 25,000 IQD and a 25,000 IQD renewal, when he renews, then his balance is 0 and a second renewal is blocked with an insufficient-balance message.
- **AC-20b** — Given any point in time, then every manager's displayed balance equals the sum of their ledger entries exactly.
- **AC-22a** — Given one voucher code submitted simultaneously from the portal and by an operator, then exactly one redemption succeeds and the other gets "already used".
- **AC-23a** — Given a confirmed ZainCash callback replayed 3 times, then exactly one renewal and one ledger entry exist.
- **AC-23b** — Given a payment where the callback never arrives, when the reconciliation poll finds it confirmed, then the renewal completes automatically.
- **AC-24a** — Given the app's DB role, when an UPDATE on a ledger row is attempted, then the database refuses it.
- **AC-25a** — Given a refunded renewal, then the ledger shows original + reversing entry (net 0), balance is restored, and expiry rolled back — with the original entry untouched.
- **AC-59a** — Given an expired subscriber submitting a card code at midnight, then within 5 s they have a 1-day provisional renewal (online-in-expired-pool case restored via CoA) and a pending item exists in the verification queue; a second submission while pending is rejected with a clear message.
- **AC-59b** — Given an admin approving a pending card payment on day 0 for a 30-day profile, then the subscriber's expiry = trial start + 30 days (not +31); given a rejection, then the ledger nets to 0 for the trial, the subscriber is expired again, and a rejected notification (with reason) is delivered.
- **AC-59c** — Given the card-payments list API, then card codes never appear in it; the reveal action returns the code once and writes an audit entry naming the revealing manager.

## 4. Data & interfaces

**Owned entities:** `ledger_transactions` (append-only, fields per FR-24), `payments` (intent lifecycle, gateway ref, receipt no), `vouchers` + `voucher_batches`, `gateway_configs` (per-gateway creds, encrypted at rest per NFR-4), `card_payments` (FR-59: subscriber, profile, card type, card_code_enc, state `pending|approved|rejected`, trial ledger ref, decided_by/at, reject_reason), promo fields on pricing.

**Exposes:**
- `POST /api/v1/subscribers/{id}/renew` (the single renewal path, FR-19.3), `POST /api/v1/subscribers/{id}/refund`
- `GET/POST /api/v1/vouchers/batches`, `POST /api/v1/vouchers/redeem` (also consumed by Hotspot login flow)
- `POST /api/v1/managers/{id}/topup`, `GET /api/v1/managers/{id}/balance`
- `GET /api/v1/ledger?filters…` (+ CSV export)
- `POST /api/v1/payments/{gateway}/create`, `POST /api/v1/payments/{gateway}/callback` (public webhook endpoint, signature-verified), reconciliation worker.
- FR-59: `POST /api/v1/portal/card-payments {card_type, code}` (portal-scoped), `GET /api/v1/card-payments?state=` (queue), `POST /api/v1/card-payments/{id}/reveal`, `POST /api/v1/card-payments/{id}/approve`, `POST /api/v1/card-payments/{id}/reject {reason}`.
- Revenue aggregates for dashboard/reports ([03](03-lossless-accounting-live-monitoring.md), [08](08-reports.md)).
- Renewal/payment events (subscriber, amount, receipt no, new expiry) published for [03](03-lossless-accounting-live-monitoring.md) FR-55 WhatsApp receipt delivery — money logic here, message delivery there.

**Consumes:** profile price/duration + expiry mutation + policy invalidation from [04](04-subscribers-profiles.md); CoA from [02](02-radius-nas-aaa.md); permissions (`renew`, `top-up`, `export`) + audit from [06](06-managers-roles-security.md); billing-default settings and branding (receipts) from [01](01-platform-install-licensing.md).

## 5. UX notes

Renew dialog: opens pre-filled with current profile + resolved price (key flow 2 step 3), one confirm, receipt action on success — the whole flow inside the ≤ 3-click budget (NFR-5). Balances visible in the agent's own header (Hassan checks before field visits, phone-first). Ledger view: dense table, filter chips, running balance column when filtered to one manager. Receipt print view: RTL Arabic template with Eastern-Arabic numeral option per locale settings. Currency always formatted per settings — no hardcoded "IQD".

## 6. Out of scope

- Portal payment/voucher **UI** → [07](07-subscriber-portal-pwa.md) FR-42 (this module supplies the APIs).
- Report layouts over ledger data → [08](08-reports.md).
- Manager account CRUD/permissions themselves → [06](06-managers-roles-security.md).
- **Deferred by master:** prepaid card system with drag-and-drop designer (Phase 2 — vouchers here are code batches only); reseller tree with balance transfer down the chain (Phase 2 — balances here are flat); postpaid invoicing (non-goal).

## 7. Risks & open questions (owned)

- **Risk (master): E-wallet gateways (ZainCash/FastPay/Qi) require merchant accounts and have weak sandbox/docs.** Likelihood High / Impact Medium. Mitigation: pluggable interface (FR-23.1); v1 ships any subset (FR-23.5); manual + voucher paths make payments never a launch blocker. *Elaboration:* build the interface + a mock gateway adapter in P5 regardless, so adapter work is pure integration once credentials exist.
- **Open question 1 (master): Gateway priority** — which of ZainCash/FastPay/Qi is built first depends on which merchant account HikRAD or the pilot ISP can obtain; decide when the pilot ISP is selected (target: during P4).
- **NEW:** voucher accounting model — charge the batch creator's balance at generation vs. at redemption changes agent settlement semantics; decide in P3 with a real ISP workflow in hand.
- **NEW:** public webhook endpoint (FR-23.2) is the one internet-exposed surface; confirm rate-limiting and signature schemes per gateway during adapter development (OWASP ASVS L2 applies, NFR-4 → [06](06-managers-roles-security.md)).
