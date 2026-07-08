# HikRAD — Sub-PRD 04: Subscribers & Profiles

> Derived from [docs/PRD.md](../PRD.md) v1.0 on 2026-07-08. Owns: FR-1, FR-2, FR-3, FR-4, FR-5, FR-6, FR-7, FR-8, FR-9, FR-10, FR-11, FR-12 · NFR-5
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

*Elaboration:* upload → delimiter/encoding detection (Arabic text in CP1256/UTF-8 must both work) → column mapping UI with saved presets (a SAS4 export preset ships) → **dry run** validates all rows (duplicates, bad phones, unknown profiles with option to create) and reports per-row errors before any write → import creates subscribers idempotently (re-running skips already-imported usernames) and logs a summary. This is a named mitigation of the SAS4-competition risk (owned by [03](03-lossless-accounting-live-monitoring.md)) — it lowers switching cost.

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

## 4. Data & interfaces

**Owned entities:** `subscribers` (username, password_enc, name, phone, address, notes, owner_manager_id, profile_id, expires_at, status, mac_lock_mode, learned_mac, static_ip, overrides{rate, price}, pending_profile_id), `profiles` (name, price, duration_days, rate_down/up, burst fields, quota_mode, quota bytes, throttle_rate, expiry_behavior, quota_behavior, pool_id, session_limit_default, archived), import batches/rows.

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
