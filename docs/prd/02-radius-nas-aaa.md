# HikRAD — Sub-PRD 02: RADIUS Core, NAS Management & AAA

> Derived from [docs/PRD.md](../PRD.md) v1.1 on 2026-07-08 (updated 2026-07-09: FR-56 added — Decision 17; FR-58 auth-time enforcement referenced — Decision 19; updated 2026-07-16 for master v1.4: FR-62 multi-service NAS + FR-64 subscriber/profile→NAS scoping enforced at auth — Decision 28, v2 phase 1; updated 2026-07-17 for master v1.5: FR-65–67 NAS auto-setup config manager + Hotspot/PPPoE server management — Decision 33, v2 phase 2). Owns: FR-13, FR-14, FR-15, FR-16, FR-17, FR-18, FR-56, FR-62, FR-64, FR-65, FR-66, FR-67 · NFR-1 · Risk: MikroTik CoA/attribute quirks · Open question 2 (pilot ISP)
> Depends on: [01-platform-install-licensing](01-platform-install-licensing.md) (Compose wiring, `/api/v1` framework), [04-subscribers-profiles](04-subscribers-profiles.md) (subscriber credentials/status, profile attributes it must translate into RADIUS replies) · Depended on by: [03-lossless-accounting-live-monitoring](03-lossless-accounting-live-monitoring.md) (accounting feed, CoA for disconnect actions), [05-billing-payments-vouchers](05-billing-payments-vouchers.md) (CoA on renewal, Hotspot voucher login)

## 1. Scope & context

This module is HikRAD's protocol heart: FreeRADIUS 3.2 wired to the Go backend's policy engine, so a MikroTik NAS (PPPoE or Hotspot) can authenticate subscribers, receive the right reply attributes, and be controlled via CoA/Disconnect. It also owns the NAS registry and setup wizard, IP pool management, and the vendor-neutral core design. Primary persona: **Ali** (network engineer) — his measures of success are "PPPoE auth works on the first try from the generated config" and "CoA that actually works." Key flow 1 (subscriber authenticates) from the master is owned here end-to-end except session persistence, which belongs to [03](03-lossless-accounting-live-monitoring.md).

**Auth path (master §8):** FreeRADIUS → `rlm_rest` → `hikrad-api` policy engine (credential check, account validity, quota, simultaneous-session limit, MAC lock) → attributes back. Sub-100 ms budget; policy data cached in Redis with explicit invalidation on renewals/edits.

## 2. Owned requirements — elaborated

### FR-13 (M) — CRUD NAS
**Master:** name, IP, RADIUS secret, type (PPPoE/Hotspot), CoA port, SNMP community (optional), location note.

*Elaboration:*
- **FR-13.1** — NAS records also carry: enabled flag, RouterOS major version note (6/7 — drives quirk handling, see owned risk), and assigned IP pools (FR-16).
- **FR-13.2** — Creating/editing/deleting a NAS updates FreeRADIUS's client list without manual file edits — FreeRADIUS reads clients dynamically from the DB or `hikrad-api` regenerates config + reloads. Unknown-NAS requests are rejected and surfaced in the debug tool ([03](03-lossless-accounting-live-monitoring.md) FR-39).
- **FR-13.3** — RADIUS secrets are encrypted at rest (NFR-4, owned by [06](06-managers-roles-security.md)); shown only at creation and via an explicit permission-gated "reveal".
- **FR-13.4** — Validation: unique NAS IP; CoA port default 3799; deleting a NAS with live sessions requires confirmation and marks its sessions stale (handled by [03](03-lossless-accounting-live-monitoring.md) FR-38).

### FR-14 (M) — NAS setup wizard with generated RouterOS config
**Master:** Setup wizard generates a copy-paste RouterOS config (RADIUS client, PPPoE/Hotspot AAA settings, walled-garden basics for Hotspot).

*Elaboration:*
- **FR-14.1** — Wizard steps: NAS details (FR-13 fields) → type-specific options (PPPoE: interface/service name; Hotspot: server name, walled-garden hosts) → generated snippet.
- **FR-14.2** — Generated snippet includes: `/radius add` (auth+acct, secret, src-address), `interim-update` interval (default 5 min, matching NFR-1 math), `use-radius=yes` for PPP/Hotspot AAA, `/radius incoming accept` for CoA (port), and for Hotspot the walled-garden entries needed for the expired-redirect and portal/payment hosts.
- **FR-14.3** — Separate snippet variants for RouterOS 6.49+ and 7.x where syntax differs; the wizard picks based on the version field with tabs to switch.
- **FR-14.4** — Post-paste verification: wizard offers a "Test" button that reports whether an Access-Request or Status-Server has been seen from that NAS IP since creation.

