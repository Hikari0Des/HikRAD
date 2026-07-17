# HikRAD — Sub-PRD 05: Billing, Payments & Vouchers

> Derived from [docs/PRD.md](../PRD.md) v1.0 on 2026-07-08; updated 2026-07-11 for master v1.3 (FR-59 scratch-card payments added — Decision 22); updated 2026-07-17 for master v1.6 (FR-68–70 full multi-currency billing — Decision 34, v2 phase 4; supersedes the implicit-IQD assumption stated in §1 below). Owns: FR-19, FR-20, FR-21, FR-22, FR-23, FR-24, FR-25, FR-26, FR-59, FR-68, FR-69, FR-70 · Risk: e-wallet gateway availability · Open question 1 (gateway priority)
> Depends on: [04-subscribers-profiles](04-subscribers-profiles.md) (profile price/duration, expiry updates, per-user overrides), [02-radius-nas-aaa](02-radius-nas-aaa.md) (CoA after renewal, Hotspot voucher login), [06-managers-roles-security](06-managers-roles-security.md) (manager accounts, permissions, audit) · Depended on by: [07-subscriber-portal-pwa](07-subscriber-portal-pwa.md) (portal renewal + payment UI), [08-reports](08-reports.md) (financial reports read the ledger), [03-lossless-accounting-live-monitoring](03-lossless-accounting-live-monitoring.md) (revenue tile, low-balance alerts)

## 1. Scope & context

All money movement: prepaid renewals, the immutable transaction ledger, manager/agent balances (how **Hassan** the field agent operates and settles), manual cash payments with printable receipts, one-time voucher batches, and Iraqi e-wallet payments (ZainCash, FastPay, Qi) behind a pluggable gateway interface. Business model is prepaid only (Decision 7). Payments must never be a launch blocker: manual + voucher paths work fully offline (NFR-7 — e-wallets are the *only* online-dependent feature).

**v2 (FR-68–70, Decision 34) supersedes this scope note's "currency is IQD by default":** every monetary column is now an integer minor-unit amount **plus a currency code on the same row** — `IQD`/`USD`/`EUR` shipped enabled, extensible catalog. Nothing here becomes online-dependent: exchange rates are admin-entered (`currency_rates`), never fetched, so the NFR-7 offline posture is unaffected.

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

### FR-68 (M) — Currency model (v2)
**Master:** A `currencies` catalog (`IQD`/`USD`/`EUR` shipped enabled, extensible) replaces the implicit-IQD assumption. Every monetary column becomes an integer minor-unit amount + a `currency` code. Exchange rates are admin-maintained only — no online feed. Every rate actually used is stamped onto the ledger row.

*Elaboration:*
- **FR-68.1** — `currencies(code PK, minor_unit_digits, symbol, enabled)`. `minor_unit_digits` is 0 for IQD (its stored integer already *is* the whole-currency amount, so the FR-24 rename below changes no IQD value), 2 for USD/EUR. Adding a fourth currency later is a data-only change (a new `currencies` row + enabling it in Settings), never a schema change — the FR-17-style "no core changes to add one more" principle applied to money instead of NAS vendors.
- **FR-68.2** — Every existing `*_iqd` column (`ledger_transactions.amount_iqd`, `manager_balances.balance_iqd`, `manager_low_balance_thresholds.threshold_iqd`, `profiles.price_iqd`, `voucher_batches.unit_price_iqd`, `payments.amount_iqd`) is renamed to drop the `_iqd` suffix and gains a sibling `currency` column (`NOT NULL REFERENCES currencies(code)`, backfilled `'IQD'`). The rename is deliberate, not cosmetic: a column still spelled `amount_iqd` holding USD cents would be a standing lie in the schema every future reader has to know to distrust.
- **FR-68.3** — `currency_rates(id, from_currency, to_currency, rate, effective_from, created_by, created_at)`. `rate` is expressed in **whole-currency** terms (e.g. `1 USD = 1310 IQD` stores `rate=1310`, `from_currency='USD'`, `to_currency='IQD'`) so the admin-facing rate-entry form is human-auditable; any code converting minor-unit amounts applies each side's `minor_unit_digits` around that whole-currency rate. No code path in this feature ever makes an outbound HTTP call to fetch a rate (NFR-7) — the only way a `currency_rates` row is created is the admin CRUD endpoint (FR-68.4).
- **FR-68.4** — Rate CRUD is permission-gated (new permission `currency_rates.manage`) and audit-logged like any other settings mutation; rates are append-style (a new `effective_from` row, not an edit of an old one) so a historical ledger entry's stamped rate is never retroactively altered — consistent with the ledger's own append-only philosophy.

