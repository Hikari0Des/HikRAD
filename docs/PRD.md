# HikRAD — Product Requirements Document

> Version 1.0 — 2026-07-08 — Status: Draft (all decisions user-confirmed)
> Decomposed into domain sub-PRDs: see [docs/prd/00-index.md](prd/00-index.md)

## 1. Overview

HikRAD is a commercial RADIUS AAA + billing platform for ISPs, built first for the Iraqi market. It delivers the feature depth of Snono SAS4 Radius (users, profiles, NAS management, billing, reports) with a dramatically simpler, modern UI and easier UX, and goes beyond SAS4 in one deliberate specialty: **monitoring and live data** — real-time session visibility, NAS/system health, and a lossless accounting pipeline that never drops a usage record. It is sold as a one-time license per server and installed on-premise at each ISP via a Docker-based installer.

## 2. Problem Statement

Small and mid-size Iraqi ISPs (WISPs, building networks, regional providers) run their subscriber authentication and billing on SAS4 or ad-hoc MikroTik User Manager setups. The pain:

- **SAS4's UI/UX is dated and dense.** Staff need long onboarding; daily tasks (renew a user, find why someone is offline, credit an agent) take too many clicks across cluttered screens.
- **Monitoring is an afterthought.** Operators lack a trustworthy real-time picture of who is online, what NAS is struggling, and whether accounting data is complete. Missed accounting packets mean disputed bills and lost revenue.
- **Alternatives don't fit the market.** International billing platforms are priced in recurring USD subscriptions, lack Arabic/Kurdish RTL interfaces, ignore Iraqi payment channels (agent cash collection, ZainCash/FastPay/Qi), and assume reliable internet — a dealbreaker when RADIUS must keep working through upstream outages.

Today ISPs tolerate SAS4's UX because nothing else covers the same feature set for this market. HikRAD's opening: match the core feature set, beat it decisively on UX and monitoring, and sell on the licensing model Iraqi ISPs already prefer (one-time purchase).

## 3. Goals & Success Metrics

### Goals
- Ship a v1 an Iraqi ISP can run its entire prepaid subscriber business on: AAA, profiles, billing, vouchers, live monitoring.
- Make every daily operator task achievable in ≤ 3 clicks from the dashboard, with zero training beyond a one-page guide.
- Capture 100% of RADIUS accounting data, durably, even under NAS floods, DB restarts, or upstream outages.
- Be installable by a mid-level network tech on a clean Linux server in under 30 minutes.

### Non-goals (explicitly out of scope for v1)
- Prepaid card system with drag-and-drop card designer (Phase 2 — one-time *vouchers* are in v1; printable card batches/designer are not).
- Infinite reseller/sub-manager tree with balance transfer down the chain (Phase 2; v1 has flat managers with granular permissions).
- Native mobile apps. The "app" experience ships in v1 as installable PWAs of the portal and panel (FR-54); a native/TWA store app is only built later if Play Store presence proves necessary.
- Publicly documented third-party API (the API exists in v1 for our own frontends; public docs and stability guarantees come later).
- Cloud/SaaS multi-tenant hosting; non-MikroTik NAS certification; postpaid invoicing; FTTH/OLT management; CRM/ticketing.