### FR-15 (M) — CoA / Disconnect-Message support
**Master:** disconnect session, apply new rate limit without disconnect where supported.

*Elaboration:*
- **FR-15.1** — `hikrad-api` sends Disconnect-Request and CoA-Request packets (UDP, per-NAS CoA port/secret) identifying the session by User-Name + Acct-Session-Id + Framed-IP-Address.
- **FR-15.2** — Operations exposed internally: `disconnect(session)`, `apply_rate(session, rate-limit)`, `move_pool(session, pool)` (used by renewals to lift a user out of the expired pool instantly — key flow 2 step 4, consumed by [05](05-billing-payments-vouchers.md) FR-19).
- **FR-15.3** — Every CoA attempt records result (ACK/NAK/timeout) in the audit trail and surfaces failures to the caller; timeout default 5 s with one retry. NAK/timeout falls back per operation (e.g. renewal falls back to disconnect so re-auth picks up new attributes; if that also fails, the UI reports it — never silently).
- **FR-15.4** — Rate-limit-without-disconnect uses MikroTik's supported CoA attributes; where a ROS version doesn't support in-place change, fall back to disconnect (see owned risk / test matrix).

### FR-16 (M) — IP pool management
**Master:** define pools, assign to profiles/NAS, view utilization %, warn on exhaustion.

*Elaboration:*
- **FR-16.1** — Pool = name + one or more CIDR/ranges + purpose (active / expired-walled-garden / static). Pools are referenced by profiles ([04](04-subscribers-profiles.md) FR-8) and returned as `Framed-Pool` (pool name — allocation happens on the MikroTik) at auth.
- **FR-16.2** — Static IPs (subscriber field, [04](04-subscribers-profiles.md) FR-1) are validated against pool ranges for uniqueness and returned as `Framed-IP-Address`, which takes precedence over `Framed-Pool`.
- **FR-16.3** — Utilization % computed from live sessions ([03](03-lossless-accounting-live-monitoring.md) data) vs. pool size; exhaustion warning threshold (default 90%) raises an alert event via the alert engine ([03](03-lossless-accounting-live-monitoring.md) FR-36).

### FR-17 (M) — Vendor-neutral RADIUS core
**Master:** MikroTik ships as the certified vendor via its dictionary/templates; architecture must not hard-code MikroTik so other vendor dictionaries can be added later (W for certifying other vendors in v1).

*Elaboration:*
- **FR-17.1** — Reply attributes are produced through a **vendor adapter layer**: the policy engine emits abstract intents (`rate_limit`, `address_pool`, `session_timeout`, `redirect_expired`) and a per-vendor adapter maps them to concrete VSAs (v1: `Mikrotik-Rate-Limit`, `Framed-Pool`, etc.). No `Mikrotik-*` literal outside the MikroTik adapter and its templates.
- **FR-17.2** — NAS `type`/vendor field selects the adapter; FreeRADIUS loads vendor dictionaries additively. Certifying a second vendor is explicitly **Won't** for v1 — the requirement on v1 is only that adding one requires no core changes.

### FR-18 (S) — Hotspot login page template
**Master:** ISP logo/colors template served for MikroTik Hotspot with voucher-code login.

*Elaboration:*
- **FR-18.1** — HikRAD serves a downloadable/customized MikroTik Hotspot `login.html` package themed from branding settings ([01](01-platform-install-licensing.md) FR-53), with username/password and **voucher-code** login (voucher becomes the credential; redemption logic owned by [05](05-billing-payments-vouchers.md) FR-22).
- **FR-18.2** — Walled-garden host list needed for the page's assets is included in the FR-14 snippet.

### FR-56 (S) — NAS auto-discovery & API auto-setup
**Master:** Discover MikroTik routers on reachable networks (MikroTik Neighbor Discovery / IP-range scan) to pre-fill the FR-14 wizard; optionally apply the generated config directly over the RouterOS API using admin-supplied router credentials (encrypted at rest). Auto-apply always shows a diff/preview before writing, makes only additive HikRAD-scoped changes — it never overwrites or deletes existing router config, and conflicts abort with a report; the FR-14 copy-paste snippet remains the always-available fallback.

