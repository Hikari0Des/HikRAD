# HikRAD v4 — Master Plan

> **Program:** absorb SAMM's feature depth into HikRAD, fix HikRAD's accumulated structural defects, replace the delivery and licensing model, rebuild the subscriber portal, and clean out everything the previous three versions left behind.
>
> **Status:** Draft — pending owner sign-off on Decisions 57–68 (§3).
> **Numbering (verified against the repo, 2026-08-16):** FRs continue at **FR-114** (max is FR-113) · Decisions continue at **57** (max is 56) · Migrations continue at **0596** (max is 0595) · known-issues has **71** rows.
>
> **Order of truth:** [docs/PRD.md](../PRD.md) stays the master PRD; this file is the v4 program plan and amends it. Every FR below must be folded into the master PRD and its owning sub-PRD at the phase's kickoff, exactly as v2/v3 did.

---

## 1. Why v4 exists

Three things forced a program rather than another maintenance pass:

1. **The delivery model is the product's biggest field liability.** Docker is the single largest source of installation trouble; the install path, the update path, the data directories, and the permissions problems all trace back to it. SAMM's native `curl | sudo bash` + systemd install is what operators actually succeed with.
2. **HikRAD is behind SAMM on the things buyers compare.** Not on architecture — HikRAD's lossless pipeline and router-ownership model are genuinely better — but on *visible* depth: the router manager, the limits engine, the CoA outbox, double-entry accounting, tickets, notifications, self-service. §2 is the full gap list.
3. **Accumulated structural debt now costs more than it saves.** 126 planning documents, a project-instructions file that is a wall of prose, two divergent frontend token sets, 71 known-issue rows, dead code from three retired features, and a canonical repository that is **currently public on GitHub** (Decision 56) — contradicting the entire closed-source premise.

v4 is not a rewrite. Every phase is an in-place, keep-the-suite-green increment on the existing Go/React codebase.

---

## 2. Scope summary

| # | Phase | What it delivers | FRs | Migrations |
|---|---|---|---|---|
| **v4-0** | [Ground clearing](phases/phase-v4-0-ground-clearing/00-phase.md) | Repo goes private, docs collapse 126→~25, dead code/scripts/files deleted, CLAUDE.md rewritten | FR-114 | none |
| **v4-1** | [Color & theme rebuild](phases/phase-v4-1-color-theme/00-phase.md) | One shared token contract, missing semantic roles added, contrast + undefined-token CI gates, no-flash theme boot | FR-115–117 | none |
| **v4-2** | [Native install + RouterOS](phases/phase-v4-2-native-install/00-phase.md) | Docker deleted; systemd installer; RouterOS container target; updater daemon reworked | FR-118–121 | none |
| **v4-3** | [Licensing v2 + security](phases/phase-v4-3-licensing-security/00-phase.md) | New licence document + activation flow, entitlements, closed-source distribution, source-leak CI audit | FR-122–127 | 0596 |
| **v4-4** | [Limits engine](phases/phase-v4-4-limits-engine/00-phase.md) | Uptime + daily limits, speed windows, exhaust actions, seed plans, plan duplication | FR-128–132 | 0597–0600 |
| **v4-5** | [Subscribers & cards](phases/phase-v4-5-subscribers-cards/00-phase.md) | Batch creation, billing-vs-resettable counters, card groups replacing flat vouchers | FR-133–138 | 0601–0605 |
| **v4-6** | [Router manager I](phases/phase-v4-6-router-manager-1/00-phase.md) | Device monitor grid + Overview/Interfaces/General/Bridge/VLAN/IP/DNS tabs | FR-139–143 | 0606–0608 |
| **v4-7** | [Router manager II](phases/phase-v4-7-router-manager-2/00-phase.md) | 10-step wizard, PPPoE/Hotspot/Internet/Firewall/Update tabs, hotspot login pages, IPv6 | FR-144–149 | 0609–0612 |
| **v4-8** | [CoA outbox](phases/phase-v4-8-coa-outbox/00-phase.md) | Durable CoA outbox screen, hybrid update→disconnect fallback, enforcement worker | FR-150–152 | 0613–0614 |
| **v4-9** | [Double-entry accounting](phases/phase-v4-9-accounting/00-phase.md) | Journal, invoices, expenses, statements, undo window, agent approvals | FR-153–159 | 0615–0622 |
| **v4-10** | [Notifications & support](phases/phase-v4-10-notifications-support/00-phase.md) | Notification Center, SMS channel, broadcast, tickets, Telegram bot | FR-160–165 | 0623–0628 |
| **v4-11** | [Client portal rebuild](phases/phase-v4-11-portal-rebuild/00-phase.md) | Full portal rebuild, **connected-client auto-login**, self-service guest registration | FR-166–172 | 0629–0633 |
| **v4-12** | [Reports & dashboard](phases/phase-v4-12-reports-dashboard/00-phase.md) | Top-10s, traffic-by-entity, pipeline health, recent activity, final program gate | FR-173–176 | 0634–0635 |

