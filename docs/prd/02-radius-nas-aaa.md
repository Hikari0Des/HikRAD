# HikRAD — Sub-PRD 02: RADIUS Core, NAS Management & AAA

> Derived from [docs/PRD.md](../PRD.md) v1.1 on 2026-07-08 (updated 2026-07-09: FR-56 added — Decision 17; FR-58 auth-time enforcement referenced — Decision 19). Owns: FR-13, FR-14, FR-15, FR-16, FR-17, FR-18, FR-56 · NFR-1 · Risk: MikroTik CoA/attribute quirks · Open question 2 (pilot ISP)
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

### NFR-1 (owned) — Performance
**Master:** At 5,000 subscribers / ~2,000 concurrent sessions with 5-minute interims (~7 acct packets/sec sustained, 50/sec burst): auth latency < 100 ms at the backend (p99), accounting ingest keeps queue depth near zero, panel pages load < 1.5 s, live-session updates ≤ 2 s end-to-end.

*Elaboration (ownership note):* this module owns the **auth latency < 100 ms p99** budget: policy decision reads (subscriber, profile, session count, MAC lock) served from Redis cache with explicit invalidation on renewal/edit ([04](04-subscribers-profiles.md)/[05](05-billing-payments-vouchers.md) must call the invalidation hook); DB fallback on cache miss must still meet budget at the 5k scale. The accounting-ingest and live-update numbers are implemented by [03](03-lossless-accounting-live-monitoring.md); the panel-load number by each UI module — all referencing this NFR.

### Enforced-here policies owned elsewhere (reference, not ownership)
At Access-Request time this module enforces, per the master's key flow 1: credential check against stored password (storage rules NFR-4 → [06](06-managers-roles-security.md)); status active/disabled/expired and expiry behavior (FR-9 → [04](04-subscribers-profiles.md)); quota-exhausted behavior (FR-10 → [04](04-subscribers-profiles.md)); simultaneous-session limit and MAC lock incl. first-MAC auto-learn (FR-5 → [04](04-subscribers-profiles.md)); per-user overrides (FR-7 → [04](04-subscribers-profiles.md)); dual-service login (FR-58 → [04](04-subscribers-profiles.md)): a Hotspot-service Access-Request for a PPPoE subscriber is accepted only when 04's allow-Hotspot flag is set — at most one concurrent Hotspot session, **not** counted against the PPPoE session limit, reply rate = the profile's Hotspot-specific rate (fallback: main rate), and the session is tagged `hotspot` in accounting so [03](03-lossless-accounting-live-monitoring.md)/[04](04-subscribers-profiles.md) exclude its usage from quota math (it still counts for graphs/reports and requires a non-expired, non-disabled account). Reject reason for a non-flagged attempt: `service_not_allowed`. This file defines *where* they execute; their business rules live with their owners.

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

## 4. Data & interfaces

**Owned entities:** `nas` (id, name, ip, secret_enc, type, vendor, coa_port, snmp_community_enc, ros_version, location, enabled, api_port, api_user, api_password_enc — the api_* fields for FR-56 auto-setup), `ip_pools` (id, name, ranges[], purpose), `pool_assignments` (pool ↔ profile/NAS).

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

## 7. Risks & open questions (owned)

- **Risk (master): MikroTik CoA/attribute quirks across RouterOS 6/7.** Likelihood Medium / Impact High. Mitigation: test matrix on ROS 6.49 & 7.x early (P1–P2); packet-level test harness in CI (NFR-8). *Elaboration:* maintain a quirk table per ROS version (CoA rate-change support, attribute casing, Hotspot login differences) driving both the vendor adapter and FR-14.3 snippet variants.
- **Open question 2 (master): Pilot ISP** — which ISP hosts the pilot; drives the ROS-version test matrix (and Kurdish-language priority, which [07](07-subscriber-portal-pwa.md) tracks). Target: decide during P4.
- **NEW:** interim-update interval trade-off — 5 min default matches NFR-1 sizing; shorter intervals improve live-rate accuracy ([03](03-lossless-accounting-live-monitoring.md) FR-31) but multiply accounting volume. Make it a per-NAS wizard option with guidance.
- **NEW:** decide whether FreeRADIUS reads NAS clients via SQL (`rlm_sql` clients) or generated config + reload — affects how fast FR-13.2 changes take effect. Resolve in P1.
- **NEW (FR-56):** the RouterOS API surface and command syntax differ between ROS 6.49 and 7.x — auto-setup preview/apply must be validated as part of the P5 ROS test matrix before it is enabled against real routers; until validated per version, the wizard falls back to copy-paste for that version.