*Elaboration:*
- **FR-56.1** — **Discovery:** listen for MikroTik Neighbor Discovery (MNDP, UDP 5678) on attached networks, plus an operator-triggered IP-range scan probing the RouterOS API port. Results (identity, RouterOS version, MAC, IP) pre-fill the FR-14 wizard and are deduplicated against already-registered NAS records. Discovery is passive/read-only — it never touches a router.
- **FR-56.2** — **Auto-setup:** with admin-supplied router credentials (AES-GCM encrypted at rest like SNMP communities, NFR-4; write-only after save, reveal permission-gated per FR-13.3), HikRAD connects over the RouterOS API (8728/8729) and applies the FR-14.2 config. **Safety contract (frozen by Decision 17):** a mandatory diff/preview (exact commands to run + current-router-state check) precedes any write; changes are additive and HikRAD-scoped only — existing router entries are never modified or deleted; a conflict (e.g. an existing `/radius` entry pointing elsewhere) aborts the apply with a per-item report instead of overwriting. This is the scariest write path in the product — it must be boringly predictable.
- **FR-56.3** — Copy-paste (FR-14) remains the always-available path; any API failure falls back to showing the snippet. A successful apply automatically runs the FR-14.4 "seen since created" test and reports the result.
- **FR-56.4** — All RouterOS API client code lives inside the MikroTik vendor adapter boundary — the FR-17.1 rule applies verbatim and the CI vendor-isolation grep covers it.

### FR-62 (S) — Multi-service NAS (v2)
**Master:** A router can run multiple Hotspot + PPPoE server instances at once; a `nas_services` child table replaces the one-`type`-per-NAS model; the policy engine resolves the service instance from RADIUS attributes (vendor adapter owns the mapping, FR-17); FR-14/FR-56 cover all enabled services; live/graphs/reports can group by instance; `nas.type` retired after backfill.

