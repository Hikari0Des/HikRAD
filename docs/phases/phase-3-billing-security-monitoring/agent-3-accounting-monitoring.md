# Phase 3 — Agent 3 (Accounting & Monitoring): health probes, alerts engine, dashboard, self-monitoring

> Owns FR-32, FR-34, FR-35, FR-36, FR-60 (backends). Depends on contracts in [00-phase.md](00-phase.md) (C1-C, C4, C5); parallel with Agents 1, 2, 4, 5.

## Mission & context
Monitoring is why ISPs switch to HikRAD (metric M3): a NAS problem must reach Omar's Telegram within a minute, and the dashboard must answer "is my network OK, is my business OK" at a glance. You build `hikrad-monitor` (ICMP/SNMP probes), the alerts engine with routing/quiet-hours/cooldown, system self-monitoring, and the dashboard API — on top of your Phase-2 pipeline. Detail source: sub-PRD [03-lossless-accounting-live-monitoring](../../prd/03-lossless-accounting-live-monitoring.md) FR-32/34/35/36.

## File ownership
- **Exclusive:** `backend/cmd/hikrad-monitor/**`, `backend/internal/monitorsvc/**`, `backend/internal/live/**`, `backend/internal/accounting/**`, `backend/migrations/0230_*.sql`–`0239_*.sql`, compose `hikrad-monitor` block (pre-agreed).
- **Read-only:** `nas` table (B's, probe targets + snmp_community via A's crypto Decrypt), settings (SMTP/Telegram/WhatsApp), ledger view `revenue_daily` (D's, frozen C5). **Forbidden:** `internal/{billing,auth,radius}` code, `frontend/**`.

## Tasks
1. Migrations 0230–0239 per phase C1-C: `health_probes` hypertable (nullable nas_id/device_id target columns), `alert_rules`, `alert_events`, `monitored_devices` (FR-60).
2. **Probes** (FR-34): ICMP every 15 s (latency/loss), SNMP every 60 s (CPU/mem/uptime/port traffic) when community set; per-NAS state machine — 4 consecutive ICMP misses → `down` event; recovery → `up` + publish `nas.recovered` (C5) + run the missing-Stops reconciliation pass over your sessions table (flag, don't synthesize — reaper handles closure). Probe history API for the per-NAS status page.
2b. **Monitored devices** (FR-60, amendment 2026-07-11): CRUD API per C5 (`/api/v1/devices`), then point the *same* probe scheduler/state machine at both target kinds — devices get ICMP always, SNMP when community set (encrypted via A's crypto), down/up events feed `device_down|device_up` rules, probe history shares the per-NAS history API shape. No new engine: devices are a second target list for task 2's machinery. Keep them fully out of NAS registry/paths.
3. **Self-monitoring** (FR-35): FreeRADIUS via Status-Server + reject-rate from `radius:decisions` stream (read-only consumer); api/db/redis health; queue depth + drain rate + FR-40 invariant from your Phase-2 counters (+ B's `enforcement_failures`); disk per volume. Compose into `GET /api/v1/health` per C5. [FR-35, FR-40 surfacing]
4. **Alerts engine** (FR-36): rule evaluation loop over the frozen rule types (C5); per-rule channel routing, quiet hours (Asia/Baghdad-aware), cooldown; channels — in-app (SSE `live/notifications` + persisted events), Telegram bot API, SMTP, WhatsApp Business Cloud API (creds + template names from settings; admin alerts use a generic approved alert template with text params; per-rule recipient numbers — sub-PRD 03 FR-36.2); delivery ≤ 60 s from condition; channel failures logged+retried without blocking others (NFR-7 — a dead WhatsApp endpoint never delays Telegram or in-app); every fire → `alert_events` row with delivery results. Digest rules (expiring-in-N, daily business digest with figures from `revenue_daily` + D's subscriber counts endpoint) render as single scheduled messages. Default rule set seeded (NAS down, disk 85%, backlog, invariant broken). Subscriber-facing WhatsApp (FR-55) is Phase 4 — build the sender reusable for it.
5. **Dashboard API** (FR-32) per C5: online-now + 24 h sparkline (downsampled from live-count samples you record each minute), subscriber tiles (D's counts), revenue today (`revenue_daily`), NAS cards (probe state), RADIUS rps, pipeline status. Tile latencies ≤ 10 s freshness; online count ≤ 2 s.
6. Per-NAS status page API: probe history graphs (latency/loss/SNMP series from the hypertable), current state, downtime log.

Edge cases: probe worker must not pile up when a NAS times out (bounded concurrency); SNMP v2c only in v1 (document); alert storm on multi-NAS outage → per-rule cooldown + grouped digest message; Telegram/SMTP unreachable (offline ISP) must never delay in-app events; quiet-hours boundary events fire once, not twice.

## Contracts consumed/exposed
- **Consumes:** nas registry (read-only), crypto Decrypt (A), settings channels config (A), `revenue_daily` + counts endpoints (D), `radius:decisions` + `enforcement_failures` (B).
- **Exposes:** C5 health/alert/dashboard APIs + notification SSE (E), `nas.recovered` (B), probe history (E's NAS status page).

## Definition of done
- Gate items 6 and 7 pass exactly (unplug → red card + Telegram < 60 s; stale-not-dropped; all-clear + reconciliation; quiet hours honored; dashboard/health correctness).
- Tests: probe state machine (flap, recovery), rule evaluation incl. cooldown/quiet-hours matrix, channel failure isolation, digest composition, sparkline downsampling, health invariant surfacing.

## Handoff
Phase 4 (same role) adds Web Push as a fourth channel + the expiring digest to portal push; Phase 5 turns your counters/health into the M2 release evidence. E ships dashboard/alerts/health UIs on these APIs this phase.
