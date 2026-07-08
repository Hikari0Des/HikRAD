# Phase 3 — Billing, Security & Monitoring (MVP gate)

> Goal: the master PRD's MVP (end of its P4): money flows (renewals with instant CoA restore, immutable ledger, agent balances, vouchers, receipts), the security module is complete (roles matrix, 2FA, sessions, audit viewer), and monitoring goes live (NAS/system health, alerts to Telegram, dashboard). Requires Phase 2 gate green (real AAA + lossless pipeline + operational panel).

## Agent roster & path ownership (verified disjoint)

| Agent | Task file | Exclusive paths this phase |
|---|---|---|
| A — Platform & Security | [agent-1-platform-security.md](agent-1-platform-security.md) | `backend/internal/auth/**`, migrations `0210–0219` |
| B — RADIUS & NAS | [agent-2-radius-nas.md](agent-2-radius-nas.md) | `backend/internal/radius/**`, `deploy/freeradius/**`, `backend/test/harness/**`, migrations `0220–0229` |
| C — Accounting & Monitoring | [agent-3-accounting-monitoring.md](agent-3-accounting-monitoring.md) | `backend/cmd/hikrad-monitor/**`, `backend/internal/monitorsvc/**`, `backend/internal/live/**`, `backend/internal/accounting/**`, migrations `0230–0239`, compose `hikrad-monitor` block |
| D — Backend Business | [agent-4-backend-business.md](agent-4-backend-business.md) | `backend/internal/billing/**`, `backend/internal/subscribers/**`, `backend/internal/profiles/**`, migrations `0200–0209` |
| E — Frontend Panel | [agent-5-frontend-panel.md](agent-5-frontend-panel.md) | `frontend/panel/**` |

## Frozen contracts

### C1. Schema additions
- **D 0200–0209:** `ledger_transactions` (append-only, REVOKE UPDATE/DELETE: id, at, type `renewal|topup|manual_payment|voucher_redeem|refund|adjustment|discount`, amount_iqd signed, actor_manager_id, subscriber_id?, source `panel|agent|voucher|portal-<gw>`, reference, reverses_id?, note), `payments` (receipt_no seq, method, ledger_tx_id), `voucher_batches` + `vouchers` (code_hash, state `unused|used|void`, used_by/for/at), profile burst + time-of-day rule fields (FR-11).
- **A 0210–0219:** `roles` + `role_permissions` + manager↔role, `ip_allowlists`, TOTP activation fields, backup codes (hashed).
- **B 0220–0229:** enforcement worker state table (last-processed cursors), hotspot template assets table if needed.
- **C 0230–0239:** `health_probes` hypertable (nas_id, at, kind `icmp|snmp`, latency_ms, loss, cpu, mem, uptime, ok), `alert_rules` (type, threshold jsonb, channels jsonb, quiet_hours, cooldown_s, enabled), `alert_events` (rule_id, at, state, payload, deliveries jsonb).

### C2. Renewal API (D) — the single money path (sub-PRD 05 FR-19.3)
`POST /api/v1/subscribers/{id}/renew {profile_id?: uuid, note?: string}` → `{ledger_tx_id, receipt_no, new_expires_at, coa_result: "restored|disconnect_fallback|failed|not_online"}`. Atomic: permission `renew` → balance check (agents hard-enforced; admins per settings) → ledger debit + payment → expiry extension per anchor setting → quota reset → status active → `InvalidatePolicy` → CoA restore via B (MovePool/ApplyRate; NAK→Disconnect fallback; result in response, never silent). Price resolution: user override → profile. Refund: `POST /{id}/refund {ledger_tx_id, reason}` (reversing entry, expiry rollback, FR-9 re-application via CoA).

### C3. Balance & voucher APIs (D)
`GET /api/v1/managers/{id}/balance` → `{balance_iqd}` (ledger-derived, cached); `POST /api/v1/managers/{id}/topup {amount_iqd, note}` (permission `topup`). `POST /api/v1/vouchers/batches {profile_id, count≤10000, prefix?, expires_at?}` → CSV download link (plaintext codes only at generation); `POST /api/v1/vouchers/redeem {code, subscriber_id}` (row-locked single use → C2 renewal, source=voucher). Voucher charging model decision is **frozen now: charge at generation** to the batch creator's balance (simplest agent settlement; revisit post-pilot — recorded deviation from sub-PRD 05's open question, decision made here to unblock parallel work).
Receipt: `GET /api/v1/payments/{receipt_no}/receipt?lang=ar|en` → print-ready HTML.