*Elaboration:*
- **FR-62.1** — Schema `nas_services` (one NAS → N rows): `service pppoe|hotspot`, `label` (zone/SSID), `interface_note`, `ip_pool_id` (per-service pool, nullable), `ros_server_name` (RouterOS Hotspot server name / PPPoE service-name used for instance matching), `enabled`. Migration seeds one row per existing NAS from `nas.type`, then drops `nas.type`. Every NAS has ≥ 1 service row.
- **FR-62.2** — Service-instance resolution at auth is **vendor-owned** (FR-17): the FreeRADIUS bridge forwards the raw identifying attributes (Called-Station-Id, NAS-Port-Type, NAS-Port-Id) into the authorize request; the MikroTik adapter maps them to the matching `nas_services` row (by `ros_server_name` for Hotspot, service-name for PPPoE), falling back to the NAS's single enabled service of the coarse `service` kind when no finer match exists. No RADIUS-attribute parsing for instance identity appears outside `internal/radius/vendor/` — the CI isolation grep covers it.
- **FR-62.3** — The resolved instance supplies the address pool (FR-64 precedence) and per-instance attributes. Resolution failure rejects **`nas_not_allowed`** (surfaced in the FR-39 debug tool) rather than guessing, in two cases: **no enabled instance of the requested kind on this NAS** (e.g. a hotspot login on a PPPoE-only router — a *configuration* fact, deliberately distinct from `service_not_allowed`, which means the *subscriber* may not use the service), and an ambiguous match (several candidate instances of that kind, none matching). Guessing would hand the session another zone's address pool.
- **FR-62.4** — FR-14 wizard renders one snippet covering **all** enabled services (multiple `/ip hotspot` servers + PPPoE AAA + a single shared `/radius` whose `service=` list covers every enabled kind), ROS 6/7 tabs preserved; FR-56 auto-setup treats each service additively. Each hotspot block addresses its profile via the instance's own `ros_server_name` rather than `[find]`, so configuring a second zone cannot silently re-point the first. *Known limitation:* auto-setup's hotspot half still only targets the stock profile named `default` and refuses (conflict, nothing written) on a router whose zones carry their own profiles — the copy-paste snippet handles that router; reading the real profile layout is v2 phase 2's subject. See `docs/ops/known-issues.md`.
- **FR-62.6** — **Service discovery** (added 2026-07-16 on owner request; an early slice of v2 phase 2's config-manager scope, built here because operators were hand-typing service names during the pilot). `POST /api/v1/nas/{id}/discover-services` reads the router's real instances over the RouterOS API with the FR-56.2 saved credentials — `/interface/pppoe-server/server/print` and `/ip/hotspot/print` — and returns them for the operator to confirm in the NAS form. **Read-only on both sides**: print sentences only (never add/set, same contract as FR-56's preview), and it writes nothing to HikRAD either — it proposes rows the operator saves through the normal `services[]` array, so a discovery mistake can never silently rewrite a NAS that is authenticating people. Already-present instances come back with `matched_service_id` so a re-run offers no duplicates, and the panel **merges** rather than replaces: an existing row keeps its id (and therefore its pool assignment and the subscribers scoped to it) and refreshes only what the router is authoritative for (server name, interface, enabled). Rows the router does not report are kept, not deleted — discovery is evidence of what the router answered, not evidence a service is gone. Each hotspot's **`address-pool` is reported** too: that name must match a real `/ip pool` on the router or the login fails `no address from ip pool`, and surfacing it is how the operator sees HikRAD's pool name and the router's side by side. 422 without API credentials, 502 when the router doesn't answer (mirrors the FR-13 probe). RouterOS paths/fields live only in the vendor adapter (FR-17).
- **FR-62.5** — **Accounting resolves the instance too** (added during build, 2026-07-16): a session's service can no longer be inherited from the NAS, so `hikrad-acct` resolves it per record through the same vendor seam (the accounting bridge forwards the same raw attributes). `sessions.nas_service_id` (migration 0503, owned by [03](03-lossless-accounting-live-monitoring.md)) records which instance a session ran on — this is what lets live/graphs/reports group by instance. Resolution **never drops a record** (M2 outranks attribution): adapter answer → the NAS's sole enabled instance when unambiguous → the coarse hint, unattributed. Rationale in `docs/ops/known-issues.md` (the pipeline previously read the retired `nas.type` and would have silently filed every session as `pppoe`).

### FR-64 (S) — Subscriber/profile → NAS scoping, enforced at auth (v2)
**Master:** Subscribers and profiles can be assigned to specific NAS devices + service instances (nullable = any); enforced at RADIUS auth with a new `nas_not_allowed` reject; precedence subscriber-over-profile; missing-pool-anywhere omits `address_pool` so the router uses its local pool; assignment carried in AuthView + `InvalidatePolicy` on change.

*Elaboration (this module owns the columns' **enforcement**; [04](04-subscribers-profiles.md) renders the pickers):*
- **FR-64.1** — Columns: `nas_id` + `nas_service_id` (nullable, `ON DELETE SET NULL`) on **both** `subscribers` and `profiles`. Nullable pair = any NAS/service (v1 behaviour, the default). `nas_service_id` set without `nas_id` implies its parent NAS.
- **FR-64.2** — Enforcement order (auth-time): after known-NAS + service-instance resolution (FR-62.2) and subscriber resolution, compute the **effective assignment** with subscriber-over-profile precedence (subscriber's pair wins whole when its `nas_id` is set; else the profile's). If the effective assignment is non-empty and the authenticating NAS (and, when `nas_service_id` is set, the resolved service instance) does not match, reject `nas_not_allowed`. Empty effective assignment = accept anywhere. This check sits before credentials in the chain so scope is enforced regardless of password (order is frozen in the phase brief).
- **FR-64.3** — Address-pool precedence (FR-16) is **service-aware** (corrected 2026-07-16 after a pilot bug — v1 leaked the PPPoE profile pool onto hotspot sessions, causing "no more free addresses" on the router; see `docs/ops/known-issues.md`). **PPPoE:** static IP → resolved pppoe-service `ip_pool_id` → profile `pool_id` → omit. **Hotspot:** static IP → resolved hotspot-service `ip_pool_id` → **omit** — the profile `pool_id` (a PPPoE pool) is **never** applied to a hotspot session; omitting `address_pool` makes the MikroTik Hotspot use its own interface/DHCP pool. Omit = no `address_pool` intent (the engine already only emits it for a non-empty pool). Locked with tests; documented on the pools screen ("profile pools are PPPoE; hotspot pools are per-service or router-local").
- **FR-64.4** — AuthView carries the effective `nas_id`/`nas_service_id`; `InvalidatePolicy(subscriberID)` fires on any subscriber **or profile** assignment change (a profile change fans out to its subscribers' cached views).
- **FR-64.5** — Panel: `nas_not_allowed` is localized (en/ar/ku) in the FR-39 debug reason list; the NAS page shows a per-service session/status sub-list (FR-63 renders the subscriber/profile assignment pickers).

### FR-65 (S) — NAS config inspection, read-only (v2)
**Master:** `GET /api/v1/nas/{id}/config` connects with the saved RouterOS API credentials (FR-56.2) and returns the router's current RADIUS-relevant state — `/radius` entries, `/radius incoming`, `/ppp aaa`, `/ip hotspot profile` AAA fields, walled-garden entries, plus the version probe — read-only, audit-logged.

*Elaboration:*
- **FR-65.1** — Pure print sentences (same `ROSConn.Read` seam as FR-56/FR-62.6/health-check); never write. 422 without saved API credentials, 502 when the router doesn't answer (mirrors the FR-13 probe / FR-62.6 discovery contract).
- **FR-65.2** — Response is shaped to be diffable against FR-66's plan input, not a raw sentence dump: one section per config area, each row carrying exactly the fields the planner reasons about (address, service, secret-presence — never the secret value — comment, accept/port, use-radius, interim, walled-garden host list).
- **FR-65.3** — Panel: "Current config" tab in the auto-setup modal (FR-14/FR-56 wizard), rendered before the operator edits the FR-66 values form so pre-fill has something to pre-fill from.
- **FR-65.4** — Audit-logged like every router-facing read (`nas.config_inspect`), consistent with FR-56.1 discovery and FR-62.6 service discovery being explicitly read-only-on-both-sides.

### FR-66 (S) — Form-driven auto-setup plan input + modify-or-create per item (v2)
**Master:** The FR-56 auto-setup wizard becomes a two-step flow (values form → preview computed from the form); preview items gain a per-conflict resolution choice (`keep existing` / `update to planned value` / `abort`) instead of only aborting. Abort-on-conflict remains the default (Decision 17's safety contract, extended not replaced).

*Elaboration:*
- **FR-66.1** — Values form: RADIUS server address, CoA port, interim interval, walled-garden hosts, per-service toggles — pre-filled from HikRAD settings and, where FR-65 can read them, from the router's current values. The same overrides extend `vendor.SnippetInput` (C8) so the FR-14 copy-paste snippet and the FR-56.2 auto-setup plan describe one desired state from one input, never two.
- **FR-66.2** — `PlanAutoSetup` gains a fourth bucket beside its existing three (no-op / additive item / conflict, see FR-56.2's elaboration): a conflict **with a resolution**. The operator picks per conflicting item; `keep` drops it from the plan (nothing written for that item); `update` turns it into a **PlanItem-with-before-state** the apply step executes as a `/set` (not a blind `/add`) with the existing value recorded for the audit log; `abort` (the default, and the only choice for an unresolved item) keeps today's whole-apply-refuses behavior.
- **FR-66.3** — The `preview_hash` (frozen by FR-56.2/C6) is extended to cover the chosen resolutions, not only the plan items: a router-state change between preview and apply — including one that invalidates a chosen `update` (the router now has yet another different value) — still aborts apply with `preview_stale`, exactly as today's hash does for the additive-only case.
- **FR-66.4** — **Never delete.** No resolution or plan item this FR introduces issues a `/remove` sentence; `update` is always a `/set` on an item the router already has. Router-side removal is manual, by design, unchanged from FR-56.2.
- **FR-66.5** — Apply executes adds then chosen updates, still whole-apply-abort-on-first-failure (unchanged from FR-56.2's `ApplyAutoSetup` contract) and still refuses outright while any item remains `abort`-resolved.

### FR-67 (S) — Hotspot & PPPoE server management (v2, Decision 31)
**Master:** Every `nas_services` instance carries a management mode (`router` = discovered/adopted, read-only until adopted; `system` = HikRAD-created and HikRAD-owned). Operators can list live router-side config per instance, create new system-managed servers, edit system-managed instances, and adopt router-managed ones — all through FR-66's hash-gated pipeline. Deletion stays manual on the router.

*Elaboration:*
- **FR-67.1** — `nas_services.management_mode` (`router|system`, default `router` for anything not created through this FR — including every row backfilled by v2 phase 1's migration 0501, which came from discovery/manual entry, never from HikRAD provisioning). A row created via FR-67's create flow is `system` from the start.
- **FR-67.2** — **List**: the NAS services screen renders each instance's real router-side config — reusing FR-65's inspection plumbing scoped to one service (hotspot: interface, address pool, hotspot+user profile, RADIUS flags; PPPoE: service-name, interfaces, default profile, authentication) — read live, not from HikRAD's cached `nas_services` row, so the operator always sees router truth.
- **FR-67.3** — **Create (system-managed)**: a panel form for a new Hotspot server (interface, local address/pool, profile values, walled-garden per FR-14.2, RADIUS wired to HikRAD, including the FR-62.7 `address-pool=none` user-profile guard from day one) or a new PPPoE server (interface(s), service-name, profile with RADIUS auth), planned/previewed/applied through FR-66's pipeline, then a `nas_services` row is inserted automatically with `management_mode='system'` on a successful apply — the operator never hand-fills the `nas_services` form afterward.
- **FR-67.4** — **Edit**: a `system`-mode instance can be re-formed (same create form, pre-filled from the stored row) and re-applied — diff preview, before/after audit, same FR-66 pipeline. A `router`-mode instance is read-only from HikRAD until adopted (FR-67.5); attempting to edit one is refused with a message pointing at adopt.
- **FR-67.5** — **Adopt**: for a `router`-mode instance, HikRAD reads its full current config (FR-65/FR-67.2) into the same edit form pre-filled entirely from the router, the operator reviews and confirms (even an unchanged confirm is an explicit action), and only then does `management_mode` flip to `system` — never silently, never as a side effect of merely viewing the config.
- **FR-67.6** — **Never delete.** No server is ever removed from the router by HikRAD, matching FR-66.4. Disabling a `nas_services` row (existing FR-62 toggle) only stops HikRAD serving/counting that instance; the router-side server is untouched either way.

### NFR-1 (owned) — Performance
**Master:** At 5,000 subscribers / ~2,000 concurrent sessions with 5-minute interims (~7 acct packets/sec sustained, 50/sec burst): auth latency < 100 ms at the backend (p99), accounting ingest keeps queue depth near zero, panel pages load < 1.5 s, live-session updates ≤ 2 s end-to-end.

*Elaboration (ownership note):* this module owns the **auth latency < 100 ms p99** budget: policy decision reads (subscriber, profile, session count, MAC lock) served from Redis cache with explicit invalidation on renewal/edit ([04](04-subscribers-profiles.md)/[05](05-billing-payments-vouchers.md) must call the invalidation hook); DB fallback on cache miss must still meet budget at the 5k scale. The accounting-ingest and live-update numbers are implemented by [03](03-lossless-accounting-live-monitoring.md); the panel-load number by each UI module — all referencing this NFR.

### Enforced-here policies owned elsewhere (reference, not ownership)
At Access-Request time this module enforces, per the master's key flow 1: credential check against stored password (storage rules NFR-4 → [06](06-managers-roles-security.md)); status active/disabled/expired and expiry behavior (FR-9 → [04](04-subscribers-profiles.md)); quota-exhausted behavior (FR-10 → [04](04-subscribers-profiles.md)); simultaneous-session limit and MAC lock incl. first-MAC auto-learn (FR-5 → [04](04-subscribers-profiles.md)); per-user overrides (FR-7 → [04](04-subscribers-profiles.md)); dual-service login (FR-58 → [04](04-subscribers-profiles.md)): a Hotspot-service Access-Request for a PPPoE subscriber is accepted only when 04's allow-Hotspot flag is set — at most one concurrent Hotspot session, **not** counted against the PPPoE session limit, reply rate = the profile's Hotspot-specific rate (fallback: main rate), and the session is tagged `hotspot` in accounting so [03](03-lossless-accounting-live-monitoring.md)/[04](04-subscribers-profiles.md) exclude its usage from quota math (it still counts for graphs/reports and requires a non-expired, non-disabled account). Reject reason for a non-flagged attempt: `service_not_allowed`. This file defines *where* they execute; their business rules live with their owners.

**v2 (FR-61/64 → owned data in [04](04-subscribers-profiles.md), enforcement here):** the FR-58 flag generalizes to `service_type ∈ {pppoe,hotspot,dual}` — this module applies the service matrix (pppoe/hotspot each reject the other kind `service_not_allowed`; dual keeps FR-58; hotspot-only uses `session_limit` for concurrent Hotspot sessions and **applies** the data quota); and enforces the FR-64 subscriber/profile→NAS scope (`nas_not_allowed`). The concrete authorize check chain, request/response deltas and matrix are frozen in `docs/v2/phases/phase-v2-1-hotspot-management/00-phase.md`.

## 3. Acceptance criteria

- **AC-13a** — Given a new NAS created in the panel, when that router sends an Access-Request with the right secret, then it is accepted as a known client with no service restart or manual file edit.
- **AC-14a** — Given Ali pastes the generated ROS 7 snippet into a clean MikroTik, when a test subscriber dials PPPoE, then auth succeeds on the first try and accounting starts flowing (M4 flow).
- **AC-15a** — Given an online session, when an operator clicks Disconnect in Live Sessions, then the MikroTik ends the session within 5 s and the CoA result is recorded.
- **AC-15b** — Given an online user in the expired pool, when a renewal completes, then a CoA restores full-speed attributes without the user redialing (key flow 2 step 4).
- **AC-16a** — Given a pool at 91% utilization, then the panel shows the warning state and an alert event fires per routing rules.
- **AC-17a** — Given the codebase, when searched for MikroTik VSA names, then they appear only in the MikroTik adapter/dictionary/templates.
- **AC-NFR1a** — Given 2,000 active sessions and a 50/sec burst of auth requests (CI packet harness, NFR-8), then backend auth latency p99 < 100 ms.
- **AC-18a** — Given Hotspot type NAS with the served login page, when a subscriber enters a valid unused voucher code, then they get online and the voucher is consumed (verified with [05](05-billing-payments-vouchers.md)).
- **AC-56a** — Given a discovered router and valid admin credentials, when auto-setup preview is accepted, then only additive HikRAD-scoped entries are created on the router and a subsequent test Access-Request succeeds; given a conflicting existing `/radius` entry, the apply aborts with a per-item report and the router is unchanged.
- **AC-62a** *(v2)* — Given one router with two Hotspot services + one PPPoE service (three `nas_services` rows), when a subscriber authenticates on each, then each is resolved to its own instance and receives that instance's pool; the FR-14 snippet configures all three; live sessions show the service instance. The service-instance resolution from RADIUS attributes appears only inside `internal/radius/vendor/` (isolation grep green).
- **AC-64a** *(v2)* — Given a subscriber assigned to NAS A (and/or a service instance), when it authenticates through NAS B (or a non-assigned instance), then auth rejects `nas_not_allowed`; through the assigned NAS/service it accepts. A profile-level assignment applies to its subscribers unless the subscriber sets its own (subscriber-over-profile).
- **AC-64b** *(v2)* — Given a dual/hotspot subscriber whose profile carries a PPPoE pool but whose resolved hotspot service carries none, when it logs in on Hotspot, then the accept reply contains **no** `address_pool` intent (router-local pool used — the v1 "no free addresses" bug cannot recur); the same subscriber's PPPoE login still receives the profile pool; setting a hotspot-service pool makes the Hotspot reply emit that pool.
- **AC-65a** *(v2)* — Given a NAS with saved API credentials and a foreign `/radius` entry plus a walled-garden list, when `GET /nas/{id}/config` is called, then the response lists that entry, the walled-garden hosts, and the PPP AAA/hotspot-profile RADIUS flags exactly as the router reports them, and an audit row is written; given no saved credentials, the endpoint returns 422.
- **AC-66a** *(v2)* — Given a router with a foreign `/radius` entry, when preview runs, then the item is reported as a conflict with all three resolution choices available; choosing `update` and applying rewrites it to HikRAD's values and the audit log shows before/after; choosing `keep` applies every other item but leaves that one untouched and unwritten; choosing neither (default) still aborts the whole apply with nothing written, identical to pre-FR-66 behavior.
- **AC-66b** *(v2)* — Given a preview whose `update` resolution was chosen, when the router's value for that item changes again before apply is called, then apply refuses with `preview_stale`, identical to how an additive-item drift is caught today.
- **AC-67a** *(v2)* — Given an operator creating a new Hotspot server via the FR-67.3 form on a NAS that already runs a PPPoE service, when the plan applies cleanly, then a new `nas_services` row appears with `management_mode='system'`, the new zone is reachable, and a subscriber can authenticate through it and receive that instance's pool (not the PPPoE profile's).
- **AC-67b** *(v2)* — Given a pre-existing, HikRAD-discovered PPPoE server (`management_mode='router'`), when the operator opens it in HikRAD, then its config is shown read-only with an "Adopt" action and no edit form; after adopting (explicit confirm), `management_mode` becomes `system` and the operator can now change its default profile from HikRAD, with the change appearing in the audit log with before/after values.
- **AC-67c** *(v2)* — Given any `nas_services` row regardless of management mode, then no HikRAD action ever issues a RouterOS `/remove` sentence against it — verified by the same vendor-isolation-style grep FR-56.2/56.4 already run, extended to cover FR-66/67's new plan paths.

## 4. Data & interfaces

**Owned entities:** `nas` (id, name, ip, secret_enc, ~~type~~ *(v2 FR-62: retired — moved to `nas_services`)*, vendor, coa_port, snmp_community_enc, ros_version, location, enabled, api_port, api_user, api_password_enc — the api_* fields for FR-56 auto-setup), `nas_services` *(v2 FR-62: nas_id, service pppoe|hotspot, label, interface_note, ip_pool_id, ros_server_name, enabled; **v2 FR-67 adds `management_mode` `router|system`, default `router`**)*, `ip_pools` (id, name, ranges[], purpose), `pool_assignments` (pool ↔ profile/NAS). Enforces (data owned by [04](04-subscribers-profiles.md)) the FR-64 `subscribers.nas_id/nas_service_id` and `profiles.nas_id/nas_service_id` assignment columns.

**v2 additions (FR-65–67):**
- `GET /api/v1/nas/{id}/config` (FR-65) — read-only router state (`/radius`, `/radius incoming`, `/ppp aaa`, hotspot-profile RADIUS flags, walled-garden, version); 422 no-credentials / 502 unreachable, same contract shape as the FR-56/FR-62.6 read paths.
- `POST /api/v1/nas/{id}/auto-setup/preview` (FR-56, extended by FR-66) — request body gains the values-form overrides + prior resolution choices; response's `conflicts[]` items gain `resolvable: true` and the preview hash covers resolutions.
- `POST /api/v1/nas/{id}/auto-setup/apply` (FR-56, extended by FR-66) — request body gains `resolutions: {conflict_key: "keep"|"update"|"abort"}`; unresolved or `abort`-resolved conflicts still refuse the whole apply exactly as FR-56.2 always has.
- `POST /api/v1/nas/{id}/services/{serviceId}/adopt` (FR-67.5) — flips `management_mode` to `system` after the operator confirms the read config; audited.
- Server create/edit for FR-67.3/67.4 route through the same preview/apply pair as above, scoped to one service's sentences, then upsert the `nas_services` row on a successful apply.

Full request/response shapes, the resolution enum, and the `management_mode` state machine are frozen in [docs/v2/phases/phase-v2-2-autosetup-config-manager/00-phase.md](../v2/phases/phase-v2-2-autosetup-config-manager/00-phase.md) once written at that phase's kickoff.

**Exposes:**
- `POST /api/v1/radius/authorize` — internal endpoint called by FreeRADIUS `rlm_rest`; input: RADIUS request attrs; output: accept/reject + abstract attribute intents (vendor adapter applied before reply).
- Go-internal CoA service: `Disconnect(sessionRef)`, `ApplyRate(sessionRef, rate)`, `MovePool(sessionRef, pool)` — consumed by [03](03-lossless-accounting-live-monitoring.md) (UI disconnect) and [05](05-billing-payments-vouchers.md) (renewal).
- `GET/POST/PUT/DELETE /api/v1/nas`, `/api/v1/pools` + `GET /api/v1/nas/{id}/config-snippet`.
- FR-56: `POST /api/v1/nas/discover` (trigger scan + return found routers), `POST /api/v1/nas/{id}/auto-setup/preview` (diff/current-state report), `POST /api/v1/nas/{id}/auto-setup/apply` (permission-gated, audited).
- Cache-invalidation hook: `InvalidatePolicy(subscriberID)` — **contract:** any module changing subscriber/profile/billing state that affects auth MUST call it.

**Consumes:** subscriber + profile read models from [04](04-subscribers-profiles.md); accounting forwarding target `hikrad-acct` is [03](03-lossless-accounting-live-monitoring.md)'s (FreeRADIUS acct config written here, semantics owned there); branding for FR-18 from [01](01-platform-install-licensing.md) FR-53.

## 5. UX notes

NAS list = health-at-a-glance cards (status color comes from [03](03-lossless-accounting-live-monitoring.md) FR-34 probes) with add-NAS as a primary action. Wizard snippet blocks: monospace, copy button, ROS 6/7 tabs. All screens RTL-ready per NFR-6 ([07](07-subscriber-portal-pwa.md)); config snippets and attribute names always render LTR inside RTL layouts. Errors must be Ali-grade actionable ("secret mismatch — the router at 10.0.0.2 sent a bad Message-Authenticator").

## 6. Out of scope

- Accounting ingestion, session records, live sessions UI, stale reaper, RADIUS debug tool → [03](03-lossless-accounting-live-monitoring.md) (FR-31, 37–40).
- Business rules for expiry/quota/session-limit/MAC lock → [04](04-subscribers-profiles.md) (FR-5, 9, 10).
- Voucher redemption logic behind Hotspot login → [05](05-billing-payments-vouchers.md) FR-22.
- **Deferred by master:** certifying non-MikroTik vendors (Won't in v1, FR-17); FTTH/OLT management (non-goal).
- **v2 FR-66/67 explicitly never delete** router config — removal of a `/radius` entry, a hotspot server, or any other router-side object stays a manual, Winbox/CLI-only action, by design (Decision 31/33), not a gap to close later.

## 7. Risks & open questions (owned)

- **Risk (master): MikroTik CoA/attribute quirks across RouterOS 6/7.** Likelihood Medium / Impact High. Mitigation: test matrix on ROS 6.49 & 7.x early (P1–P2); packet-level test harness in CI (NFR-8). *Elaboration:* maintain a quirk table per ROS version (CoA rate-change support, attribute casing, Hotspot login differences) driving both the vendor adapter and FR-14.3 snippet variants.
- **Open question 2 (master): Pilot ISP** — which ISP hosts the pilot; drives the ROS-version test matrix (and Kurdish-language priority, which [07](07-subscriber-portal-pwa.md) tracks). Target: decide during P4.
- **NEW:** interim-update interval trade-off — 5 min default matches NFR-1 sizing; shorter intervals improve live-rate accuracy ([03](03-lossless-accounting-live-monitoring.md) FR-31) but multiply accounting volume. Make it a per-NAS wizard option with guidance.
- **NEW:** decide whether FreeRADIUS reads NAS clients via SQL (`rlm_sql` clients) or generated config + reload — affects how fast FR-13.2 changes take effect. Resolve in P1.
- **NEW (FR-56):** the RouterOS API surface and command syntax differ between ROS 6.49 and 7.x — auto-setup preview/apply must be validated as part of the P5 ROS test matrix before it is enabled against real routers; until validated per version, the wizard falls back to copy-paste for that version.