### Success metrics
- **M1:** First pilot Iraqi ISP in production, running all subscribers on HikRAD for 30 consecutive days.
- **M2:** Zero lost accounting records at the pilot — every Accounting-Request received is durably stored (verified by the pipeline's own audit counters, FR-40).
- **M3:** Pilot operators use the live-monitoring dashboard daily (it is the answer to "why is customer X offline?", not SSH into the MikroTik).
- **M4:** Fresh install to first authenticated PPPoE user in < 30 minutes following the docs.

## 4. Target Users & Personas

| Persona | Role | Technical level | Context & primary need |
|---|---|---|---|
| **Omar — ISP owner/manager** | Buys the license, owns the business | Medium (networking yes, servers somewhat) | Wants total visibility: revenue, online users, NAS health, agent balances — at a glance, in Arabic, on any device. |
| **Sara — front-desk operator** | Renews users, activates vouchers, answers "why am I offline?" calls | Low | Needs to find any user in seconds, see their live session/usage, and renew or fix them in a few clicks. Trained in one hour. |
| **Ali — network engineer** | Installs HikRAD, adds NAS devices, defines profiles | High (MikroTik expert, Linux basics) | Needs the installer, NAS wizard with copy-paste MikroTik config, RADIUS debugging tools, and CoA that actually works. |
| **Hassan — field agent** | Collects cash in a neighborhood, credits subscriber accounts | Low (phone-first) | Needs a limited manager account: search his users, renew from his balance, see his own collection report — installed on his phone as the panel PWA (FR-54). |
| **Noor — subscriber** | End customer of the ISP | Low | Portal (Arabic/Kurdish/English) to check remaining days/quota, current speed, usage history, and renew via e-wallet or voucher code. |

## 5. User Stories & Key Flows

### User stories (by feature area)

**Authentication & sessions**
- As Ali, I want to add a MikroTik NAS and get a generated config snippet to paste into RouterOS, so that PPPoE auth works on the first try.
- As Sara, I want to see instantly whether a caller's session is online, its live speed, and last-disconnect reason, so that I can answer support calls without escalating.
- As Omar, I want expired users automatically moved to a "expired" pool/profile with a redirect page instead of hard-cut, so that they renew instead of calling.

**Billing & subscriptions**
- As Sara, I want to renew a user with one click (repeat last profile) or switch profiles, so that the counter line moves fast.
- As Hassan, I want renewals to deduct from my agent balance and appear in my collection report, so that my end-of-week settlement with the ISP is automatic.
- As Noor, I want to redeem a voucher code or pay with ZainCash from the portal, so that I renew at midnight without calling anyone.

**Monitoring & health**
- As Omar, I want a Telegram alert within a minute when a NAS goes unreachable or the RADIUS service degrades, so that I hear it from HikRAD before customers call.
- As Ali, I want per-user and per-NAS traffic history graphs with no gaps, so that usage disputes are settled by data.
- As Omar, I want a daily digest (new users, renewals, revenue, expiring soon) so I can steer the business from my phone.

### Key flow 1 — Subscriber authenticates (PPPoE)
1. Subscriber's router initiates PPPoE to the MikroTik NAS.
2. NAS sends Access-Request to HikRAD's FreeRADIUS.
3. FreeRADIUS queries the HikRAD backend: credentials valid? account active (not expired/disabled/over-quota)? simultaneous-session limit ok? MAC lock ok?
4. Backend returns Access-Accept with profile attributes (Mikrotik-Rate-Limit, Framed-Pool or Framed-IP-Address, session-timeout) — or Accept with the "expired" pool attributes if the account is expired and the ISP enabled the redirect-walled-garden option.
5. Accounting Start arrives → session appears in the Live Sessions table within 2 s; Interim-Updates stream usage; Stop closes the session record.

### Key flow 2 — Front-desk renewal
1. Sara types any fragment (username, name, phone) in the global search bar (visible on every page).
2. User page opens: status banner (online/offline, expiry, remaining quota), live session widget, last payments.
3. She clicks **Renew** → dialog pre-selects the user's current profile and price → confirm.
4. Backend: charges (records payment against Sara's manager account), extends expiry per profile rules, and if the user is currently online in the expired pool, fires CoA so full speed resumes immediately — no manual disconnect.
5. Receipt is printable/sendable; the transaction appears in reports in real time.

### Key flow 3 — NAS goes down
1. Health monitor's ping/SNMP probe misses N consecutive checks on a NAS.
2. Alert fires: in-app banner + Telegram/email per alert-routing settings.
3. Dashboard NAS card turns red with downtime duration; Live Sessions marks that NAS's sessions as stale rather than silently dropping them.
4. When the NAS returns, an all-clear notification fires and an interim-accounting reconciliation pass flags sessions with missing Stops.

## 6. Functional Requirements

Priorities: **M**ust / **S**hould / **C**ould / **W**on't (this release). All Musts constitute v1.

### 6.1 User (subscriber) management
- **FR-1 (M):** CRUD subscribers: username, password, name, phone, address, notes, owner (manager), profile, expiry, status (active/disabled/expired), MAC address, static IP (optional).
- **FR-2 (M):** Global instant search across username/name/phone from every screen.
- **FR-3 (M):** User detail page combines: live session state, usage graphs (daily/monthly), session history, payment history, audit log of changes.
- **FR-4 (M):** Bulk actions on filtered user lists: enable/disable, change profile, extend expiry, move owner, export CSV.
- **FR-5 (M):** Simultaneous-session limit and optional MAC-lock per user (auto-learn first MAC, one-click reset).
- **FR-6 (S):** CSV import wizard for migrating subscriber bases from SAS4/other systems (field mapping + dry-run report).
- **FR-7 (S):** Per-user overrides of profile attributes (custom rate limit, custom price on renewal).

### 6.2 Profiles (service plans)
- **FR-8 (M):** Profiles define: price, duration (days), download/up speed (Mikrotik-Rate-Limit), data quota (total, or separate down/up) or unlimited, IP pool, simultaneous-session default.
- **FR-9 (M):** Expiry behavior per profile: hard block, or move to "expired" pool (walled garden/redirect) via RADIUS attributes.
- **FR-10 (M):** Quota-exhausted behavior per profile: block, throttle to a configured speed, or move to expired pool.
- **FR-11 (S):** Burst settings (burst rate/threshold/time) and time-of-day rules (e.g., free night quota, off-peak speed boost).
- **FR-12 (C):** Profile change scheduling (upgrade applies at next renewal vs. immediately with proration).

### 6.3 NAS management (MikroTik PPPoE + Hotspot)
- **FR-13 (M):** CRUD NAS: name, IP, RADIUS secret, type (PPPoE/Hotspot), CoA port, SNMP community (optional), location note.
- **FR-14 (M):** Setup wizard generates a copy-paste RouterOS config (RADIUS client, PPPoE/Hotspot AAA settings, walled-garden basics for Hotspot).
- **FR-15 (M):** CoA/Disconnect-Message support against MikroTik: disconnect session, apply new rate limit without disconnect where supported.
- **FR-16 (M):** IP pool management: define pools, assign to profiles/NAS, view utilization %, warn on exhaustion.
- **FR-17 (M):** Vendor-neutral RADIUS core: MikroTik ships as the certified vendor via its dictionary/templates; architecture must not hard-code MikroTik so other vendor dictionaries can be added later (W for certifying other vendors in v1).
- **FR-18 (S):** Hotspot login page template (ISP logo/colors) served for MikroTik Hotspot with voucher-code login.

### 6.4 Billing, payments & vouchers
- **FR-19 (M):** Prepaid recurring model: renewal charges the profile price, extends expiry by profile duration (from expiry date if still active, from now if already expired — configurable).
- **FR-20 (M):** Manager balances: each manager/agent account holds a balance; renewals they perform deduct from it; admins top up agent balances; every movement is a ledger transaction.
- **FR-21 (M):** Manual payments: record cash payments with receipt number, printable receipt (Arabic/English templates).
- **FR-22 (M):** One-time vouchers: generate batches of codes (profile, count, prefix, expiry-of-code); redeemable by operators, via subscriber portal, or at Hotspot login; single-use; batch list shows used/unused; export CSV.
- **FR-23 (M):** Iraqi e-wallet payments from the subscriber portal via a **pluggable gateway interface**; v1 ships adapters for ZainCash, FastPay, and Qi (subset shippable per gateway merchant-account availability — see Open Questions), with webhook/callback verification and automatic renewal on confirmed payment.
- **FR-24 (M):** Full transaction ledger: immutable, filterable by manager/user/date/type, exportable; discounts and manual adjustments are explicit ledger entries, never edits.
- **FR-25 (S):** Refund/cancel-renewal flow with reason, reversing ledger entries and expiry.
- **FR-26 (C):** Promo pricing (temporary profile price override with start/end dates).

### 6.5 Managers, roles & security
- **FR-27 (M):** Manager accounts with granular permission sets (per module: view/create/edit/delete; per action: renew, disconnect, top-up, export) and per-manager user-ownership scoping (a manager sees only their own users) — flat structure in v1; tree in Phase 2.
- **FR-28 (M):** TOTP two-factor authentication (optional per account, enforceable by admin); login rate-limiting and lockout; full audit log of every manager action (who/what/when/before-after).
- **FR-29 (M):** Session management for panel logins (active sessions list, revoke).
- **FR-30 (S):** IP allowlist per manager account.

### 6.6 Monitoring, live data & lossless accounting
- **FR-31 (M):** Live Sessions table: every online session with username, NAS, IP, MAC, uptime, live down/up rate (computed from interim deltas), total session usage; auto-refreshing (≤ 2 s latency from packet to screen); filter by NAS/profile/manager; actions: disconnect (CoA), open user.
- **FR-32 (M):** Dashboard: online-users count (now + 24 h sparkline), total active/expired/expiring-soon subscribers, today's revenue, per-NAS health cards, RADIUS requests/sec, accounting-pipeline status.
- **FR-33 (M):** Per-user usage graphs: daily and monthly download/upload, session timeline; per-NAS and whole-network aggregate traffic graphs. Retention: raw sessions ≥ 12 months, aggregated daily rollups ≥ 3 years (configurable).
- **FR-34 (M):** NAS health monitoring: ICMP probe (latency/loss) always; SNMP (CPU, memory, uptime, port traffic) when community configured; per-NAS status page with probe history.
- **FR-35 (M):** System self-monitoring: FreeRADIUS up/throughput/auth-reject rate, backend API health, DB health, queue depth of the accounting pipeline, disk space — all on an admin health page.
- **FR-36 (M):** Alerts engine: rules for NAS down/up, RADIUS failure spike, accounting-queue backlog, low disk, user-expiring-in-N-days digest, low agent balance; channels: in-app, Telegram bot, email (SMTP); per-rule routing and quiet hours.
- **FR-37 (M):** **Lossless accounting pipeline:** every Accounting-Request (Start/Interim/Stop) is acknowledged only after being durably enqueued; a consumer writes to the DB; if the DB is down, the queue buffers to disk and drains on recovery; duplicates (NAS retransmits) are idempotently deduplicated by (NAS, Acct-Session-Id, record type, event timestamp).
- **FR-38 (M):** Stale-session reaper: sessions with missed interims are marked stale, then closed with a synthesized Stop after a configurable timeout, flagged as "reaped" (never silently deleted).
- **FR-39 (S):** RADIUS debug tool: live tail of auth attempts for a given username/NAS with human-readable reject reasons (bad password, expired, session limit, unknown NAS...).
- **FR-40 (M):** Pipeline audit counters: received/enqueued/persisted/deduplicated totals exposed on the health page, proving M2 (zero loss) at any moment.

### 6.7 Subscriber self-care portal
- **FR-41 (M):** Subscriber login (username/password) to a mobile-responsive portal: status, expiry, remaining quota, current speed, usage graphs, payment history.
- **FR-42 (M):** Portal renewal: redeem voucher code; pay via enabled e-wallet gateways.
- **FR-43 (M):** Portal fully localized (Arabic RTL, Kurdish Sorani RTL, English) with ISP branding (logo, name, colors) set in admin settings.
- **FR-44 (C):** Password self-change and phone-number confirmation.
- **FR-54 (M):** Both the subscriber portal and the admin/manager panel ship as installable **PWAs**: web app manifest (per-ISP icon/name from branding settings), service worker with app-shell caching and an offline "no connection" state, HTTPS-served, "Add to Home Screen" install prompt. Push notifications via Web Push where the platform allows (Android fully; iOS after home-screen install). This replaces native mobile apps; an optional TWA wrapper for Play Store distribution is a post-v1 item.

### 6.8 Reports
- **FR-45 (M):** Financial reports: revenue by day/month, by manager/agent, by profile, by payment method; agent collection/settlement report.
- **FR-46 (M):** Subscriber reports: new/expired/expiring, actives by profile, inactive-N-days; all reports filterable, exportable (CSV; printable view).
- **FR-47 (S):** Usage reports: top consumers, per-NAS totals per period.
- **FR-48 (C):** Scheduled report emails (daily digest to Omar).

### 6.9 Platform, install & licensing
- **FR-49 (M):** Docker Compose–based installer: single script on Ubuntu 22.04/24.04 LTS provisions all services; guided first-run wizard (admin account, ISP branding, first NAS, first profile).
- **FR-50 (M):** One-time license: signed license key bound to a server fingerprint, validated offline (no internet dependency for daily operation); grace behavior and re-issue flow for hardware changes.
- **FR-51 (M):** Backup/restore: scheduled DB + config dumps to local path; one-command restore; update mechanism preserving data (versioned migrations).
- **FR-52 (M):** Internal REST API used by all frontends (panel, portal), versioned from day one (`/api/v1`) so Phase-2 mobile apps and eventual public exposure need no rework.
- **FR-53 (S):** Settings module: timezone (default Asia/Baghdad), currency (IQD default, display formatting), date formats, SMTP, Telegram bot token, expiry/quota behavior defaults.

## 7. Non-Functional Requirements

- **NFR-1 Performance:** At 5,000 subscribers / ~2,000 concurrent sessions with 5-minute interims (~7 acct packets/sec sustained, 50/sec burst): auth latency < 100 ms at the backend (p99), accounting ingest keeps queue depth near zero, panel pages load < 1.5 s, live-session updates ≤ 2 s end-to-end.
- **NFR-2 Reliability:** RADIUS auth keeps working if the web panel is down; accounting is never lost across restarts of any single component (queue is disk-backed); target service availability 99.9% on a single server; unclean shutdown must not corrupt data.
- **NFR-3 Hardware footprint:** Runs fully on one modest server: 4 vCPU / 8 GB RAM / 200 GB SSD for the 5k tier.
- **NFR-4 Security:** Passwords hashed (argon2id) — note CHAP/MS-CHAP support for PPPoE requires reversible storage of subscriber RADIUS passwords: store encrypted-at-rest (AES-GCM, key in server config) and document the tradeoff; TLS on all web surfaces (bundled reverse proxy + Let's Encrypt or self-signed); RADIUS secrets encrypted at rest; audit log immutable; OWASP ASVS L2 for the web layer; rate-limited portal login.
- **NFR-5 Usability:** Every daily operator task ≤ 3 clicks from dashboard; keyboard-first global search; a new front-desk operator productive within one hour using a one-page guide; mobile-responsive panel (Hassan's phone).
- **NFR-6 Localization:** All UI strings externalized; Arabic and Kurdish Sorani with true RTL layout (mirrored navigation, charts LTR inside RTL pages); English as development baseline; numerals and currency per locale.
- **NFR-7 Offline resilience:** No feature required for daily operation may depend on internet access (license checks offline per FR-50; e-wallet payments are the only online-dependent feature and fail gracefully).
- **NFR-8 Maintainability:** Solo-dev-friendly: monorepo, one backend service + workers, migrations automated, seeded demo data, CI running unit + integration tests (including a RADIUS packet-level test harness simulating a MikroTik NAS).

## 8. Technical Architecture

**Stack (agent-recommended, user-confirmed):** Go backend · FreeRADIUS 3.2 · PostgreSQL 16 + TimescaleDB · Redis · React 18 + TypeScript · Docker Compose.

Rationale: FreeRADIUS removes all RADIUS protocol risk (same foundation as SAS4); Go gives a single static binary, cheap concurrency for the accounting pipeline and health probes, and low footprint for on-prem installs; TimescaleDB turns usage history into cheap hypertables with automatic rollups (the lossless/graphs requirements are time-series problems); Redis holds live-session state and the fast layer of the accounting queue; React+TS with an RTL-capable component library (e.g. MUI/Ant with RTL, or Tailwind + Radix with logical properties) serves both panel and portal.

```
                     ┌────────────────────────── ISP server (Docker Compose) ─────────────────────────┐
 MikroTik NAS ──1812─► FreeRADIUS 3.2 ──rlm_rest──► hikrad-api (Go)───────► PostgreSQL 16 + Timescale │
 (PPPoE/Hotspot)     │      │ 1813 acct                 ▲    │ CoA (radclient/udp)      ▲             │
                     │      └─► hikrad-acct (Go)        │    ▼                          │             │
                     │           durable queue ─────────┴─► Redis (live sessions,    rollups          │
                     │           (Redis stream +            queue, cache)                             │
                     │            disk spill)                                                         │
                     │  hikrad-monitor (Go): ICMP/SNMP probes, alert engine ──► Telegram/SMTP         │
                     │  Caddy reverse proxy ──► React admin panel · React subscriber portal ──/api/v1─┘
                     └────────────────────────────────────────────────────────────────────────────────┘
```

- **Auth path:** FreeRADIUS → `rlm_rest` → `hikrad-api` policy engine (validity, quota, sessions, MAC) → attributes back. Sub-100 ms budget; policy data cached in Redis with explicit invalidation on renewals/edits.
- **Accounting path (lossless):** FreeRADIUS forwards accounting to `hikrad-acct`, which acks only after appending to a Redis stream backed by disk spill-over; consumers upsert sessions and insert usage points into Timescale hypertables; idempotency key = (nas_id, acct_session_id, record_type, event_time). Audit counters at every stage (FR-40).
- **Live data:** Redis holds current sessions; the panel subscribes over WebSocket/SSE for the ≤ 2 s Live Sessions experience.
- **Data model (core entities):** `subscribers`, `profiles`, `nas`, `ip_pools`, `sessions` (+ `usage_points` hypertable, `usage_daily` rollup), `managers`, `roles/permissions`, `ledger_transactions`, `payments`, `vouchers` (+ batches), `alert_rules`/`alert_events`, `health_probes` (hypertable), `audit_log`, `settings`, `license`.
- **Integrations:** MikroTik RouterOS (RADIUS + CoA :3799, generated config), Telegram Bot API, SMTP, e-wallet gateways behind a `PaymentGateway` interface (create-payment / verify-callback / query-status per adapter).

## 9. Milestones & Phasing

ASAP pacing, sized for a solo developer with AI assistance; sequenced so something real is testable at every step. MVP = end of **P4**; v1 (sellable) = end of **P6**.

| Phase | Contents | Requirements | Rough size |
|---|---|---|---|
| **P1 — Skeleton & auth** | Repo, Docker Compose, DB schema/migrations, FreeRADIUS wired to Go policy API, first PPPoE Accept against real MikroTik | FR-49, FR-1 (partial), FR-8, FR-13–14, FR-17 | 2–3 wks |
| **P2 — Accounting & live data** | Lossless pipeline, sessions, Live Sessions table, usage graphs, CoA disconnect, stale reaper | FR-31, FR-33, FR-15, FR-37–38, FR-40 | 2–3 wks |
| **P3 — Users, profiles, billing** | Full user mgmt + search, profile behaviors (expiry/quota), renewals, manager balances/ledger, receipts, vouchers | FR-1–5, FR-9–10, FR-16, FR-19–22, FR-24 | 3–4 wks |
| **P4 — Managers, monitoring, alerts** *(MVP)* | Roles/permissions, 2FA, audit log, dashboard, NAS/system health, alert engine | FR-27–29, FR-32, FR-34–36 | 2–3 wks |
| **P5 — Portal & payments** | Subscriber portal (3 languages, RTL), voucher redeem, e-wallet gateway interface + first adapter(s), Hotspot login page, PWA packaging of portal + panel | FR-41–43, FR-54, FR-23, FR-18 | 2–3 wks |
| **P6 — Reports, install & license** *(v1)* | Reports, backup/restore & updates, license system, install wizard polish, CSV import, docs; pilot ISP go-live | FR-45–46, FR-50–53, FR-6 | 2–3 wks |
| **P7+ (post-v1)** | Card system + designer, reseller tree, TWA wrapper for Play Store (if needed), public API docs, more gateways/vendors | (Phase-2 backlog) | — |

## 10. Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| E-wallet gateways (ZainCash/FastPay/Qi) require merchant accounts and have weak sandbox/docs | High | Medium | Pluggable gateway interface (FR-23); v1 can ship with any subset; manual+voucher paths make payments never a launch blocker |
| MikroTik CoA/attribute quirks across RouterOS 6/7 versions | Medium | High | Test matrix on ROS 6.49 & 7.x early (P1–P2); packet-level test harness in CI (NFR-8) |
| Solo dev + "ASAP" → scope creep from SAS4 parity pressure | High | High | MoSCoW is contract: Musts only until pilot; parity items live in the P7 backlog, not v1 |
| Lossless pipeline complexity (dedupe, spill, reconcile) | Medium | High | Build in P2 (early, not bolted on); audit counters (FR-40) verify continuously; chaos tests (kill DB mid-flood) |
| License cracking of on-prem one-time licenses | Medium | Medium | Signed keys + fingerprint (FR-50); accept residual risk — support & updates are the real paid value |
| RTL/trilingual UI doubles frontend effort | Medium | Medium | RTL-capable component library from day one; logical CSS properties; ship Arabic+English first, Kurdish strings before v1 |
| Competing head-on with entrenched SAS4 | Medium | High | Wedge = monitoring/lossless data + modern UX + SAS4 CSV import (FR-6) to lower switching cost |

## 11. Decisions Log

| # | Decision | Choice | Source |
|---|---|---|---|
| 1 | Product identity | HikRAD — SAS4 alternative for Iraqi ISPs, modern UI/UX | User |
| 2 | Business model | Commercial, many ISPs; one-time license/server + paid major updates | User |
| 3 | Scale target | Up to ~5k subscribers per deployment | User |
| 4 | v1 scope philosophy | Core AAA + billing + monitoring first; cards/reseller-tree later | User |
| 5 | Surfaces | Admin panel + subscriber portal in v1; the mobile-app experience delivered as installable PWAs of both (FR-54) instead of native apps — simpler and faster to deploy; TWA/store wrapper only if later needed | User (revised 2026-07-08) |
| 6 | NAS support | MikroTik PPPoE + Hotspot on a vendor-neutral core | User |
| 7 | Billing models | Prepaid recurring profiles + one-time vouchers | User |
| 8 | Payment channels | Manual/agent collection + Iraqi e-wallets (ZainCash, FastPay, Qi) | User |
| 9 | Monitoring scope | Live sessions, lossless accounting, NAS/system health, alerts — all v1 | User |
| 10 | RADIUS engine | FreeRADIUS 3.x + custom backend (not custom protocol server) | User |
| 11 | Tech stack | Go · PostgreSQL+TimescaleDB · Redis · React+TS · Docker Compose | Agent-recommended, user-confirmed |
| 12 | Deployment | On-premise per ISP, Docker installer, offline license validation | User |
| 13 | Languages | Arabic (RTL) + English + Kurdish Sorani | User |
| 14 | Team & pacing | Solo developer + AI tooling; ASAP, phase-gated | User |
| 15 | Success metric | Pilot ISP 30 days in production with zero lost accounting records | Agent-proposed, user-confirmed |

## 12. Open Questions

1. **Gateway priority:** which of ZainCash / FastPay / Qi gets built first depends on which merchant account HikRAD (or the pilot ISP) can actually obtain — decide when pilot ISP is selected (target: during P4).
2. **Pilot ISP:** which ISP hosts the pilot deployment (drives the ROS version test matrix and Kurdish-language priority).
3. **Price point** of the one-time license — market decision, not needed before P6.