### FR-69 (M) — Money flows in the transaction's own currency (v2)
**Master:** Renewals, balances, vouchers, and refunds all thread the currency of the money actually moving, end to end. Manager balances become per-currency with no implicit conversion; converting currency is an explicit, ledger-visible exchange.

*Elaboration:*
- **FR-69.1** — Profile price gains a currency (FR-8 elaboration, owned by [04](04-subscribers-profiles.md) for the column, enforced here for the renewal math). FR-19.3's single renewal code path is unchanged in shape — it now also threads `currency` alongside `price` from resolution through to the ledger entry, payment/receipt row, and CoA restore is unaffected (currency has no RADIUS meaning).
- **FR-69.2** — `manager_balances` becomes a per-`(manager_id, currency)` row (composite PK), each independently derived-from-the-ledger (FR-20.1 applies **per currency**: `balance(M, C) = sum(ledger.amount WHERE actor_manager_id=M AND currency=C)`, never summed across `C`). A renewal in a currency the manager has never held creates that currency's zero-balance row on first touch, identical in spirit to today's `manager_balances` upsert-on-first-use. Insufficient-balance errors name the currency that was short.
- **FR-69.3** — **Exchange** (new): a manager (or admin on their behalf) converts between two of their own currency balances using a `currency_rates` row. This inserts exactly **two** linked `ledger_transactions` rows in one transaction — a debit in `from_currency`, a credit in `to_currency` — both carrying a shared reference and the `currency_rates.id` used (FR-68.1's "every rate actually used is stamped" requirement). This is the **only** way value moves between a manager's currencies; nothing else (a renewal, a top-up, a refund) is ever allowed to implicitly convert. New ledger `type` value `'exchange'` (widens FR-24's existing CHECK).
- **FR-69.4** — Vouchers (FR-22) keep the charge-at-generation model exactly as Phase 3 built it (the "NEW" open question in §7 was already resolved there) — this FR only adds that a batch's `unit_price`/`currency` are the generating profile's, unchanged mechanism.
- **FR-69.5** — Refunds (FR-25) reverse in the **original entry's own currency, at no rate at all** (a refund is not a re-conversion — it is undoing the original movement exactly) — `reverses_id` already links the pair, so the reversing entry's currency is read from the entry it reverses, never re-resolved from today's profile price or today's rate table.

### FR-70 (S) — Multi-currency display & reports (v2)
**Master:** A default display currency in Settings; the shared amount formatter generalizes from IQD-only; reports/reconciliation run per currency, never silently summed across one.

*Elaboration:*
- **FR-70.1** — `formatIQD(amount, locale, opts)` generalizes to `formatMoney(amount, currency, locale, opts)`; `formatIQD` becomes a thin wrapper (`formatMoney(amount, 'IQD', locale, opts)`) so existing call sites keep compiling, but every call site that renders a genuinely multi-currency field (profile price, ledger row, manager balance, receipt, voucher batch) must migrate to pass the row's real `currency`, not assume IQD. IQD's rendered output is byte-identical to today's (regression-locked by the existing format test suite), including the Eastern-Arabic-numeral option ([07](07-subscriber-portal-pwa.md) NFR-6.3).
- **FR-70.2** — Reports ([08](08-reports.md)) and the M-series reconciliation invariants (FR-40-style "received/persisted" but for money: ledger sum = displayed balance) are computed **per currency** — a report spanning multiple currencies shows one column set per currency, never a single blended total. An optional "converted to display currency" view is additive and always labeled with the rate and its `effective_from` date, so it is legibly an estimate, not the authoritative figure.
- **FR-70.3** — Settings gains a default display currency (used only for FR-70.1's fallback when a screen has no more specific currency context, e.g. a dashboard tile aggregating across managers — which itself must respect FR-70.2's per-currency rule rather than silently pick one).

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
- **AC-68a** *(v2)* — Given a fresh install or an upgraded one, then `currencies` contains exactly `IQD`/`USD`/`EUR`, all enabled; every pre-migration `*_iqd` row backfills to `currency='IQD'` with its stored integer unchanged (IQD's `minor_unit_digits=0` means the rename changes no value).
- **AC-68b** *(v2)* — Given the `currency_rates` CRUD endpoint, then no code path in this feature ever issues an outbound HTTP request — rates only ever come from an authenticated admin submission, audit-logged.
- **AC-69a** *(v2)* — Given a USD-priced profile and an agent with a USD balance of $50 and an IQD balance of 25,000, when the agent renews a $25 USD subscriber, then the USD balance is $25, the IQD balance is untouched at 25,000, and the receipt shows USD.
- **AC-69b** *(v2)* — Given a manager exchanging $100 to IQD at a stored rate of 1310, then exactly two new ledger rows exist (a $100 USD debit, a 131,000 IQD credit), both referencing the same `currency_rates` row; the manager's USD balance drops by exactly $100 and IQD balance rises by exactly 131,000; no other manager's balance in either currency changes.
- **AC-69c** *(v2)* — Given any point in time, then every manager's displayed balance **in each currency they hold** equals the sum of their ledger entries **in that currency** exactly (AC-20b generalized — the invariant must hold per currency, and a bug that summed across currencies would fail this on the very first mixed-currency manager).
- **AC-69d** *(v2)* — Given a refunded USD renewal, then the reversing entry is in USD at the original entry's amount — never re-priced at today's profile price or re-converted at today's rate.
- **AC-70a** *(v2)* — Given `formatMoney` called with `currency='IQD'`, then its output is byte-identical to today's `formatIQD` for the same amount/locale/options (regression-locked).
- **AC-70b** *(v2)* — Given a report spanning subscribers billed in both IQD and USD, then the report shows separate per-currency figures; no total anywhere silently adds an IQD number to a USD number.

## 4. Data & interfaces

**Owned entities:** `ledger_transactions` (append-only, fields per FR-24; **v2:** `amount` + `currency` replace `amount_iqd`, plus a nullable `currency_rate_id` stamped only on `type='exchange'` rows), `payments` (intent lifecycle, gateway ref, receipt no; **v2:** `amount`/`currency` replace `amount_iqd`), `vouchers` + `voucher_batches` (**v2:** `unit_price`/`currency` replace `unit_price_iqd`), `gateway_configs` (per-gateway creds, encrypted at rest per NFR-4), `card_payments` (FR-59: subscriber, profile, card type, card_code_enc, state `pending|approved|rejected`, trial ledger ref, decided_by/at, reject_reason), promo fields on pricing. **v2 additions (FR-68):** `currencies` (code, minor_unit_digits, symbol, enabled), `currency_rates` (from_currency, to_currency, rate, effective_from, created_by). `manager_balances` and `manager_low_balance_thresholds` (owned data, FR-20) become per-`(manager_id, currency)` composite-keyed rows.

**Exposes:**
- `POST /api/v1/subscribers/{id}/renew` (the single renewal path, FR-19.3), `POST /api/v1/subscribers/{id}/refund`
- `GET/POST /api/v1/vouchers/batches`, `POST /api/v1/vouchers/redeem` (also consumed by Hotspot login flow)
- `POST /api/v1/managers/{id}/topup`, `GET /api/v1/managers/{id}/balance` (**v2:** returns one balance per currency the manager holds, not a single integer)
- `POST /api/v1/managers/{id}/exchange {from_currency, to_currency, amount, currency_rate_id}` (**v2, FR-69.3** — the only currency-conversion path)
- `GET/POST /api/v1/currency-rates` (**v2, FR-68.4**, `currency_rates.manage`-gated), `GET /api/v1/currencies`
- `GET /api/v1/ledger?filters…` (+ CSV export; **v2:** filterable/groupable by `currency`)
- `POST /api/v1/payments/{gateway}/create`, `POST /api/v1/payments/{gateway}/callback` (public webhook endpoint, signature-verified), reconciliation worker.
- FR-59: `POST /api/v1/portal/card-payments {card_type, code}` (portal-scoped), `GET /api/v1/card-payments?state=` (queue), `POST /api/v1/card-payments/{id}/reveal`, `POST /api/v1/card-payments/{id}/approve`, `POST /api/v1/card-payments/{id}/reject {reason}`.
- Revenue aggregates for dashboard/reports ([03](03-lossless-accounting-live-monitoring.md), [08](08-reports.md)) — **v2:** aggregated per currency (FR-70.2).
- Renewal/payment events (subscriber, amount, currency, receipt no, new expiry) published for [03](03-lossless-accounting-live-monitoring.md) FR-55 WhatsApp receipt delivery — money logic here, message delivery there.

**Consumes:** profile price/duration/currency + expiry mutation + policy invalidation from [04](04-subscribers-profiles.md); CoA from [02](02-radius-nas-aaa.md); permissions (`renew`, `top-up`, `export`, **`currency_rates.manage`**) + audit from [06](06-managers-roles-security.md); billing-default settings, default display currency, and branding (receipts) from [01](01-platform-install-licensing.md).

Full request/response shapes, the migration sequence, and the exchange-pair ledger contract are frozen in [docs/v2/phases/phase-v2-4-multi-currency/00-phase.md](../v2/phases/phase-v2-4-multi-currency/00-phase.md).

## 5. UX notes

Renew dialog: opens pre-filled with current profile + resolved price (key flow 2 step 3), one confirm, receipt action on success — the whole flow inside the ≤ 3-click budget (NFR-5). Balances visible in the agent's own header (Hassan checks before field visits, phone-first). Ledger view: dense table, filter chips, running balance column when filtered to one manager. Receipt print view: RTL Arabic template with Eastern-Arabic numeral option per locale settings. Currency always formatted per settings — no hardcoded "IQD".

## 6. Out of scope

- Portal payment/voucher **UI** → [07](07-subscriber-portal-pwa.md) FR-42 (this module supplies the APIs).
- Report layouts over ledger data → [08](08-reports.md).
- Manager account CRUD/permissions themselves → [06](06-managers-roles-security.md).
- **Deferred by master:** prepaid card system with drag-and-drop designer (Phase 2 — vouchers here are code batches only); reseller tree with balance transfer down the chain (Phase 2 — balances here are flat); postpaid invoicing (non-goal).
- **Cost, margin and reseller (wholesale) pricing → v2-9**, briefed at [docs/v2/09-cost-margin-and-reseller-pricing.md](../v2/09-cost-margin-and-reseller-pricing.md) (owner request 2026-07-16). v1 deliberately has **no concept of cost**: `profiles.price_iqd` is what the subscriber is charged, and a renewal debits the owning manager's balance **that same amount** — so every reseller pays retail and margin is unrepresentable. That is a real limitation, not an oversight to patch here: adding a cost price and a wholesale price changes what a renewal ledger row *means* (one debit at one price becomes a retail charge + a wholesale debit + a cost stamp), which is the contract Phase 3 froze and [08](08-reports.md) reconciles against. It lands with v2-4's multi-currency rework so the ledger is migrated once.

## 7. Risks & open questions (owned)

- **Risk (master): E-wallet gateways (ZainCash/FastPay/Qi) require merchant accounts and have weak sandbox/docs.** Likelihood High / Impact Medium. Mitigation: pluggable interface (FR-23.1); v1 ships any subset (FR-23.5); manual + voucher paths make payments never a launch blocker. *Elaboration:* build the interface + a mock gateway adapter in P5 regardless, so adapter work is pure integration once credentials exist.
- **Open question 1 (master): Gateway priority** — which of ZainCash/FastPay/Qi is built first depends on which merchant account HikRAD or the pilot ISP can obtain; decide when the pilot ISP is selected (target: during P4).
- **NEW:** voucher accounting model — charge the batch creator's balance at generation vs. at redemption changes agent settlement semantics; decide in P3 with a real ISP workflow in hand.
- **NEW:** public webhook endpoint (FR-23.2) is the one internet-exposed surface; confirm rate-limiting and signature schemes per gateway during adapter development (OWASP ASVS L2 applies, NFR-4 → [06](06-managers-roles-security.md)).
