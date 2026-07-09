# HikRAD — Sub-PRD 03: Lossless Accounting, Live Data & Monitoring

> Derived from [docs/PRD.md](../PRD.md) v1.1 on 2026-07-08 (updated 2026-07-09: FR-55 added, FR-36 gains the WhatsApp channel — Decision 16). Owns: FR-31, FR-32, FR-33, FR-34, FR-35, FR-36, FR-37, FR-38, FR-39, FR-40, FR-55 · NFR-2 · Risks: lossless-pipeline complexity, competing with entrenched SAS4
> Depends on: [01-platform-install-licensing](01-platform-install-licensing.md) (Compose volumes for disk-backed queue, settings for retention/SMTP/Telegram/WhatsApp), [02-radius-nas-aaa](02-radius-nas-aaa.md) (accounting packet feed, CoA service, NAS registry), [05-billing-payments-vouchers](05-billing-payments-vouchers.md) (renewal/payment events for FR-55 receipts) · Depended on by: [04-subscribers-profiles](04-subscribers-profiles.md) (usage graphs on user page, quota counters), [05-billing-payments-vouchers](05-billing-payments-vouchers.md) (low-agent-balance alerts), [08-reports](08-reports.md) (usage report data)

## 1. Scope & context

This module is HikRAD's deliberate specialty and market wedge: **monitoring and live data**. It owns the lossless accounting pipeline (never drop a usage record — success metric **M2**), the Live Sessions experience (≤ 2 s packet-to-screen — metric **M3**: operators answer "why is customer X offline?" here, not via SSH), usage history and graphs, NAS and system health monitoring, and the alerts engine. Services involved: `hikrad-acct` (ingest + durable queue), `hikrad-monitor` (probes + alerts), Redis (live-session state, queue fast layer), TimescaleDB (hypertables + rollups).

**Accounting path (master §8):** FreeRADIUS forwards accounting to `hikrad-acct`, which acks only after appending to a Redis stream backed by disk spill-over; consumers upsert sessions and insert usage points into Timescale hypertables; idempotency key = (nas_id, acct_session_id, record_type, event_time). Audit counters at every stage.

## 2. Owned requirements — elaborated

### FR-37 (M) — Lossless accounting pipeline
**Master:** Every Accounting-Request (Start/Interim/Stop) is acknowledged only after being durably enqueued; a consumer writes to the DB; if the DB is down, the queue buffers to disk and drains on recovery; duplicates (NAS retransmits) are idempotently deduplicated by (NAS, Acct-Session-Id, record type, event timestamp).

*Elaboration:*
- **FR-37.1** — Ack contract: `hikrad-acct` returns the RADIUS Accounting-Response only after the record is appended to the Redis stream **and** Redis has persisted it (AOF `appendfsync everysec` minimum; measure against NFR-1 ingest targets). If Redis is unavailable, records append to a local disk WAL (spill file) and are replayed into the stream on recovery — the NAS retransmit behavior covers the sub-second ack gap.
- **FR-37.2** — Consumer group reads the stream → upserts `sessions` (Start creates, Interim updates counters/last-seen, Stop closes with terminate cause) → inserts `usage_points` (delta-computed from interim counters, handling 32-bit counter wrap and NAS counter resets) → acks the stream entry only after DB commit.
- **FR-37.3** — DB-down behavior: consumer stops acking, stream grows, spills to disk past a memory threshold; on DB recovery it drains in order. Queue depth and drain rate are first-class metrics (FR-35/FR-40).
- **FR-37.4** — Dedup: unique index on (nas_id, acct_session_id, record_type, event_time); duplicate inserts are counted (FR-40) and dropped idempotently. Out-of-order interims are tolerated (usage points keyed by event time).
- **FR-37.5** — Chaos tests in CI (per master risk table): kill the DB mid-flood, kill `hikrad-acct` mid-flood, unclean host restart — zero records lost, counters prove it (uses the NFR-8 packet harness).

### FR-38 (M) — Stale-session reaper
**Master:** Sessions with missed interims are marked stale, then closed with a synthesized Stop after a configurable timeout, flagged as "reaped" (never silently deleted).

*Elaboration:*
- **FR-38.1** — A session is **stale** after missing 2 consecutive expected interims (interval known per NAS from [02](02-radius-nas-aaa.md) FR-14.2); stale sessions render dimmed in Live Sessions, not removed (key flow 3 step 3).
- **FR-38.2** — After the reap timeout (default 3× interim interval + 5 min; configurable via settings), a synthesized Stop closes the session with `reaped=true` and usage as of last interim. If a real Stop or interim later arrives, it supersedes/reopens correctly and the event is counted.
- **FR-38.3** — NAS-recovery reconciliation (key flow 3 step 4): when a NAS returns from down, flag its sessions with missing Stops for the reconciliation pass.