### C4. Enforcement events (C → B → D-rules)
Redis pub/sub channels frozen: `enforce.quota_exceeded {subscriber_id}` (C publishes on interim evaluation), `enforce.expired {subscriber_id}` (D's expiry sweep publishes). B's enforcement worker consumes both, reads behavior via AuthView, executes CoA (throttle/move-pool/disconnect) within one cycle (≤ 5 min), records outcome to audit. Time-of-day windows (FR-11): B runs boundary sweeps reading profile TOD fields (D's schema); usage exemption flag: B publishes `tod.window {profile_id, active}` → C marks `usage_points.exempt`.

### C5. Health, alerts & dashboard (C)
- `GET /api/v1/health` → freeradius {up, req_rate, reject_rate}, api, db, redis, queue {depth, drain_rate, invariant_ok, counters}, disk per volume, license placeholder (Phase 5).
- `GET/POST/PUT /api/v1/alert-rules`; rule types frozen: `nas_down|nas_up|radius_reject_spike|acct_backlog|disk_low|expiring_digest|agent_balance_low`; channels `inapp|telegram|email`. `GET /api/v1/alert-events` (paginated). In-app: SSE `GET /api/v1/live/notifications`.
- `GET /api/v1/dashboard` → `{online_now, online_24h_sparkline[], subs {active, expired, expiring_7d}, revenue_today_iqd, nas_cards[{id, name, status, latency_ms, downtime_s?}], radius_rps, pipeline {invariant_ok, depth}}` (revenue from D's ledger view `revenue_daily`, frozen read-only).
- NAS down/up detection: N=4 missed ICMP (15 s interval) → event; recovery → all-clear + publish `nas.recovered {nas_id}` (B may reconcile, C flags missing Stops).

### C6. Debug tool (B backend, E UI)
`GET /api/v1/live/debug?username=&nas_id=` — SSE tail of `radius:decisions` (Phase-2 stream): `{at, username, nas, outcome, reason, checks[]}`, permission-gated (`nas.view`).

### C7. Panel API surface for E
Everything above plus A's: `GET/PUT /api/v1/roles`, manager CRUD with role assignment + scoped flag + IP allowlist, `POST /api/v1/auth/totp/{enroll|verify|disable}`, backup codes, `GET /api/v1/audit-log` full filters, `GET/DELETE /api/v1/panel-sessions?manager_id=`.

## Cross-assignments (deliberate)
FR-19–22/24/25 backend D, UI E. FR-27–30 backend A, UI E. FR-32/34/35/36 backend C, UI E. FR-39 backend B, UI E. FR-9/10 runtime enforcement B (rules stay D's). FR-11 fields D, sweeps B, exemption marks C.

## Integration gate (≈ master MVP, M-metrics 2/3 testable)
1. Key flow 2 end to end: search → user page → Renew (pre-selected profile, resolved price) → agent balance debited, ledger entry written, expiry extended, **online expired user restored to full speed via CoA without redial**; printable Arabic receipt renders.
2. Insufficient agent balance blocks renewal; top-up unblocks; balance always equals ledger sum (property test).
3. Voucher batch of 100: generate → CSV → redeem one (operator path) → single-use enforced under concurrent double-redeem test.
4. Quota crossing while online → throttle CoA applied ≤ 5 min (or per-profile alternative); expiry crossing → expired-pool move; both visible in audit.
5. Security: role matrix editing takes effect immediately; TOTP enroll/login works; scoped agent API-verified isolation; audit viewer shows before/after for a subscriber edit; DB-level immutability tests green (ledger + audit).
6. Key flow 3: NAS unplugged → dashboard card red + Telegram alert < 60 s; sessions marked stale not dropped; recovery all-clear + reconciliation flags missing Stops. Quiet hours suppress Telegram but not in-app.
7. Dashboard tiles correct against seeded/derived data; health page shows pipeline invariant green; debug tool tails a live reject with human-readable reason.
