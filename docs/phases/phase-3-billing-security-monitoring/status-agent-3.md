# Phase 3 — Agent 3 (Accounting & Monitoring) status

**Done. Build + vet + `go test ./internal/monitorsvc/...` (and accounting/live) green; Agent-3 gate legs all PASS.**

Delivered (FR-32/34/35/36/60, contracts C1-C/C5):
- **Migrations 0230–0234**: `health_probes` hypertable (nas XOR device target, one-target constraint), `alert_rules` (frozen types incl. `device_down|device_up`, default rules seeded), `alert_events`, `monitored_devices` (FR-60, separate from `nas`), `online_samples` hypertable.
- **`internal/monitorsvc` + `cmd/hikrad-monitor`**: probe engine (ICMP 15 s via system `ping`, SNMP 60 s via a dependency-free v2c GET), pure 4-miss up/down state machine driving both NAS and device targets; NAS recovery publishes `nas.recovered` + flags missing Stops (reaper still closes). Alerts engine (in-app/Telegram/SMTP/WhatsApp, per-rule cooldown, Asia/Baghdad quiet hours that never suppress in-app, concurrent failure-isolated dispatch, `alert_events` rows). Self-monitoring `GET /api/v1/health` (freeradius/api/db/redis, queue depth+drain+FR-40 invariant+enforcement failures, disk/volume). `GET /api/v1/dashboard` (online-now, 24 h sparkline, subs, revenue via `revenue_daily`, NAS cards, rps, pipeline). Device CRUD, per-NAS/device probe-history, alert-rule/event APIs, `live/notifications` SSE.

Cross-boundary touches (flagged): `cmd/hikrad-api/modules.go` (one blank-import mount line — the sanctioned mechanism), `deploy/compose.yml` (`hikrad-monitor` block, pre-agreed) + `deploy/docker/monitor.Dockerfile`, `go.mod` (`x/sys` → direct, used by disk stats), `scripts/gate-phase-3.sh` (my legs appended).

Notes / degradations: SNMP v2c-only (documented); ICMP shells out to `ping` (container has `NET_RAW`/iputils) behind a `Pinger` interface so the machinery is unit-tested with no network. `revenue_daily` (D) and `manager_balances` (agent_balance_low) are read defensively — absent source degrades to 0/skip, never a 500. Physical/UX gate legs 6 (unplug→Telegram<60 s) and 8 (device unreachable→`device_down`) are human-run; their scriptable parts pass.
