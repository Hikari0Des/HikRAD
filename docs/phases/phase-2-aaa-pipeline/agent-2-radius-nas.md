# Phase 2 — Agent 2 (RADIUS & NAS): full policy engine, NAS management, CoA, IP pools, vendor adapter

> Owns FR-13, FR-14, FR-15, FR-16, FR-17 complete; NFR-1 (auth latency). Depends on contracts in [00-phase.md](00-phase.md) (C1-B, C3, C4, C5, C6, C7-B); parallel with Agents 1, 3–5.

## Mission & context
Replace the Phase-1 stub with the real authorize policy engine (key flow 1 of the master PRD): credential check (PAP + CHAP against AES-GCM-sealed passwords), status/expiry with per-profile expiry behavior, quota-exhausted behavior, simultaneous-session limit, MAC lock with first-use learning — all under a 100 ms p99 budget using D's cached AuthView. Plus the NAS registry with RouterOS config wizard, CoA/Disconnect services, IP pool management, and the vendor-adapter layer that keeps the core MikroTik-free. Detail source: sub-PRD [02-radius-nas-aaa](../../prd/02-radius-nas-aaa.md).

## File ownership
- **Exclusive:** `backend/internal/radius/**`, `deploy/freeradius/**`, `backend/test/harness/**`, `backend/migrations/0120_*.sql`–`0129_*.sql`.
- **Read-only:** `internal/subscribers`/`profiles` via the C4 interface only; `internal/live` via C6 `live.Count`. **Forbidden:** direct SQL on D's or C's tables, `frontend/**`.

## Tasks
1. Migrations 0120–0129: `nas`, `ip_pools`, `pool_assignments` per phase C1-B; secrets sealed with A's crypto (C3). [FR-13, FR-16]
2. **Policy engine** replacing `stub_policy.go`: resolve AuthView (C4) → check in order: known NAS (else reject unknown_nas) → **service check (FR-58):** request `service=hotspot` for a PPPoE subscriber is allowed only when `AuthView.AllowHotspot` (else reject `service_not_allowed`) → credentials (PAP compare / CHAP verify using decrypted password — decryption only here, NFR-4.2) → status disabled → expiry (behavior `block` → reject expired; `expired_pool` → accept with expired-pool intents + minimal rate) → quota (per `QuotaBehavior`: block/throttle intents/expired pool — **skip the quota check for hotspot-service requests**, FR-58.3) → session limit via `live.Count(subscriberID, service)`: PPPoE counts against `SessionLimit`, hotspot allows exactly 1 concurrent hotspot session outside that limit (FR-58.2) → MAC lock (learn mode: absent → `LearnMac` callback + accept; mismatch → reject mac_mismatch; **MAC lock applies to PPPoE only** — hotspot devices are inherently different MACs). Hotspot accepts emit `rate_limit` = `HotspotRateLimit` (fallback `RateLimit`). Emit abstract intents; structured decision event per attempt (username, checks, outcome, reason — feeds FR-39 in Phase 3, write to capped Redis stream `radius:decisions` now). [FR-5 enforcement, FR-9, FR-10, FR-58, key flow 1]
3. **Vendor adapter layer:** `intents.go` abstract set (`rate_limit`, `address_pool`, `session_timeout`, `redirect_expired`); `vendor/mikrotik.go` maps to VSAs (`Mikrotik-Rate-Limit` incl. burst syntax slots for Phase 3, `Framed-Pool`, `Framed-IP-Address` for static IPs — precedence over pool). CI grep-test: no `Mikrotik-` literal outside `vendor/` + dictionaries. [FR-17]
4. **NAS module:** CRUD endpoints per C7-B; FreeRADIUS clients driven from DB (pick and implement: `rlm_sql` clients table OR config-regen + reload via a control socket — decide, document in `deploy/freeradius/README.md`, per sub-PRD 02 open question); delete-with-live-sessions confirmation flag; audit all mutations (A's C2). [FR-13]
5. **Config wizard backend:** `GET /api/v1/nas/{id}/config-snippet?ros=6|7` — templated RouterOS snippets per sub-PRD 02 FR-14.2 (radius client, interim interval, PPP/Hotspot AAA, CoA incoming, walled-garden basics for hotspot type); "seen since created" test endpoint (`GET /{id}/status` → last Access-Request/acct time from Redis). [FR-14]
5b. **NAS discovery** (FR-56.1): MNDP listener (UDP 5678) + operator-triggered IP-range scan probing the RouterOS API port; `POST /api/v1/nas/discover` per phase C7-B returning identity/ros_version/mac/ip deduped against registered NAS. Strictly read-only — never connects to or modifies a router (the API auto-setup with preview/apply is Phase 4, per sub-PRD 02 FR-56.2–56.4). Harness gains an MNDP-announce mode for the gate.
6. **CoA service** per C5: Disconnect/ApplyRate/MovePool via UDP to nas.coa_port with per-NAS secret; 5 s timeout + 1 retry; NAK/timeout → typed error so callers fall back (renewal falls back to Disconnect in Phase 3); every attempt audited. Harness gains a CoA-listener mode to assert packets. [FR-15]
7. **IP pools:** CRUD + utilization % (live sessions per pool via C6 list vs. range size), exhaustion state at 90% exposed in the list payload (alert wiring is Phase 3). Static-IP uniqueness validation service D calls. [FR-16]
8. Extend harness: scenario suite covering every reject reason + expired-pool accept + CHAP + static-IP + session-limit + FR-58 dual-service matrix (hotspot accept at PPPoE limit / second-hotspot reject / flag-off reject / hotspot rate fallback) (needs C's live hash primed — do it via real accounting Starts through C's ingest); load mode target for gate item 2.

Edge cases: subscriber with static IP AND pool → Framed-IP-Address wins; NAS with wrong secret → FreeRADIUS drops (log it); AuthView cache miss during Redis restart must still answer < 100 ms p99 at nominal load (DB fallback path benchmarked); CoA against NAS behind NAT documented as unsupported v1.

## Contracts consumed/exposed
- **Consumes:** C4 `GetAuthView`/`LearnMac` (D), C6 `live.Count` (C), C3 crypto (A), C2 auth middleware + Audit (A).
- **Exposes:** C5 CoA service (E-via-C this phase; D's renewals Phase 3), `radius.InvalidatePolicy(subscriberID)` (D calls on mutations), `radius:decisions` stream (Phase 3 FR-39), NAS/pool REST (E), accounting forward config pointing at C's ingest (C6).

## Definition of done
- Gate items 1 and 2 pass (full scenario matrix + p99 < 100 ms at 50 req/s with 2k live sessions).
- Gate item 5's NAS/CoA parts: CRUD + snippet generates valid ROS 6 & 7 configs (validated against a real MikroTik or ROS CHR in CI-adjacent manual step, documented); Disconnect ACK round-trip proven.
- Unit tests: every policy branch incl. attribute precedence; adapter mapping; CoA encode/timeout/NAK. Vendor-isolation grep-test green.

## Handoff
Phase 3 receives: CoA service for renewals (D) and the enforcement worker (B, next phase — your own base), decision stream for the debug tool, pool-exhaustion signal for alert rules, and a regression harness suite the enforcement work extends.
