# Phase 2 — AAA Core & Lossless Pipeline

> Goal: a **real** subscriber (from the DB, with profile, expiry, MAC lock, session limit) authenticates through the full policy engine; every accounting packet is durably captured (zero loss, provable); operators watch sessions live in the panel and manage NAS devices and users. Master phases P1(rest)+P2. Requires Phase 1 gate green.

## Agent roster & path ownership (verified disjoint)

| Agent | Task file | Exclusive paths this phase |
|---|---|---|
| A — Platform & Security | [agent-1-platform-security.md](agent-1-platform-security.md) | `backend/internal/auth/**`, `backend/internal/platform/crypto/**`, migrations `0110–0119` |
| B — RADIUS & NAS | [agent-2-radius-nas.md](agent-2-radius-nas.md) | `backend/internal/radius/**`, `deploy/freeradius/**`, `backend/test/harness/**`, migrations `0120–0129` |
| C — Accounting & Monitoring | [agent-3-accounting-monitoring.md](agent-3-accounting-monitoring.md) | `backend/cmd/hikrad-acct/**`, `backend/internal/accounting/**`, `backend/internal/live/**`, migrations `0130–0139`, `deploy/compose.yml` **only** to enable the pre-agreed `hikrad-acct` block |
| D — Backend Business | [agent-4-backend-business.md](agent-4-backend-business.md) | `backend/internal/subscribers/**`, `backend/internal/profiles/**`, `backend/internal/seed/**`, migrations `0100–0109` |
| E — Frontend Panel | [agent-5-frontend-panel.md](agent-5-frontend-panel.md) | `frontend/panel/**` |

## Frozen contracts

### C1. Schema additions (by migration range owner)
- **D 0100–0109:** subscribers += address, notes, owner_manager_id, mac_lock_mode (`off|learn|fixed`), learned_mac, static_ip, session_limit_override, rate_override, price_override, disabled_reason; profiles += pool_id, session_limit_default, quota_mode (`unlimited|total|split`), quota_total_bytes, quota_down/up_bytes, throttle_rate, expiry_behavior (`block|expired_pool`), quota_behavior (`block|throttle|expired_pool`), archived; `audit-relevant` updated_at triggers.
- **A 0110–0119:** managers += totp fields (Phase 3 uses), scoped bool, `panel_sessions` (id, manager_id, refresh_hash, ua, ip, created, last_seen, revoked), `audit_log` (append-only: id, actor_id, action, entity_type, entity_id, before jsonb, after jsonb, ip, at) with REVOKE UPDATE/DELETE.
- **B 0120–0129:** `nas` (per sub-PRD 02 §4: name, ip unique, secret_enc, type, vendor default 'mikrotik', coa_port default 3799, snmp_community_enc, ros_version, location, enabled), `ip_pools` (name, ranges inet[], purpose `active|expired|static`), `pool_assignments`.
- **C 0130–0139:** `sessions` (nas_id, acct_session_id, subscriber_id, username, ip, mac, started_at, stopped_at, terminate_cause, bytes_in/out, packets, stale bool, reaped bool; unique (nas_id, acct_session_id)), `usage_points` hypertable (time, subscriber_id, nas_id, delta_down, delta_up, exempt bool), `usage_daily` continuous aggregate, `acct_dedup` (nas_id, acct_session_id, record_type, event_time, unique all four), `pipeline_counters`.

### C2. Auth middleware (A exposes; all API modules adopt)
`auth.Manager` in request context; `auth.Require("subscribers.view")`-style permission strings (`<module>.<verb>` + action perms `renew|disconnect|topup|export`); `auth.ScopeFilter(ctx) *ManagerScope` returning nil (unscoped) or manager ID — every list/get/mutation on subscriber-owned data must apply it. Real `POST /api/v1/auth/login` (+ `/refresh`, `/logout`) replaces the Phase-1 stub **keeping the C7/Phase-1 response shape**. Permission model this phase: role string `admin|operator|agent` with hardcoded permission sets (full matrix editor is Phase 3); contract: code checks permission strings, never role names.
`auth.Audit(ctx, action, entityType, entityID, before, after any)` write API — every mutating endpoint calls it.

### C3. Crypto service (A exposes)
`crypto.Encrypt/Decrypt([]byte) ([]byte, error)` (AES-GCM, key = `HIKRAD_ENCRYPTION_KEY`). D re-points subscriber password sealing to it (removing the Phase-1 temporary helper); B uses it for NAS secrets/SNMP communities. Subscriber password decryption is called **only** from B's authorize path.

