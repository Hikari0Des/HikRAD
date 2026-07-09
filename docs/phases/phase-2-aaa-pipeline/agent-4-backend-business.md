# Phase 2 — Agent 4 (Backend Business): subscribers & profiles domain, search, bulk, auth read-model

> Owns FR-1, FR-2 (backend), FR-3 (composition APIs), FR-4, FR-5 (rules), FR-7, FR-8, FR-9/10 (config), and the C4 read-model. Depends on contracts in [00-phase.md](00-phase.md) (C1-D, C2, C3, C4, C7-D, C8); parallel with Agents 1–3, 5.

## Mission & context
Fill in the business core: full subscriber CRUD with the fields Iraqi ISP operators live on, instant global search, bulk operations, per-user overrides, MAC-lock/session-limit rules, and profiles with expiry/quota behaviors — plus the **AuthView read-model** that agent B's policy engine consumes on every Access-Request (Redis-cached, ~1 ms). Every mutation invalidates B's cache and writes A's audit log. Detail source: sub-PRD [04-subscribers-profiles](../../prd/04-subscribers-profiles.md).

## File ownership
- **Exclusive:** `backend/internal/subscribers/**`, `backend/internal/profiles/**`, `backend/internal/seed/**`, `backend/migrations/0100_*.sql`–`0109_*.sql`.
- **Read-only:** `internal/auth` (use C2), `internal/radius` (call `InvalidatePolicy` only), `internal/live` (embed live flag via C6 read API). **Forbidden:** `internal/{accounting,radius}` internals, `deploy/**`, `frontend/**`.

## Tasks
1. Migrations 0100–0109 per phase C1-D (subscriber + profile columns, incl. the amended `allow_hotspot`/`whatsapp_opt_in` subscriber fields and `hotspot_rate_down/up_kbps` profile fields — FR-58/FR-55) **plus** the frozen read-only SQL view `subscriber_quota_view` (subscriber_id, quota_mode, quota bytes, cycle anchor) consumed by C. [FR-1, FR-8]
2. Subscribers module: full CRUD per C7-D — username immutable + citext-unique; password sealed via A's crypto (C3), reset-only (never returned); phone normalization (`07xx…` ↔ `+964…`); status transitions (active↔disabled manual + `disabled` offers CoA disconnect flag in response for E to act on; `expired` derived — auth-time authority + a ≤ 5-min sweep job aligning the column for lists); static IP validated through B's pool-uniqueness service; owner_manager_id + ScopeFilter applied on every query; every mutation → `auth.Audit` + `radius.InvalidatePolicy`. [FR-1, FR-5 fields]
3. `GET /api/v1/search?q=` — trigram/prefix indexes on username/name/phone; < 300 ms at 5k rows; scoped. [FR-2]
4. User-detail composition endpoint `GET /api/v1/subscribers/{id}` embedding: profile summary, override badges, live flag (C6), links/ids the panel needs to fetch usage (C7-C) and audit trail (A's endpoint) — the panel composes the page from these frozen pieces. [FR-3]
5. Bulk endpoint per C7-D: server-side filter execution (enable/disable, change profile, extend expiry, move owner, export CSV); async job with progress + per-row failure report; audit per affected row; batched `InvalidatePolicy`; export gated by `export` permission. [FR-4]
6. Per-user overrides (rate, price on renewal, session limit) with clear API semantics (null = inherit). [FR-7]
6b. FR-58/FR-55 fields in CRUD + bulk: `allow_hotspot` and `whatsapp_opt_in` editable on the subscriber (whatsapp_opt_in requires a valid phone), `allow_hotspot` also a bulk action; profile CRUD carries `hotspot_rate_down/up` (null = fall back to main rate). Both toggles invalidate policy like any auth-affecting mutation.
7. Profiles module: CRUD per C1-D fields incl. expiry_behavior/quota_behavior/throttle_rate/pool_id/session_limit_default; archive-not-delete when referenced; edit prompt semantics — `apply=now|next_renewal` param; `now` → bulk InvalidatePolicy + emit list of online affected subscriber refs in the response so E can offer CoA rate refresh (actual CoA call via B's service from the same response flow). [FR-8, FR-9, FR-10 config]
8. AuthView per C4 (incl. the amended `AllowHotspot`/`HotspotRateLimit` fields): single-query loader joining subscriber+profile+overrides+`quota:exhausted` flag (C8), sealed password passthrough, expired-pool name resolution; Redis cache with `LearnMac` write-through; benchmark hot path ≤ 1 ms, cold ≤ 20 ms. [C4, NFR-1 support]
9. Seed expansion: 200 realistic demo subscribers across 3 profiles (Arabic names, Iraqi phones), for gate testing and screenshots.

Edge cases: bulk change-profile must skip archived target profiles; disabling an expired user keeps `disabled_reason`; search with Arabic text (normalization: hamza/teh-marbuta variants); expiry sweep must not flap users renewed mid-sweep (compare against current row, not snapshot).

## Contracts consumed/exposed
- **Consumes:** C2 (`Require`, `ScopeFilter`, `Audit`), C3 crypto, C6 live flag, C8 quota flag, B's static-IP validation + `InvalidatePolicy`.
- **Exposes:** C4 AuthView (B's hot path), `subscriber_quota_view` (C), C7-D REST (E), seed fixtures (everyone's tests).

## Definition of done
- Gate item 1 depends on your AuthView correctness — every policy branch B tests is driven by data you serve; gate item 5's user-list/detail/search parts pass.
- Tests: CRUD validation matrix, phone normalization, search latency at 5k seeded rows, bulk async job with induced per-row failures, AuthView cache invalidation on each mutation type, override inheritance, profile archive rules, expiry-sweep idempotence.
- Zero mutations without Audit + InvalidatePolicy (A's lint script green).

## Handoff
Phase 3 (same role) builds renewals/ledger on these subscriber/profile semantics; B's enforcement worker reads behaviors you stored; E gets stable CRUD/search/bulk APIs.