### FR-40 (M) — Pipeline audit counters
**Master:** received/enqueued/persisted/deduplicated totals exposed on the health page, proving M2 (zero loss) at any moment.

*Elaboration:* monotonic counters per stage (received, acked, enqueued, spilled, drained, persisted, deduplicated, reaped) persisted across restarts; the health page shows the invariant check **received − duplicates − in-queue = persisted** with a green/red state. Counter mismatch raises an alert (FR-36).

### FR-31 (M) — Live Sessions table
**Master:** Every online session with username, NAS, IP, MAC, uptime, live down/up rate (computed from interim deltas), total session usage; auto-refreshing (≤ 2 s latency from packet to screen); filter by NAS/profile/manager; actions: disconnect (CoA), open user.

*Elaboration:*
- **FR-31.1** — Redis holds the live-session hash (keyed by nas_id + acct_session_id); the consumer updates it in the same pass as FR-37.2; panel subscribes via WebSocket/SSE (`/api/v1/live/sessions`) per [01](01-platform-install-licensing.md) FR-52.4.
- **FR-31.2** — Live rate = counter delta ÷ interim interval, labeled as averaged over the interval (honest UI — not instantaneous). Stale sessions show the stale badge instead of a rate.
- **FR-31.3** — Manager scoping applies: a scoped manager ([06](06-managers-roles-security.md) FR-27) sees only their own users' sessions. Disconnect action requires the `disconnect` permission and calls [02](02-radius-nas-aaa.md)'s CoA service.

### FR-32 (M) — Dashboard
**Master:** online-users count (now + 24 h sparkline), total active/expired/expiring-soon subscribers, today's revenue, per-NAS health cards, RADIUS requests/sec, accounting-pipeline status.

*Elaboration:* one-screen answer for **Omar**; all tiles link to their module's detail page (revenue → ledger [05](05-billing-payments-vouchers.md), subscriber counts → filtered lists [04](04-subscribers-profiles.md)). Refresh ≤ 10 s for tiles, ≤ 2 s for the online count. Fully usable on a phone.

### FR-33 (M) — Usage graphs & retention
**Master:** Per-user daily and monthly download/upload, session timeline; per-NAS and whole-network aggregate traffic graphs. Retention: raw sessions ≥ 12 months, aggregated daily rollups ≥ 3 years (configurable).

*Elaboration:* `usage_points` hypertable with Timescale continuous aggregates → `usage_daily` (per subscriber, per NAS, network-wide); compression on chunks older than 30 days; retention jobs read settings ([01](01-platform-install-licensing.md) FR-53.2) and must respect the ≥ floors. Sizing math for NFR-3's 200 GB documented here (2,000 sessions × 12/hr interims ≈ 210M points/yr pre-compression). Graph API powers the user page ([04](04-subscribers-profiles.md) FR-3) and usage reports ([08](08-reports.md) FR-47).

### FR-34 (M) — NAS health monitoring
**Master:** ICMP probe (latency/loss) always; SNMP (CPU, memory, uptime, port traffic) when community configured; per-NAS status page with probe history.

*Elaboration:* `hikrad-monitor` probes every NAS (interval default 15 s ICMP, 60 s SNMP); N consecutive misses (default 4) = down → alert (key flow 3); recovery fires all-clear + triggers FR-38.3 reconciliation. Probe results stored in the `health_probes` hypertable; per-NAS page shows latency/loss/SNMP history and current status. Status feeds the NAS cards ([02](02-radius-nas-aaa.md) UX) and dashboard.

### FR-35 (M) — System self-monitoring
**Master:** FreeRADIUS up/throughput/auth-reject rate, backend API health, DB health, queue depth of the accounting pipeline, disk space — all on an admin health page.

*Elaboration:* FreeRADIUS watched via Status-Server + process check; auth-reject rate computed from authorize outcomes; disk space per volume (data, backups, queue spill). The health page also shows license state ([01](01-platform-install-licensing.md) FR-50.5), backup age, and the FR-40 counter invariant. Everything on it is alertable via FR-36.

### FR-36 (M) — Alerts engine
**Master:** Rules for NAS down/up, RADIUS failure spike, accounting-queue backlog, low disk, user-expiring-in-N-days digest, low agent balance; channels: in-app, Telegram bot, email (SMTP), WhatsApp (Business Cloud API); per-rule routing and quiet hours.

