# HikRAD — Sub-PRD 04: Subscribers & Profiles

> Derived from [docs/PRD.md](../PRD.md) v1.1 on 2026-07-08 (updated 2026-07-09: FR-58 added — Decision 19; WhatsApp opt-in field for [03](03-lossless-accounting-live-monitoring.md) FR-55 — Decision 16; updated 2026-07-16 for master v1.4: FR-61 subscriber `service_type` + FR-63 panel/UX added — Decision 28, v2 phase 1). Owns: FR-1, FR-2, FR-3, FR-4, FR-5, FR-6, FR-7, FR-8, FR-9, FR-10, FR-11, FR-12, FR-58, FR-61, FR-63 · NFR-5
> Depends on: [01-platform-install-licensing](01-platform-install-licensing.md) (API framework, settings defaults), [03-lossless-accounting-live-monitoring](03-lossless-accounting-live-monitoring.md) (live widget + usage graphs on the user page), [06-managers-roles-security](06-managers-roles-security.md) (ownership scoping, permissions, audit log) · Depended on by: [02-radius-nas-aaa](02-radius-nas-aaa.md) (reads subscriber/profile state at auth), [05-billing-payments-vouchers](05-billing-payments-vouchers.md) (renewals change expiry/profile), [07-subscriber-portal-pwa](07-subscriber-portal-pwa.md) (portal reads subscriber state), [08-reports](08-reports.md) (subscriber reports)

## 1. Scope & context

The subscriber base and the service plans that govern it. This module owns subscriber CRUD and search, the user detail page (the single screen **Sara** lives on — key flow 2), bulk operations, CSV migration from SAS4, and profiles: price, duration, speeds, quotas, and the expiry/quota behaviors that [02](02-radius-nas-aaa.md) enforces at auth time. The product bar: any user findable in seconds, any daily task ≤ 3 clicks (NFR-5).

## 2. Owned requirements — elaborated

### FR-1 (M) — CRUD subscribers
**Master:** username, password, name, phone, address, notes, owner (manager), profile, expiry, status (active/disabled/expired), MAC address, static IP (optional).

