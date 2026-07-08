# Phase 3 — Agent 2 (RADIUS & NAS): enforcement worker, Hotspot login template, debug stream

> Owns FR-9/FR-10 (runtime enforcement), FR-11 (sweeps), FR-18, FR-39 (backend). Depends on contracts in [00-phase.md](00-phase.md) (C4, C6); parallel with Agents 1, 3–5.

## Mission & context
Phase 2 made expiry/quota behaviors apply at **auth time**; this phase makes them apply to **already-online sessions**: an enforcement worker consuming quota/expiry events and executing the profile's configured CoA action within one cycle. Plus the branded MikroTik Hotspot login page with voucher login (persona: hotel/building operators), time-of-day sweeps, and the RADIUS debug tail backend that makes rejects self-explanatory for Ali. Detail sources: sub-PRDs [02](../../prd/02-radius-nas-aaa.md) FR-18, [04](../../prd/04-subscribers-profiles.md) FR-9–11, [03](../../prd/03-lossless-accounting-live-monitoring.md) FR-39.

## File ownership
- **Exclusive:** `backend/internal/radius/**`, `deploy/freeradius/**`, `backend/test/harness/**`, `backend/migrations/0220_*.sql`–`0229_*.sql`.
- **Read-only:** AuthView (C4-Phase-2), live list (C6-Phase-2), profile TOD fields (D's schema, read via AuthView extension frozen this phase in C4). **Forbidden:** `internal/{billing,monitorsvc,auth}` internals, `frontend/**`.

## Tasks
1. **Enforcement worker** per phase C4: subscribe `enforce.quota_exceeded` + `enforce.expired`; for each affected subscriber with live sessions (C6 list): resolve behavior via AuthView → execute CoA — quota: `block`→Disconnect, `throttle`→ApplyRate(throttle_rate), `expired_pool`→MovePool; expiry: `block`→Disconnect, `expired_pool`→MovePool + minimal rate. Idempotent (re-delivered events safe), cursor state in 0220 migration table, per-action audit entries, failure retries with backoff then alert-worthy log (C's rules pick it up via `acct_backlog`-style counter — expose `enforcement_failures` counter). [FR-9/10 runtime]
2. **Time-of-day sweeps** (FR-11): at profile window boundaries, ApplyRate boosted/normal rates to live sessions of affected profiles; publish `tod.window {profile_id, active}` for C's exemption marking; correct attributes at auth already handled via AuthView (verify + extend adapter for burst fields D added in 0200s — burst syntax in `Mikrotik-Rate-Limit`).
3. **Hotspot login template** (FR-18): served template package endpoint `GET /api/v1/nas/{id}/hotspot-package` → zip of MikroTik `login.html` + assets, themed from branding settings, with username/password form AND voucher-code field (voucher login flow: hotspot posts voucher as credentials → your authorize path detects voucher format → calls D's redeem API internally → accept on success); update the FR-14 snippet's walled-garden entries to cover the template's asset hosts.
4. **Debug stream** (FR-39) per phase C6: SSE endpoint over `radius:decisions` with username/NAS filters, human-readable reason keys (localization keys, E renders), permission-gated; capped stream retention config.
5. Harness: enforcement scenarios (quota crossing mid-session → observe CoA; expiry mid-session; TOD boundary), hotspot voucher login simulation.

Edge cases: subscriber with multiple live sessions (enforce on all); event for an offline subscriber (no-op, don't error); CoA NAK on throttle → fall back to Disconnect (per sub-PRD 02 FR-15.4) and record which path ran; voucher-format collision with real usernames (prefix discipline from D's C3 batch prefix — validate format before redeem attempt).

## Contracts consumed/exposed
- **Consumes:** C4 enforcement channels (C publishes quota, D publishes expiry), AuthView + TOD/burst field extension, C6 live list, D's internal redeem call (voucher login), branding settings (A).
- **Exposes:** `tod.window` events (C), `enforcement_failures` counter (C's health), debug SSE (E), hotspot package (E's NAS screen download button).

## Definition of done
- Gate item 4 passes (both crossings enforced ≤ 5 min, audited); gate item 7's debug-tool backend half.
- Hotspot template validated on a real MikroTik hotspot (or CHR) with a voucher login — document the manual verification.
- Tests: worker idempotency, behavior→CoA mapping matrix, multi-session enforcement, NAK fallback, TOD boundary math (Asia/Baghdad TZ), voucher-format detection.

## Handoff
Phase 4 (same role) hardens CoA across ROS versions with this worker as the main consumer; C's alert rules surface `enforcement_failures`; E renders debug reasons and ships the hotspot download button.