*Elaboration:*
- **FR-36.1** — Rule = condition (typed, from the master's list) + threshold + routing (channel set, recipients) + quiet hours + cooldown (no re-fire storm). Delivery target ≤ 60 s from condition (user story: Telegram alert "within a minute").
- **FR-36.2** — Channels: in-app notification center + banner; Telegram bot (token/chats from settings); SMTP email; WhatsApp Business Cloud API (access token / phone-number ID from settings, [01](01-platform-install-licensing.md) FR-53.2 — admin-alert recipients are WhatsApp numbers configured per rule; business-initiated messages use pre-approved templates, see FR-55.1). Channel failures (no internet — NFR-7) are logged and retried without affecting anything else; in-app always works.
- **FR-36.3** — Digest-type rules (expiring-in-N-days, daily business digest per Omar's user story — new users/renewals/revenue/expiring) render as a single scheduled message; revenue/renewal figures come from [05](05-billing-payments-vouchers.md) ledger and [04](04-subscribers-profiles.md) counts. Low-agent-balance condition reads manager balances ([05](05-billing-payments-vouchers.md) FR-20).
- **FR-36.4** — Every fire recorded as an `alert_events` row (rule, condition snapshot, channels attempted, delivery result).

### FR-55 (S) — Subscriber-facing WhatsApp messaging
**Master:** Expiry reminders (N days before expiry) and payment receipts delivered to the subscriber's WhatsApp number using Meta-approved template messages in the subscriber's language; per-subscriber opt-in (phone consent); reuses the FR-36 delivery infrastructure (routing, quiet hours, retry, delivery isolation); internet-dependent, so it queues/skips gracefully per NFR-7.

*Elaboration:*
- **FR-55.1** — Business-initiated WhatsApp messages require **pre-approved Meta message templates**. v1 templates: `expiry_reminder` (params: subscriber name, days left, profile name) and `payment_receipt` (params: amount IQD, receipt number, new expiry date), each registered in ar/ku/en; template names + language mapping configurable in settings ([01](01-platform-install-licensing.md) FR-53.2).
- **FR-55.2** — Triggers: the expiry reminder extends the `expiring_digest` rule machinery with per-subscriber targeting (each subscriber's own expiry, N days, their own language); the receipt sends on renewal/payment events published by [05](05-billing-payments-vouchers.md) (FR-19/FR-21/FR-23) — money logic stays there, delivery lives here.
- **FR-55.3** — Consent & identity: sent only to subscribers with a valid normalized phone ([04](04-subscribers-profiles.md) FR-1.3) and the per-subscriber WhatsApp opt-in flag set ([04](04-subscribers-profiles.md) owns the field); language follows the subscriber's preference ([07](07-subscriber-portal-pwa.md)).
- **FR-55.4** — Delivery isolation identical to FR-36.2: failures are logged and retried, never block alerts or anything else; every send recorded like an alert event. Meta's per-conversation pricing is documented for the ISP in the admin guide.

### FR-39 (S) — RADIUS debug tool
**Master:** Live tail of auth attempts for a given username/NAS with human-readable reject reasons (bad password, expired, session limit, unknown NAS…).

*Elaboration:* the authorize endpoint ([02](02-radius-nas-aaa.md)) emits a structured decision event (matched subscriber, checks evaluated, failing check, reply summary) to a capped stream; the panel tails it filtered by username/NAS over the live WebSocket. Reasons are the enumerated set from key flow 1's checks, rendered in operator language and localized.

### NFR-2 (owned) — Reliability
**Master:** RADIUS auth keeps working if the web panel is down; accounting is never lost across restarts of any single component (queue is disk-backed); target service availability 99.9% on a single server; unclean shutdown must not corrupt data.

*Elaboration:* auth path has no dependency on panel/portal processes (verify by killing them in the chaos suite); each component restarts independently under Compose restart policies; queue durability per FR-37.1; unclean-shutdown safety relies on Postgres WAL + Redis AOF + spill-file fsync discipline. 99.9% availability = design constraint (no single scheduled operation, incl. updates per [01](01-platform-install-licensing.md) FR-51.4, takes auth down for more than seconds).

## 3. Acceptance criteria

- **AC-37a** — Given a 50 pkt/s accounting flood (harness), when Postgres is killed for 10 minutes mid-flood, then zero records are lost after recovery and FR-40 counters prove the invariant.
- **AC-37b** — Given a NAS retransmitting each packet 3×, then persisted usage equals single-delivery usage and the dedup counter equals the retransmit count.
- **AC-38a** — Given a NAS that goes silent, then its sessions turn stale after 2 missed interims, are closed as `reaped` after the timeout, and appear in history flagged — never deleted.
- **AC-31a** — Given an Accounting-Start, then the session row appears in Live Sessions within 2 s; given a Stop, it leaves within 2 s (key flow 1 step 5).
- **AC-31b** — Given a scoped manager, then Live Sessions shows only their users, and Disconnect is hidden without the permission.
- **AC-34a** — Given a NAS that stops answering pings, then its card turns red with downtime duration and a Telegram alert arrives within 60 s of the Nth missed probe (key flow 3).
- **AC-36a** — Given quiet hours 23:00–07:00 on a rule, then conditions during that window produce in-app events but suppress Telegram/email/WhatsApp until the window ends.
- **AC-55a** — Given a subscriber with a valid phone, WhatsApp opt-in, and language ar, when their expiry crosses the reminder threshold or a renewal completes, then the matching Arabic template message arrives at their number; with opt-in off or the internet down, nothing else is affected and the skip/queue is logged.
- **AC-33a** — Given 13 months of data, then daily graphs still render from rollups, raw sessions ≥ 12 months are intact, and retention jobs have not violated configured floors.
- **AC-NFR2a** — Given the panel container stopped, then PPPoE auth and accounting continue unaffected.

## 4. Data & interfaces

**Owned entities:** `sessions` (nas_id, acct_session_id, subscriber_id, ip, mac, start/stop, terminate_cause, bytes_in/out, stale/reaped flags), `usage_points` (hypertable), `usage_daily` (continuous aggregate), `health_probes` (hypertable), `alert_rules`, `alert_events`, `pipeline_counters`.

**Exposes:**
- `GET /api/v1/live/sessions` (WS/SSE + REST snapshot, filterable), `GET /api/v1/live/debug` (FR-39 tail)
- `GET /api/v1/usage/{subscriber|nas|network}?granularity=…` — graph data for [04](04-subscribers-profiles.md) and [08](08-reports.md)
- `GET /api/v1/health` (FR-35 page data), `GET /api/v1/dashboard` (FR-32 tiles)
- `GET/POST /api/v1/alert-rules`, `GET /api/v1/alert-events`
- Internal: `POST` accounting ingest from FreeRADIUS to `hikrad-acct` (transport configured by [02](02-radius-nas-aaa.md)).

**Consumes:** CoA service and NAS registry from [02](02-radius-nas-aaa.md); settings (retention, SMTP, Telegram, WhatsApp credentials/templates, quiet-hour defaults) from [01](01-platform-install-licensing.md); revenue/balance figures and renewal/payment events (FR-55 receipts) from [05](05-billing-payments-vouchers.md); subscriber counts/expiry queries + phone/WhatsApp-opt-in/language fields from [04](04-subscribers-profiles.md); permission checks from [06](06-managers-roles-security.md).

## 5. UX notes

Live Sessions: dense but scannable table, sub-second perceived updates (no full-page refresh), row action menu (Disconnect, Open user), stale rows dimmed with tooltip. Dashboard: card grid that reflows to single column on phones; sparklines and NAS cards colorblind-safe (state conveyed by icon + text, not color alone). Charts render LTR inside RTL layouts (NFR-6, [07](07-subscriber-portal-pwa.md)). Empty states matter: a fresh install shows "waiting for first accounting packet" with a link to the NAS wizard.

## 6. Out of scope

- NAS CRUD/wizard/CoA mechanics → [02](02-radius-nas-aaa.md).
- Subscriber/profile business rules and the user detail page composition → [04](04-subscribers-profiles.md) (this module supplies its live widget + graphs).
- Revenue math → [05](05-billing-payments-vouchers.md); report layouts → [08](08-reports.md).
- **Deferred by master:** nothing in this domain is deferred — monitoring is all-in for v1 (Decision 9).

## 7. Risks & open questions (owned)

- **Risk (master): Lossless pipeline complexity (dedupe, spill, reconcile).** Likelihood Medium / Impact High. Mitigation: build in P2 (early, not bolted on); audit counters (FR-40) verify continuously; chaos tests (kill DB mid-flood). *Elaboration:* the chaos suite (FR-37.5) is a P2 deliverable, not an afterthought; the counter invariant is the definition of done for M2.
- **Risk (master): Competing head-on with entrenched SAS4.** Likelihood Medium / Impact High. Mitigation: wedge = monitoring/lossless data + modern UX + SAS4 CSV import (import itself → [04](04-subscribers-profiles.md) FR-6). This module carries the wedge: M2 and M3 are its scoreboard.
- **NEW:** Redis AOF `everysec` admits a ≤ 1 s durability window on hard power loss; decide whether the ack path needs the disk WAL in front of Redis by default, or only as failover — measure both against NFR-1 in P2.
- **NEW:** live-rate accuracy depends on interim interval (see [02](02-radius-nas-aaa.md) NEW question) — UI must label the averaging window to keep operator trust.
- **NEW (FR-55):** WhatsApp Business Cloud API requires a Meta business account, a verified WhatsApp Business phone number, and per-template pre-approval — real lead time per ISP. Document the onboarding steps in the admin guide; in-app + Telegram remain the primary alert channels (consistent with the NFR-7 posture), so WhatsApp being unavailable never blocks anything.