*Elaboration:*
- **FR-1.1** — Username unique, case-insensitive, immutable after creation (it's the RADIUS identity); password storage per NFR-4 ([06](06-managers-roles-security.md)): encrypted-at-rest reversible (CHAP/MS-CHAP requirement), never displayed after save, reset-only.
- **FR-1.2** — `status` transitions: active ↔ disabled are manual (permission-gated); expired is derived from `expires_at` (a scheduled job + auth-time check flip it, so lists and auth agree). Disabling an online user offers immediate CoA disconnect.
- **FR-1.3** — Static IP validated against pools ([02](02-radius-nas-aaa.md) FR-16.2). Phone stored normalized (Iraqi formats accepted: `07xx…`, `+964…`).
- **FR-1.5** — Per-subscriber `whatsapp_opt_in` boolean (default off, shown next to the phone field): consent flag for [03](03-lossless-accounting-live-monitoring.md) FR-55 subscriber messaging (expiry reminders, receipts). Requires a valid phone to enable.
- **FR-1.4** — Every create/edit/delete writes the audit log with before/after ([06](06-managers-roles-security.md) FR-28). Any change affecting auth calls `InvalidatePolicy` ([02](02-radius-nas-aaa.md) contract).

### FR-2 (M) — Global instant search
**Master:** across username/name/phone from every screen.

*Elaboration:* persistent search bar in the panel shell, keyboard-focusable (`/` shortcut — NFR-5 "keyboard-first"); prefix + substring matching on username/name/phone, results as-you-type (< 300 ms at 5k subscribers), scoped to the manager's owned users ([06](06-managers-roles-security.md) FR-27); Enter on the top hit opens the user page (key flow 2 step 1).

### FR-3 (M) — User detail page
**Master:** combines live session state, usage graphs (daily/monthly), session history, payment history, audit log of changes.

*Elaboration:* composition contract — status banner (online/offline via [03](03-lossless-accounting-live-monitoring.md) live data; expiry countdown; remaining quota), live session widget ([03](03-lossless-accounting-live-monitoring.md)), usage graphs ([03](03-lossless-accounting-live-monitoring.md) FR-33 API), session history with last-disconnect reason (Sara's user story), payment history ([05](05-billing-payments-vouchers.md) ledger), audit trail ([06](06-managers-roles-security.md)). Primary actions pinned: **Renew** ([05](05-billing-payments-vouchers.md)), Disconnect, Edit, Reset MAC (FR-5).

### FR-4 (M) — Bulk actions
**Master:** on filtered user lists: enable/disable, change profile, extend expiry, move owner, export CSV.

*Elaboration:* actions run server-side against the filter (not just the visible page), show a preview count, execute async with a progress/result summary (per-row failures listed), write one audit entry per affected user, and fire policy invalidation + CoA where state changed for online users. Export respects manager scoping and the `export` permission.

### FR-5 (M) — Simultaneous-session limit & MAC lock
**Master:** per user; auto-learn first MAC, one-click reset.

*Elaboration:*
- **FR-5.1** — Session limit: per-user value, defaulting from the profile (FR-8); enforced at auth by [02](02-radius-nas-aaa.md) counting live sessions; limit-exceeded is a distinct reject reason (visible in FR-39 debug tool).
- **FR-5.2** — MAC lock: off / learn-first / fixed. In learn-first, the first successful auth stores `Calling-Station-Id`; subsequent auths must match; **Reset MAC** clears it (one click, audit-logged) so the next login re-learns — the standard "customer changed router" fix.

### FR-6 (S) — CSV import wizard
**Master:** for migrating subscriber bases from SAS4/other systems (field mapping + dry-run report).

*Elaboration:* upload → delimiter/encoding detection (Arabic text in CP1256/UTF-8 must both work) → column mapping UI with saved presets (a SAS4 export preset ships) → **dry run** validates all rows (duplicates, bad phones, unknown profiles with option to create) and reports per-row errors before any write → import creates subscribers idempotently (re-running skips already-imported usernames) and logs a summary. *(v2, FR-61: the mappable fields include `service_type`. It accepts the three canonical values and — because no other system models a three-valued service — a SAS4-era boolean "is this a hotspot user?" column, mapped exactly as migration 0500 maps HikRAD's own v1 bit: `true→dual`, `false→pppoe`, so an import and an upgrade agree. An operator who means hotspot-**only** writes `hotspot` explicitly. Omitted → `pppoe`.)* This is a named mitigation of the SAS4-competition risk (owned by [03](03-lossless-accounting-live-monitoring.md)) — it lowers switching cost.

### FR-7 (S) — Per-user overrides
**Master:** of profile attributes (custom rate limit, custom price on renewal).

*Elaboration:* nullable override fields on the subscriber (rate limit, renewal price); auth uses override-else-profile ([02](02-radius-nas-aaa.md)); renewal pricing uses override-else-profile ([05](05-billing-payments-vouchers.md) FR-19). Overrides are visibly badged on the user page and audit-logged.

### FR-8 (M) — Profiles
**Master:** price, duration (days), download/up speed (Mikrotik-Rate-Limit), data quota (total, or separate down/up) or unlimited, IP pool, simultaneous-session default.

*Elaboration:* speeds stored as abstract down/up (+ optional burst per FR-11) — vendor VSA rendering is [02](02-radius-nas-aaa.md) FR-17's adapter job. Quota modes: unlimited | total bytes | separate down/up bytes, per billing cycle (reset on renewal). Profiles in use cannot be deleted (archive instead); edits prompt "apply to existing users now vs. on next renewal" (immediate apply fires policy invalidation, and CoA rate-change for online users via [02](02-radius-nas-aaa.md) FR-15.2).

### FR-9 (M) — Expiry behavior per profile
**Master:** hard block, or move to "expired" pool (walled garden/redirect) via RADIUS attributes.

*Elaboration:* mode A **hard block** = Access-Reject with reason `expired`; mode B **expired pool** = Access-Accept with the expired pool ([02](02-radius-nas-aaa.md) FR-16.1 purpose=expired-walled-garden) + minimal rate, so the ISP's redirect page can upsell renewal (Omar's user story; key flow 1 step 4). Already-online users crossing expiry get CoA `move_pool` (mode B) or disconnect (mode A) within one enforcement-job cycle (≤ 5 min).

### FR-10 (M) — Quota-exhausted behavior per profile
**Master:** block, throttle to a configured speed, or move to expired pool.

*Elaboration:* quota consumption computed from [03](03-lossless-accounting-live-monitoring.md) usage data near-real-time (evaluated on interim processing); on exhaustion: block (disconnect + reject), throttle (CoA rate-change to the profile's configured throttle speed), or expired pool (as FR-9 mode B). Remaining quota is shown on the user page and portal ([07](07-subscriber-portal-pwa.md) FR-41).

### FR-11 (S) — Burst & time-of-day rules
**Master:** burst rate/threshold/time; time-of-day rules (e.g., free night quota, off-peak speed boost).

*Elaboration:* burst fields map to the MikroTik rate-limit string's burst segments (adapter-rendered). Time-of-day: per-profile windows granting speed boost and/or quota exemption; implemented via scheduled CoA sweeps at window boundaries for online sessions + correct attributes at auth; usage during exempt windows is flagged in `usage_points` so quota math excludes it ([03](03-lossless-accounting-live-monitoring.md) coordination).

### FR-12 (C) — Profile change scheduling
**Master:** upgrade applies at next renewal vs. immediately with proration.

*Elaboration (Could — build only if v1 schedule allows):* pending-profile field consumed at next renewal by [05](05-billing-payments-vouchers.md); immediate-with-proration debits/credits a ledger adjustment (FR-24 rules). No further depth until scheduled.

### FR-58 (S) — Dual-service login (PPPoE subscriber on Hotspot)
**Master:** A PPPoE subscriber can optionally also authenticate on Hotspot NASes with the same credentials (per-subscriber "allow Hotspot" toggle). The Hotspot session is allowed **in addition to** the PPPoE simultaneous-session limit (+1, at most one concurrent Hotspot session); Hotspot usage counts against the subscription's validity/expiry but **not** its data quota; Hotspot session speed uses a Hotspot-specific rate limit defined on the profile (falls back to the profile's main rate when unset).

*Elaboration:*
- **FR-58.1** — Data: ~~subscriber boolean `allow_hotspot` (default off)~~ **superseded by FR-61.1 (v2, shipped 2026-07-16): the column no longer exists** — the equivalent is `service_type='dual'`, and the v1 bit was migrated losslessly (`false→pppoe`, `true→dual`) by migration 0500. Profile optional fields `hotspot_rate_down/up` (null = fall back to the profile's main rate) are unchanged and still apply. Changing either calls `InvalidatePolicy` ([02](02-radius-nas-aaa.md) contract). *FR-58's auth semantics themselves are unchanged — `dual` preserves them exactly; only the storage generalized.*
- **FR-58.2** — Session rule (Decision 19a): the Hotspot session never counts into the PPPoE `session_limit`; exactly **one** concurrent Hotspot session is permitted per subscriber — a second concurrent Hotspot login rejects with `session_limit`.
- **FR-58.3** — Quota rule (Decision 19b): usage from Hotspot sessions is tagged by service in the accounting data ([03](03-lossless-accounting-live-monitoring.md) coordination, same mechanism family as FR-11's exemption flag) and **excluded** from FR-8/FR-10 quota consumption; it still appears in usage graphs and reports. Validity rule: Hotspot login requires a non-expired, non-disabled account — FR-9 expiry behaviors apply to it exactly like PPPoE.
- **FR-58.4** — Enforcement is auth-time in [02](02-radius-nas-aaa.md) (the authorize request's `service` field distinguishes pppoe/hotspot); this module owns the fields and rules. With the flag off, a Hotspot attempt rejects with `service_not_allowed` (visible in the FR-39 debug tool).

### FR-61 (S) — Subscriber service type (v2)
**Master:** Replace the FR-58 `allow_hotspot` boolean with a per-subscriber `service_type ∈ {pppoe, hotspot, dual}`; hotspot-only subscribers are full records that authenticate only on Hotspot services, with quota applied; `dual` keeps FR-58 semantics.

*Elaboration (this module owns the data + rules; [02](02-radius-nas-aaa.md) enforces them at auth):*
- **FR-61.1** — Data: subscriber column `service_type text NOT NULL DEFAULT 'pppoe' CHECK (service_type IN ('pppoe','hotspot','dual'))`, editable on the user page and via bulk action. **Migration is lossless**: `allow_hotspot=false → pppoe`, `true → dual`; the `allow_hotspot` column is dropped after backfill. Any change to `service_type` calls `InvalidatePolicy` ([02](02-radius-nas-aaa.md) contract). Profile Hotspot-rate fields (`hotspot_rate_down/up`) are unchanged and reused by hotspot-only and dual alike.
- **FR-61.2** — Service matrix (the rule [02](02-radius-nas-aaa.md) enforces, per the authorize request's `service`): `pppoe` accepts pppoe / rejects hotspot `service_not_allowed`; `hotspot` accepts hotspot / rejects pppoe `service_not_allowed` (the exact mirror); `dual` accepts both, with the Hotspot session keeping FR-58 semantics. A redeemed FR-22 voucher is inherently a Hotspot credential and bypasses this gate.
- **FR-61.3** — Hotspot-only session/quota/validity rules (differ from dual): concurrent Hotspot sessions are governed by the subscriber's `session_limit` **directly** (not the FR-58.2 fixed "+1, max one" rule, which exists only to protect a PPPoE limit); the account's **data quota applies** to Hotspot usage (contrast FR-58.3's dual exemption, which protects the PPPoE quota); expiry/disabled/validity behave exactly like PPPoE (FR-9). Hotspot rate = profile `hotspot_rate_*` when set, else main rate.
- **FR-61.4** — Portal (Decision 21): a hotspot-only subscriber has a full portal login and sees **consumed** data like any subscriber — never the quota ceiling/remaining balance.

### FR-63 (S) — Service-type & multi-service panel/UX (v2)
**Master:** Subscriber form service-type selector replacing the toggle; bulk `set_allow_hotspot → set_service_type`; NAS services sub-list + per-service wizard steps; `service_type` filters; FR-64 NAS/service assignment selectors on subscriber + profile forms.

*Elaboration:*
- **FR-63.1** — Subscriber form: a three-way service-type selector (radio: **PPPoE / Hotspot / Both**) replaces the `allow_hotspot` toggle; localized (en/ar/ku). The bulk action key `set_allow_hotspot` is renamed `set_service_type` (param `service_type`), API + panel in step.
- **FR-63.2** — Filters: `service_type` filter on the subscriber list (and, via the owning modules, live sessions and reports — coordinated with [03](03-lossless-accounting-live-monitoring.md)/[08](08-reports.md)).
- **FR-63.3** — Assignment selectors (FR-64, owned by [02](02-radius-nas-aaa.md)): NAS + service-instance pickers on the subscriber form and profile form (nullable = any NAS); this module renders them, [02](02-radius-nas-aaa.md) owns the columns' enforcement. The NAS-page services sub-list itself is [02](02-radius-nas-aaa.md)'s UX.

### NFR-5 (owned) — Usability
**Master:** Every daily operator task ≤ 3 clicks from dashboard; keyboard-first global search; a new front-desk operator productive within one hour using a one-page guide; mobile-responsive panel (Hassan's phone).

*Elaboration (ownership note):* this module owns the bar because the daily tasks live here (find user → see state → renew/fix). Click-budget audits for the canonical tasks (renew = search, open, Renew, confirm; reset MAC = search, open, Reset) are acceptance criteria. All panel modules inherit this NFR by reference; the one-page operator guide ships with P6 docs ([01](01-platform-install-licensing.md)).

## 3. Acceptance criteria

- **AC-2a** — Given 5,000 subscribers, when Sara types any 3-character fragment of a phone number, then matching users appear in < 300 ms and Enter opens the top result.
- **AC-3a** — Given a caller's username, when Sara opens the user page, then online/offline state, expiry, remaining quota, and last-disconnect reason are all visible without further clicks (key flow 2 step 2).
- **AC-4a** — Given a filter matching 800 users, when "extend expiry +7 days" runs, then all 800 update, each gets an audit entry, and online affected users keep working.
- **AC-5a** — Given MAC lock in learn-first with a learned MAC, when the user connects from a new router, then auth rejects with reason `mac-mismatch`; after Reset MAC, the next auth succeeds and learns the new MAC.
- **AC-6a** — Given a SAS4 CSV export with Arabic names, when the dry run executes, then per-row problems are reported, zero rows are written, and a subsequent import creates exactly the valid rows.
- **AC-9a** — Given profile mode "expired pool", when a subscriber expires while online, then within 5 minutes their session is CoA-moved to the expired pool, and their next auth gets pool attributes instead of a reject.
- **AC-10a** — Given a throttle-on-exhaustion profile, when an online user crosses quota, then a CoA rate-change applies the throttle speed without disconnect (where ROS supports it, else fallback per [02](02-radius-nas-aaa.md) FR-15.4).
- **AC-NFR5a** — Given the dashboard, then renew-a-user completes in ≤ 3 clicks after search, measured on the release build.
- **AC-58a** — Given a `service_type='dual'` subscriber *(v2: the FR-61 successor of `allow_hotspot` on; the criterion is otherwise unchanged and still passes verbatim)* with active PPPoE sessions at their session limit, when they log in on a Hotspot NAS, then the auth accepts with the profile's Hotspot rate (or main rate when unset); a second concurrent Hotspot login rejects with `session_limit`; with `service_type='pppoe'`, the Hotspot login rejects with `service_not_allowed`.
- **AC-58b** — Given N GB of Hotspot usage on a quota-limited profile, then remaining quota is unchanged while the usage graphs and reports include the N GB.
- **AC-61a** *(v2)* — Given a base of mixed `allow_hotspot` subscribers, when the 0500 migration runs, then every `false` becomes `service_type='pppoe'` and every `true` becomes `'dual'`, no row is lost, and all Phase-2 policy tests still pass with pppoe/dual semantics unchanged.
- **AC-61b** *(v2)* — Given a `hotspot`-only subscriber, when it authenticates on a Hotspot service it accepts (Hotspot rate, quota **enforced**, concurrent sessions capped by `session_limit`); when it attempts PPPoE it rejects `service_not_allowed`.
- **AC-63a** *(v2)* — Given the subscriber form, then the PPPoE/Hotspot/Both selector persists `service_type`, the bulk `set_service_type` action updates a filtered set, and the `service_type` list filter narrows results — all strings localized (i18n:check green).

## 4. Data & interfaces

**Owned entities:** `subscribers` (username, password_enc, name, phone, address, notes, owner_manager_id, profile_id, expires_at, status, mac_lock_mode, learned_mac, static_ip, overrides{rate, price}, pending_profile_id, **service_type** *(v2 FR-61, replaces `allow_hotspot`)*, whatsapp_opt_in, **nas_id / nas_service_id** *(v2 FR-64 assignment; nullable = any — columns enforced by [02](02-radius-nas-aaa.md))*), `profiles` (name, price, duration_days, rate_down/up, burst fields, hotspot_rate_down/up, quota_mode, quota bytes, throttle_rate, expiry_behavior, quota_behavior, pool_id, session_limit_default, archived, **nas_id / nas_service_id** *(v2 FR-64 default scope; nullable — enforced by [02](02-radius-nas-aaa.md))*), import batches/rows.

**Exposes:**
- `GET/POST/PUT/DELETE /api/v1/subscribers` (+ `/bulk`, `/search?q=`, `/{id}/reset-mac`), `GET/POST/PUT /api/v1/profiles`
- Read model for [02](02-radius-nas-aaa.md) auth: subscriber credential/status/expiry/quota-state/limits/overrides + profile attributes — served cache-first per NFR-1.
- Expiry/quota **enforcement job** events (subscriber crossed expiry/quota) → CoA calls into [02](02-radius-nas-aaa.md).
- Subscriber count/expiring queries for dashboard ([03](03-lossless-accounting-live-monitoring.md) FR-32) and reports ([08](08-reports.md)).

**Consumes:** usage/quota data + user-page widgets from [03](03-lossless-accounting-live-monitoring.md); renewal execution from [05](05-billing-payments-vouchers.md); scoping/permissions/audit from [06](06-managers-roles-security.md); billing defaults from settings ([01](01-platform-install-licensing.md) FR-53.2).

## 5. UX notes

User page is the product's center: status banner color-codes state (with text labels, not color alone), primary actions always above the fold, phone-width layout first-class (Hassan). Lists: saved filters, column chooser, sticky bulk-action bar when rows selected. All strings localized, RTL-mirrored ([07](07-subscriber-portal-pwa.md) NFR-6); usernames/MACs/IPs render LTR inside RTL. Empty/error states: import wizard must never dead-end — every failed row says why and how to fix.

## 6. Out of scope

- Renewal charging, receipts, balances → [05](05-billing-payments-vouchers.md) (this module only applies the expiry/profile outcome).
- Auth-time enforcement mechanics and vendor attribute rendering → [02](02-radius-nas-aaa.md).
- Live widget/graph internals → [03](03-lossless-accounting-live-monitoring.md).
- **Deferred by master:** FR-12 is Could (build last, drop first); reseller tree ownership hierarchies (Phase 2 — v1 ownership is a flat manager reference).

## 7. Risks & open questions (owned)

- *(No master risks or open questions are owned here; the SAS4-competition risk that FR-6 mitigates is owned by [03](03-lossless-accounting-live-monitoring.md).)*
- **NEW:** expired-status flip timing — auth-time check vs. scheduled job can disagree for up to one job cycle; define the job cadence (≤ 5 min) and make auth-time the authority.
- **NEW:** time-of-day quota exemption (FR-11) needs usage points attributable to windows even when an interim spans a boundary — accept interval-level approximation and document it.
- **NEW:** SAS4 export column format varies by SAS4 version — obtain real export samples from the pilot ISP (open question 2, owned by [02](02-radius-nas-aaa.md)) before finalizing the preset.