**Order rationale.** v4-0 clears ground and closes the source leak (urgent). v4-1 must precede every screen phase or each one re-litigates color. v4-2 and v4-3 change how the product ships and is licensed, so they land before feature volume makes each release riskier. v4-4→v4-8 are the RADIUS/hotspot/PPPoE core — the product's centre of gravity. v4-9→v4-12 are the business and self-service surfaces. v4-11 depends on v4-1 (tokens), v4-4 (limits shown in the portal), and v4-9 (invoices).

---

## 3. Decisions requiring owner sign-off (57–68)

These are committed to the master PRD Decisions Log at each phase's kickoff. Recommended answers are stated; **none are final until confirmed.**

| # | Decision | Recommendation |
|---|---|---|
| **57** | **Docker is cancelled as the delivery mechanism.** Native systemd install on Linux is the only supported server path. `deploy/compose.yml`, all four Dockerfiles, and the Caddy layer are deleted; nginx replaces Caddy. | Confirm |
| **58** | **RouterOS install is retained via one OCI image — this is not a reversal of 57.** RouterOS 7.23+'s container feature is the *only* way to run software on a MikroTik router, and it consumes an OCI image, not a Docker daemon. CI builds exactly **one** all-in-one image for that target and publishes it as an importable `.tar`. No Compose file ships, no Docker is installed on any Linux server, and the image is never the Linux install path. | Confirm |
| **59** | **Caddy → nginx.** SAMM's model, additive to an existing nginx, port-fallback aware. | Confirm |
| **60** | **The canonical repo goes private immediately** (closing Decision 56's flagged contradiction), with a public source-free releases mirror as the only customer-facing repository. | Confirm — this is the urgent item |
| **61** | **Licence document v2** replaces v1: versioned payload carrying tier, subscriber cap, and **feature entitlements**; v1 blobs still verify so no customer is stranded. | Confirm |
| **62** | **White-label is a licence entitlement, not a setting** — the attribution mark is removed by a signed licence field, and the product exposes no UI to hide it. | Confirm |
| **63** | **Double-entry accounting replaces the append-only ledger as the substrate**, with the existing ledger becoming a projection of the journal. Existing ledger rows are migrated, never reinterpreted. | Confirm |
| **64** | **Connected-client portal auto-login ships opt-in and off by default**, restricted to configured trusted networks, granting a **reduced-scope** session (view + pay/renew) that requires the password to step up to full account control. | Confirm — see FR-167 for the threat model |
| **65** | **Card groups replace flat vouchers.** Existing vouchers migrate into a synthetic group per batch; no voucher code changes. | Confirm |
| **66** | **Tickets and the Telegram bot are in v4**, not deferred. | Confirm |
| **67** | **Online payment gateways (Stripe/PayPal/Binance) stay out of v4** until real merchant credentials exist — the FR-23 mistake is not repeated. | Confirm |
| **68** | **v1/v2/v3 per-agent task files, status files, and superseded briefs are deleted**, replaced by one `docs/history.md` recording what shipped and when. Gate results are kept. | Confirm |

---

## 4. New requirements (FR-114 … FR-176)

Full acceptance criteria live in each phase brief. This is the register.

**v4-0 — Ground clearing**
- **FR-114 (M):** Repository, documentation, and dead-code consolidation. Supersedes FR-113. Detailed inventory in §5.

**v4-1 — Color & theme**
- **FR-115 (M):** **Unified token contract.** One canonical token set defined in `@hikrad/shared`, imported by both apps — ending the current divergence where the panel defines `--hik-warn` and the portal defines `--hik-warning` for the same role. Adds the roles that do not exist today: `border` / `border-strong` (161 call sites currently abuse `border-surface-sunken` as a border color), `focus-ring`, `hover` / `active` / `selected` state layers, `info`, `disabled`, and a `chart-1…8` data-series palette.
- **FR-116 (M):** **Correctness and contrast gates.** CI fails on (a) any token referenced in source but undefined in either app — the live `bg-warning`/`border-warning` bug in five portal files today; (b) any raw Tailwind palette utility outside the token layer; (c) any foreground/background token pair failing WCAG AA in **either** theme. Contrast is asserted programmatically, not eyeballed.
- **FR-117 (S):** **No-flash theme boot.** `system` currently resolves in JS after hydration, so dark-mode users get a light first paint. Fixed with a `prefers-color-scheme` fallback block in CSS plus a tiny pre-hydration inline script.

**v4-2 — Native install + RouterOS**
- **FR-118 (M):** Native systemd installer (Ubuntu 22.04/24.04/26.04, Debian 12/13, other Linux with a warning): provisions PostgreSQL, FreeRADIUS 3 with dynamic DB clients, stripped binaries, nginx additively, and the unit set. Idempotent — re-running upgrades in place, never overwriting config or regenerating secrets, preserving the chosen HTTP port. Non-interactive via env vars; all prompts before work begins.
- **FR-119 (M):** Docker removal. Compose file, Dockerfiles, Caddy config, and `render-release-compose.sh` deleted; the release bundle carries binaries and assets, not images.
- **FR-120 (S):** RouterOS container target (Decision 58) — one OCI image, an importable tar, and a documented on-router procedure including the **mandatory backup-before-upgrade warning** (removing the container app destroys the database).
- **FR-121 (M):** Updater rework — the existing unix-socket daemon retargeted from `docker compose` to systemd, keeping the flock, the health gate, and the autonomous rollback that completes with the panel and daemon both dead.

**v4-3 — Licensing v2 + security**
- **FR-122 (M):** Licence document v2 — versioned payload (tier, subscriber cap, feature entitlements, issue/expiry, support window), Ed25519-signed, v1 blobs still verified.
- **FR-123 (M):** **Offline activation as a file exchange, not transcription.** The server exports a signed *activation request* containing its current fingerprint; the vendor tool consumes it and emits a licence file; the operator imports it. Removes the whole class of "re-issued against a stale fingerprint, lands straight back in grace" failures.
- **FR-124 (M):** Entitlement enforcement in one verified path — cap (hard wall + `max(10, 5%)` buffer, per FR-105) and feature entitlements, un-flippable by editing the database or config.
- **FR-125 (S):** Licence lifecycle UX — current fingerprint, per-component drift diagnostic, tier and entitlement display, expiry and support-window state, all correctly themed against FR-115's tokens.
- **FR-126 (M):** Closed-source distribution — private canonical repo, public source-free mirror, stripped binaries, **no frontend source maps**, and a **CI release-manifest audit that fails the build** if a bundle contains any source extension, `.map`, `.git`, internal doc, or vendor tool.
- **FR-127 (M):** Security hardening pass — secret scanning in CI, **removal of the checked-in FreeRADIUS private keys** under `deploy/freeradius/certs/` (`ca.key`, `server.key`, `client.key`, `*.p12`) in favour of install-time generation, separate release-signing and licence-signing keys, and the §6.1 review checklist applied to auth, uploads, payments, and the updater.

**v4-4 — Limits engine** *(SAMM parity — HikRAD today has only expiry + quota)*
- **FR-128 (M):** **Uptime limit** — cumulative connected-time ceiling per plan, with the same exhaustion actions as quota.
- **FR-129 (M):** **Daily usage limit** — per-day cap with a configurable reset time, whose exhaustion **throttles** (to a speed or a slower plan) rather than disconnecting; restored at reset.
- **FR-130 (M):** **Exhaustion action set unified** across all four limits: block · throttle to a rate · switch to another plan · move to expired pool. Evaluated in one documented, tested order.
- **FR-131 (S):** **Speed windows** — time-of-day overrides with day-of-week control and midnight crossing; highest speed wins when several match; throttled subscribers excluded. Resolved at auth *and* re-applied live by the enforcement worker via CoA.
- **FR-132 (S):** Plan ergonomics — seed plans shipped on a fresh install, one-click plan duplication, plan-level auto-renew with bulk propagation, and a list showing subscriber count, active limits, and windows.

**v4-5 — Subscribers & cards**
- **FR-133 (M):** **Batch / numbered subscriber creation** — a numbered run with a shared password, one plan, one expiry, one auto-renew choice, and a per-subscriber invoicing choice.
- **FR-134 (M):** **Counter model split** — non-resettable lifetime *billing counters* kept distinct from resettable per-limit counters, with per-counter reset actions. This is a data-model change, not a UI one.
- **FR-135 (S):** Subscriber list depth — per-limit progress bars, plan-swap badge during a temporary throttle, and the online/suspended/expired filter set.
- **FR-136 (M):** **Card groups replace flat vouchers** (Decision 65) — group-level speed, transfer, uptime and expiry-from-first-login limits, a shelf "valid until" date, add-more-cards, duplicate-group, enable/disable all, per-card counter reset.
- **FR-137 (S):** Card printing and tracking — branded PDF batches, per-card usage, card traffic surfaced separately in reports.
- **FR-138 (S):** Legacy importers beyond SAS4 CSV: Radius Manager / Super Speed Radius MySQL dumps and Pro Radius XLSX, sharing the FR-6 wizard, importing plans + subscribers only and prompting for a plan-price review afterwards.

**v4-6 / v4-7 — Router manager** *(the largest SAMM gap)*
- **FR-139 (M):** Device monitor grid — reachability, RouterOS version, CPU, memory, session count, uptime, auto-refresh.
- **FR-140 (M):** Device page shell with the tab groups, all reads going through the existing read-only inspection path.
- **FR-141 (M):** Monitoring tabs — Overview (system info + CPU/memory/session history), Interfaces (live state + per-interface traffic history).
- **FR-142 (S):** General tab — identity, time/timezone/NTP, reboot and ping tools.
- **FR-143 (S):** Network tabs — Bridge, VLAN, IP (static, DHCP client, DHCP server with live leases), DNS.
- **FR-144 (M):** **10-step setup wizard** — detect → bridge → ports → DNS → NTP → WAN → RADIUS → services → pools/profiles/firewall → review the exact command list and push.
- **FR-145 (M):** Service tabs — PPPoE and Hotspot server/profile/pool management, and the Internet tab distinguishing our uplinks (editable) from pre-existing ones (read-only).
- **FR-146 (S):** Firewall tab — rules, security analysis, and **auto-security snapshots with timed auto-revert**.
- **FR-147 (S):** RouterOS Update tab — channel, check, apply, package list.
- **FR-148 (S):** **Hotspot login page management** — router defaults, a built-in branded template, or a saved custom design, with voucher login and the self-registration tab.
- **FR-149 (C):** IPv6 dual-stack — DHCPv6-PD prefix length, pool construction, per-subscriber static prefix pinning.

*(Every one of FR-139…FR-149 is bound by the existing router-ownership principle: system-managed objects are editable and pushable, adopted objects are read-only references, nothing is written without a hash-gated preview, and nothing is ever deleted from a router.)*

**v4-8 — CoA outbox**
- **FR-150 (M):** **Durable CoA outbox** — every change enqueued as a row with states pending/sent/nack/done/failed, attempt counts, and the exact attributes sent; survives restart.
- **FR-151 (M):** **Hybrid dispatch** — CoA-Update first, automatic Disconnect-Request fallback after the configured retry count, so a router that rejects updates still picks up new settings.
- **FR-152 (S):** Outbox screen — six counters, status/action filters, username/NAS/reason search, per-row retry/cancel/inspect, sidebar badge, and 24-hour failure tracking on the dashboard.

**v4-9 — Double-entry accounting**
- **FR-153 (M):** **Journal substrate** (Decision 63) — chart of accounts and balanced journal entries under every money action; the existing ledger becomes a projection; balances stay derived; append-only discipline unchanged.
- **FR-154 (M):** **Invoices** — customer, category, dates, tax, line items; **full / pro-rated / no-charge** modes with no-charge gated by permission; auto-generated on priced renewals; PDF.
- **FR-155 (S):** **Expenses**, receipts, and payments with the unpaid/partial/due/paid lifecycle.
- **FR-156 (S):** **Financial statements** — P&L, Balance Sheet, Cash Flow, Aging, and Advanced (GL, Trial Balance, Journal), all exportable and all reconciling to the journal exactly.
- **FR-157 (C):** Cash/bank accounts, capital accounts, fixed assets with depreciation.
- **FR-158 (S):** **Five-minute undo window** with redo, explicit refusal conditions, and both the action and its reversal retained in the audit log.
- **FR-159 (M):** **Agent approval gate** — money-costing actions execute immediately; money-returning actions park as approval requests replayed through the identical code path on approval. Plus agent commission percentage and credit limit.

**v4-10 — Notifications & support**
- **FR-160 (M):** **Notification Center** — Overview / Channels / Messages / Rules & timers / Broadcast / Outbox, over one throttled queue.
- **FR-161 (M):** **SMS channel** (generic HTTP gateway + one named provider) alongside the existing email/Telegram/WhatsApp; all channel secrets encrypted at rest; per-subscriber per-channel opt-in.
- **FR-162 (S):** Editable per-event templates, per-rule timing, and the "ignore opt-out" flag for transactional messages.
- **FR-163 (S):** **Broadcast** sends to a subscriber set through the same queue.
- **FR-164 (M):** **Support tickets** — queue, priorities, statuses, public replies vs internal notes, subscriber creation and withdrawal, open-count badge.
- **FR-165 (S):** **Telegram self-service bot** — usage, invoices, tickets, notifications.

**v4-11 — Client portal rebuild**
- **FR-166 (M):** **Portal rebuild** on FR-115's tokens — status, expiry, consumed data (never the quota ceiling), current effective speed, usage over 7/14/30/90 days, session history, invoices with PDF, payment history, tickets, notification preferences, self-service profile edit, and the unified Pay screen. PWA, trilingual, true RTL.
- **FR-167 (M):** **Connected-client auto-login** (Decision 64) — a subscriber opening the portal from a device with a live session is signed into that account without typing credentials. Full contract and threat model in the phase brief; the binding rules are: **opt-in, off by default**; only from operator-configured trusted source networks; the source IP must match **exactly one** live session (ambiguity refuses); MAC corroboration required where the NAS supplies it; the resulting session is **reduced-scope** (view, renew, pay) and cannot change password, contact details, or payment configuration without a password step-up; short TTL; every auto-login audited with IP, MAC, NAS, and session id.
- **FR-168 (S):** Captive-portal hand-off — the hotspot redirect's MAC/IP parameters are treated as *hints* validated against the live-session table, never trusted on their own.
- **FR-169 (S):** **Self-service guest registration** — register on the hotspot page, passcode by SMS/WhatsApp/email, one live passcode per number, re-ask cooldown, per-MAC attempt recording, entitlement-capped daily volume, fully customisable page text.
- **FR-170 (S):** Portal ticket and notification-preference surfaces (consuming FR-164/FR-161).
- **FR-171 (S):** Portal invoice surface (consuming FR-154), including paying an unpaid invoice and immediate reconnection on payment.
- **FR-172 (C):** Portal OTP login for passwordless hotspot accounts, closing the long-open known-issues row.

**v4-12 — Reports & dashboard**
- **FR-173 (S):** Reports parity — top-10 download/upload for today and month, traffic-by-entity with separate **Users** and **Cards** tabs, daily traffic stacked bars, trend lines.
- **FR-174 (S):** Dashboard parity — clickable KPI tiles, **pipeline health panel** (CoA queue, 24h CoA failures, pending audit actions, throttled sessions, active plans), and recent-activity tables, all inside the existing permission-gated widget catalogue.
- **FR-175 (S):** Every reported figure reconciles to the journal (FR-153) exactly, verified by a reconciliation test, not by inspection.
- **FR-176 (M):** Program gate — the cross-phase acceptance run in §7.

---

## 5. Cleanup inventory (FR-114, phase v4-0)

Verified against the working tree on 2026-08-16. Nothing is deleted without being listed here first.

**Documentation — 126 `.md` files → target ~25**
- Delete **13** `docs/phases/*/status-agent-*.md` (v1 progress chatter, superseded by gate results).
- Delete the **~22** `docs/phases/phase-*/agent-N-*.md` per-agent task files — the multi-agent model was abandoned (Decision 25) and these describe an execution model that no longer exists.
- Collapse `docs/v2/` (12 briefs + 12 phase dirs) and `docs/v3/` into **`docs/history.md`**: one row per shipped phase (FRs, migrations, gate result link, date). **Keep every `gate-result.md`** — they are evidence.
- Rewrite **`CLAUDE.md`**: currently a single ~8,000-word paragraph of program history. Target: under 150 lines — what the project is, the document hierarchy, commands, architecture invariants, and a pointer to `docs/history.md`.
- Archive `REBUILD-PLANNING-PROMPT.md` into `docs/v4/` as the program's input document.
- Roll the **71 known-issues rows** into two lists: **open** (carried into the phase that fixes each) and **closed** (dated, one line).

**Scripts**
| Script | Action |
|---|---|
| `render-release-compose.sh` | **Delete** — Docker-only |
| `install.sh` | **Rewrite** native (v4-2) |
| `quick-install.sh` | Keep, retarget at the native bundle |
| `hikrad` | **Rewrite** — `docker compose` calls become `systemctl`/CLI |
| `gen-env.sh`, `gen-env.test.sh` | Rework — Compose `.env` becomes native config |
| `build-release-bundle.sh`, `verify-bundle.sh` | Rework for binary bundles; keep signature verification unchanged |
| `uninstall.sh` | Rework for systemd |
| `lint-*.sh` (3) | Keep, and add: undefined-token, contrast, secret-scan, release-manifest audit |
| `license-tool/`, `release-signing/` | Keep, move outside the shipped tree (FR-126) |

**Deploy tree**
- **Delete:** `deploy/compose.yml`, `deploy/docker/*` (4 Dockerfiles + `healthcheck.go`), `deploy/caddy/`.
- **Delete and regenerate at install:** `deploy/freeradius/certs/` — this directory contains **checked-in private keys** (`ca.key`, `server.key`, `client.key`, `*.p12`). They are FreeRADIUS's stock bootstrap certs, but shipping a known private key for any EAP surface is indefensible. Certificates become install-time generated. *(New known-issues row, security severity.)*
- **Keep:** `deploy/freeradius/` configuration itself, retargeted at native paths.

**Code**
- Retired-feature remnants: e-wallet gateway leftovers (FR-23), `allow_hotspot`, `nas.type`, `card_payments`, per-currency wallet code superseded by migration 0595, `formatIQD` now that `formatMoney` exists.
- The ~35 `Field` call sites still not passing `htmlFor`/`id` — migrate to `FormField`, which associates by construction, then delete the unsafe path.
- Any code path no test exercises through the real router (anti-pattern A14).

**Deletion rule:** every deletion is its own commit, referencing this section, with the test suite green before and after. Nothing is deleted "while I'm in here".

---

## 6. Standing rules for every v4 phase

1. **Sequential, single-agent.** One session per phase, start to finish. No parallel teams (Decision 25 stands).
2. **Migrations are one linear sequence** — always the next free number above the repo maximum. The per-phase ranges in §2 are budgets, not reservations.
3. **Frozen contracts** are declared in the phase brief and amended only in writing, in a later brief, with both sides updated in one commit.
4. **Gates are verifiable.** Every gate item names the command or observation that proves it. Hardware, browser, and dataset legs are declared **at kickoff** with the exact fallback evidence accepted — never granted an exemption afterwards (anti-pattern A1).
5. **Every bug found gets a known-issues row**, in the same commit as its fix.
6. **Security review is a gate item** on any phase touching auth, money, uploads, the updater, or the release pipeline.
7. **No native form controls, no raw color utilities, no hardcoded user-visible strings** — all three CI-enforced.
8. **Commits:** Conventional Commits with the FR id in scope or body; migrations ship in the same commit as the code and contract doc that depend on them; each phase ends with a commit carrying its `gate-result.md`.

---

## 7. Program gate (v4-12)

v4 is complete when all of the following pass in one run on a clean machine:

1. **Clean-OS native install** from the one-command path completes, and a real MikroTik authenticates a PPPoE subscriber, **in under 30 minutes measured**.
2. **RouterOS container install** completes on a real 7.23+ device and authenticates a hotspot subscriber.
3. **Upgrade** from the last v3 release preserves all data, applies migrations 0596→max cleanly on a restored realistic dataset, and reconciles the money book to the pre-upgrade figures.
4. **Deliberately broken update rolls back autonomously** with the panel and updater both killed.
5. **Release-manifest audit** finds no source, map, or vendor tool in the bundle; the signature verifies from a clean machine; the canonical repo is private.
6. **Licence lifecycle**: issue → activate by file exchange → cap enforced at the wall → entitlement gates white-label → grace never stops RADIUS or accounting.
7. **Contrast and token gates green**, and the 360/768/1280 × light/dark × LTR/RTL matrix reviewed in a real browser by a human — the one leg that may not be scripted away.
8. **Limits matrix**: expiry, quota, uptime, daily, and speed windows each verified end-to-end through the packet harness, including CoA fallback on a NACKing router.
9. **Portal**: auto-login grants a reduced-scope session from a trusted network, **refuses on an ambiguous or untrusted source**, and password step-up works.
10. **Books reconcile**: every report figure ties to the journal, verified by test.

---

## 8. Risks

| Risk | Mitigation |
|---|---|
| Docker removal breaks existing installs | v4-2 ships a one-way migration path from a Compose install to native, rehearsed on a restored pilot backup before any customer runs it |
| Router manager (v4-6/7) is large enough to become its own product | Split across two phases with independent gates; every tab is read-only first, write support added only behind the existing preview/apply pipeline |
| Double-entry migration (v4-9) touches money | Journal is added *beside* the ledger and reconciled against it for a full release before the ledger becomes a projection |
| Auto-login is a credential-free authentication path | Off by default, trusted networks only, exactly-one-session matching, reduced scope, password step-up, full audit — and an explicit security review gate item |
| Program length invites scope creep | The FR register in §4 is the contract; new owner requests land in a `docs/v4/candidates.md`, never inside a running phase |