### C4. Auth read-model (D exposes to B) — Go interface, in-process
```go
type AuthView struct { SubscriberID uuid; PasswordEnc []byte; Status string; ExpiresAt time.Time
  ExpiryBehavior, QuotaBehavior string; QuotaExhausted bool; RateLimit string; PoolName string
  ExpiredPoolName string; SessionLimit int; MacLockMode string; LearnedMac string; ThrottleRate string }
func GetAuthView(ctx, username) (AuthView, error)  // Redis-cached, ~1 ms hot path
func LearnMac(ctx, subscriberID, mac string) error
```
Cache key `auth:view:<username>`; **`radius.InvalidatePolicy(subscriberID)`** (B exposes) deletes it — D calls it on every subscriber/profile mutation. B counts live sessions for the limit check via C's C6 interface.

### C5. CoA service (B exposes; Go interface)
`coa.Disconnect(ctx, SessionRef)`, `coa.ApplyRate(ctx, SessionRef, rate string)`, `coa.MovePool(ctx, SessionRef, pool string)`; `SessionRef{NASID, AcctSessionID, Username, FramedIP}`; result recorded (ACK/NAK/timeout) + audited. Consumers this phase: E's disconnect button (via C's live API calling B); Phase 3 renewals.

### C6. Accounting ingest & live state (C owns)
- FreeRADIUS (B's config) forwards accounting via `rlm_rest` `POST http://hikrad-acct:8082/acct` `{record_type:"start|interim|stop", nas_ip, acct_session_id, username, framed_ip, calling_station_id, session_time, bytes_in/out (+gigawords), event_time, terminate_cause?}`; hikrad-acct replies 204 **only after durable enqueue** (Redis stream `acct:stream` + disk spill `data/acct-spill/`).
- Live-session Redis hash `live:sessions` (field = `nasID:acctSessionID`, value JSON: username, subscriber_id, nas_id, ip, mac, started_at, last_interim_at, bytes_in/out, rate_down/up_bps, stale) — read interface `live.Count(subscriberID) int` (B's session-limit check), `live.List(filter)`.
- Panel feed: `GET /api/v1/live/sessions` — SSE; events `snapshot` then `upsert`/`remove` with the hash JSON; filter params nas_id/profile_id/manager_id/q; scoped per C2.

### C7. REST surface for the panel (shapes fixed now)
- D: `GET/POST/PUT/DELETE /api/v1/subscribers` (+`GET /api/v1/subscribers/{id}`, `POST /{id}/reset-mac`, `POST /api/v1/subscribers/bulk` {filter, action, params}, `GET /api/v1/search?q=` → `[{type:"subscriber", id, username, name, phone, status}]`), `GET/POST/PUT /api/v1/profiles` (+archive). Subscriber detail embeds: profile summary, owner, live flag.
- B: `GET/POST/PUT/DELETE /api/v1/nas` (+`GET /{id}/config-snippet?ros=6|7`), `GET/POST/PUT/DELETE /api/v1/pools` (+utilization % in list).
- C: `GET /api/v1/usage/subscriber/{id}?granularity=daily|monthly&from&to` → `[{t, down, up}]`; `GET /api/v1/sessions?subscriber_id=` (history, paginated).

### C8. Enforcement hooks deferred
Quota/expiry **enforcement** (CoA on crossing) is Phase 3 (B). This phase: auth-time behavior only (authorize consults AuthView). C evaluates `QuotaExhausted` on interim processing and writes it to a Redis key D's AuthView reads — key `quota:exhausted:<subscriber_id>` (bool, C writes, D reads) — frozen here.

## Cross-assignments (deliberate)
FR-31/33: backend C, UI E. FR-13–16: backend B, UI E. FR-1–5: backend D, UI E. FR-5 MAC learn: rule D, invocation B (authorize). NFR-1 auth-latency: B (with D's cache); ingest: C.

## Integration gate
1. Harness: subscriber created via API authenticates (PAP+CHAP); disabled/expired/wrong-MAC/over-session-limit each reject with correct reason; expired user with `expired_pool` behavior gets Accept + expired pool attributes (key flow 1 fully).
2. Auth p99 < 100 ms at 50 req/s burst with 2k sessions loaded (harness load mode) — NFR-1 checkpoint.
3. Accounting flood (harness 50 pkt/s): kill Postgres 60 s mid-flood → zero loss after drain, counters invariant holds (received − dupes − queued = persisted); duplicate retransmits dedup'd. Session Start→visible in panel Live Sessions ≤ 2 s; Stop removes ≤ 2 s.
4. Reaper: silenced session goes stale → reaped with synthesized Stop, flagged, visible in history.
5. Panel: real login (Phase-1 stub gone), NAS CRUD + copy-paste snippet, user list/detail/search (`/` shortcut), Live Sessions with working Disconnect (CoA ACK verified against harness/real MikroTik).
6. All Phase-1 CI still green + new suites; audit_log rows exist for every mutation done during gate testing.
