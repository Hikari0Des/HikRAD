# Rebuild Planning Prompt — ISP RADIUS / Hotspot / PPPoE Management Platform

> **What this file is:** a single self-contained brief to paste into a fresh planning session (a planning agent — e.g. `/plan-prd` → `/split-prd` → `/plan-team`, or any equivalent). It is **not** an implementation spec and contains no code. Its job is to give the planner everything it needs to produce the PRD + phase documents for a **greenfield rebuild** of an ISP RADIUS/billing/monitoring platform whose centre of gravity is **Hotspot and PPPoE management**.
>
> **Two source products feed the feature inventory:**
> - **HikRAD** — the existing codebase in this repo (v1 + v2 + most of v3 shipped). Its PRD, sub-PRDs and phase briefs are the accumulated product knowledge; treat them as *requirements input*, not as an architecture to preserve.
> - **SAMM** (SecuryTik Active MikroTik Manager, `https://samm.securytik.com/docs`) — the competitor whose **installation model, router-management depth, and operational surface** we are deliberately adopting/matching.
>
> The rebuild must lose **nothing** from either. Where the two disagree, the tie-breaks are stated inline below.

---

## 0. Instructions to the planning agent

You are producing **planning documents only**. Do not write application code. Do not write the installer. Produce markdown.

### 0.1 What to output

Create this document tree (paths are a recommendation — keep the shape, adjust names if you have a better one, but keep the *count* small; see anti-pattern A9):

```
docs/
  PRD.md                          # master PRD: overview, personas, FR/NFR list, decisions log
  prd/00-index.md                 # sub-PRD index + FR ownership map (every FR owned by exactly one file)
  prd/01..NN-<domain>.md           # domain sub-PRDs (aim for 8–10, not 20)
  phases/00-execution-plan.md     # ordered phase list, dependencies, migration-number budget, gate rules
  phases/phase-N-<slug>/00-phase.md  # per phase: scope, frozen contracts, acceptance gate, evidence required
  ops/install.md                  # the installation procedure spec (see §7)
  ops/security-and-source.md      # security posture + source-protection procedure (see §6)
  ops/release-and-commits.md      # GitHub workflow, commit convention, tagging, release artifacts (see §8)
  ops/known-issues.md             # empty template — every bug found from day one gets a row
```

Each `phase-N/00-phase.md` must contain, at minimum:
1. **Scope** — the exact FR IDs this phase delivers, and an explicit "not in this phase" list.
2. **Frozen contracts** — API shapes, DB tables/columns, RADIUS reply intents, events this phase publishes. Once written, these are amended explicitly in a later phase brief, never silently changed.
3. **Dependencies** — which earlier phase's contracts it consumes.
4. **Migration numbers** — the next free numbers in the single linear sequence.
5. **Acceptance gate** — a numbered checklist of *verifiable* items with the exact command or observation that proves each. See §9 for the rules on what may count as passing.
6. **Commit plan** — the commits this phase is expected to produce (see §8).

### 0.2 How to behave while planning

- **Ask, don't guess.** §10 lists the decisions you must put to the owner before finalising. Ask them in batches, with a recommended default for each. Anything not on that list: make the routine call yourself and record it in the decisions log.
- **Every feature in §3 must land in exactly one phase.** Produce a coverage table proving it (feature → FR ID → sub-PRD → phase). A feature that appears in no phase is a planning bug.
- **Order phases so something real is testable at the end of each one**, and so no phase has to be re-done by a later one (see anti-patterns A5, A4).
- **Priorities:** use MoSCoW. Musts constitute v1. Be honest about what is v1 vs later — the source products both suffered from scope creep.
- **Sequential, single-agent execution.** Plan phases to be built one at a time by one session, start to finish. Do **not** design a parallel multi-agent team split; HikRAD tried it and abandoned it.

---

## 1. Product definition (locked)

A commercial, **self-hosted, closed-source** RADIUS AAA + billing + monitoring platform for small/mid ISPs, WISPs and hotspot operators running **MikroTik RouterOS**. Sold as a **one-time license per server** with tiered subscriber caps, installed on-premise, and **white-labelable by licensed users** (§3.12). Primary market: Iraq (Arabic/Kurdish first-class, IQD, offline-tolerant), but the design must not hardcode that.

**The two things it must be better at than anything else in this market:**
1. **Hotspot + PPPoE management depth** — including managing the router's own hotspot/PPPoE servers, not just authenticating against them.
2. **Trustworthy live data** — real-time session visibility and an accounting pipeline that never loses a record.

**Non-negotiables:**
- **Offline-first.** No feature required for daily operation may depend on internet access. License validation is offline. Online payments and messaging channels are the only internet-dependent surfaces and must degrade gracefully.
- **Native install, no containers.** See §2.1 — this is a hard constraint, not a preference.
- **Closed source, and the source never leaves our infrastructure.** See §6 — customers receive compiled artifacts only, and this is an architectural constraint on every phase, not a release-day concern.
- **Vendor-neutral RADIUS core.** MikroTik ships as the certified vendor, but vendor-specific attribute names live in one adapter layer only, enforced by a CI grep gate. RADIUS replies are abstract *intents* (`rate_limit`, `address_pool`, `session_timeout`, `address_list`, IPv6 delegation…), mapped to VSAs in the adapter.
- **Router-ownership sync principle.** For every object the platform can see on a router (hotspot server, PPPoE server, IP pool, profile, address list, queue): it is either **system-managed** (created and owned by us → editable, deletable, pushable) or **router-managed/adopted** (pre-existing → a read-only reference we never write back). Adoption is always an explicit operator action, never silent. Nothing is ever deleted from a router by us.
- **Nothing is written to a router without a preview.** Plan → preview diff → per-item resolution (keep / update / abort, default abort) → hash-gated apply that refuses if router state changed between preview and apply.
- **Money is append-only.** Ledgers and audit logs are immutable at the DB level; balances are always derived, never stored-and-edited. Corrections are reversing entries, never edits.

---

## 2. Architecture direction

### 2.1 Deployment model — native only, Docker is cancelled

This is the single most important change from HikRAD and a primary reason for the rebuild. **HikRAD ships as a Docker Compose stack. That is cancelled.** Docker has caused ongoing installation trouble in the field, adds a second runtime for operators to understand and debug, and complicates permissions, volumes, ports and updates. SAMM's native install is the model to follow.

**Locked decisions:**
- **The only supported installation is a native Linux install** on Ubuntu 22.04/24.04/26.04 LTS and Debian 12/13, provisioned by a single idempotent `install.sh` that installs PostgreSQL, FreeRADIUS 3, the application binaries, nginx, and a small set of **systemd** units directly on the host.
- **No Docker, no Compose, no container images, anywhere in the product or its distribution.** No Compose file ships. No image is built or pushed. The offline release bundle contains compiled binaries and assets, not images.
- **The RouterOS-container install option is out of scope** — it is inherently container-based. Record it in non-goals (§4) as a possible future product line, not a v1 path.
- Development environments may use whatever the developer likes locally, but **CI must build and test the same native artifacts customers receive**, and the installer must be exercised in CI on a clean OS image. No developer-only container path may become load-bearing.
- Other Linux distributions: the installer accepts them with a warning rather than blocking, since systemd + PostgreSQL + FreeRADIUS are the only real OS dependencies.

The planner must state, in the architecture section, exactly which OS packages the product depends on, how the app binaries are placed and owned, which unix user each service runs as, and how the installer behaves on a host that already runs nginx or PostgreSQL.

### 2.2 Service decomposition

Take SAMM's shape — a small set of long-running systemd services coordinating through the database — and keep HikRAD's lossless-ingest guarantee:

| Service (systemd unit) | Responsibility |
|---|---|
| **api** | Admin panel + subscriber portal + REST API + the RADIUS authorize/policy endpoint |
| **radius-worker** | Time-driven sweeps (expiry, daily reset, speed windows), CoA dispatch from an outbox, stale-session reaping |
| **accounting** | Accounting ingest with durable enqueue-before-ack, dedup, session/usage persistence, pipeline counters |
| **device-worker** | Router ping, RouterOS API metadata sync, SNMP/ICMP probes, alert evaluation |
| **notification** | Throttled outbound queue for email/Telegram/SMS/WhatsApp |
| **telegram-bot** | Interactive subscriber self-service bot |
| **updater** | Small privileged agent so the panel can trigger its own upgrade — the API service cannot restart the unit set that contains it. Listens on a **unix socket only**, token-authenticated, exposing a fixed verb set, never passing an argument to a shell (see §6.4). |

**Accounting design tie-break:** SAMM accumulates usage and evaluates limits **inside PostgreSQL functions** called directly by FreeRADIUS on each interim — no application round-trip while subscribers are online. HikRAD uses a durable queue with disk spill, idempotent replay and audited counters (`received − duplicates − in_queue = persisted`). **Plan for the durable-queue guarantee as the product claim, and evaluate the in-DB accumulation as the fast path for limit evaluation** — the planner must resolve this explicitly in the architecture section, not leave both. Whichever wins, the pipeline audit counters and the "never lose an Accounting-Request" claim survive.

### 2.3 Stack

Runtime language is the planner's recommendation to the owner (§10). Fixed regardless of choice:
- **PostgreSQL** as the single store (time-series usage via a hypertable extension or native partitioning), **FreeRADIUS 3** for the protocol, **nginx** as the reverse proxy, **React + TypeScript** for panel and portal.
- **Dynamic NAS clients from the database** — adding or removing a router must never require a FreeRADIUS restart.
- Redis is optional, not assumed. If the design doesn't need it, don't ship it — one less thing for the installer to provision and the operator to debug.
- **Strong bias toward a compiled backend that installs as a single static binary with no language runtime on the host** (§10.2). Fewer host dependencies is both an install-reliability and a source-protection win (§6.2).

---

## 3. Complete feature inventory

**This is the "never miss a feature" list.** Every row must be traceable to an FR and a phase. Tags: **[H]** = exists in HikRAD, **[S]** = exists in SAMM, **[B]** = both (merge them), **[NEW]** = neither, added because the merge implies it.

### 3.1 Hotspot & PPPoE — the core (highest priority)

**Service model**
- [B] Per-router **multiple service instances** — a router runs N hotspot servers and N PPPoE servers simultaneously; every Access-Request resolves to exactly one instance before subscriber lookup; a request resolving to none rejects with a distinct NAS-config reason (not an account reason).
- [H] Per-instance: kind (pppoe/hotspot), zone/label, interface, RouterOS server name for matching, IP pool assignment, enabled flag, **management mode** (system vs router-adopted).
- [S] **IPoE / DHCP subscribers** identified by DHCP Option 82 circuit-id or MAC — a third account type alongside PPPoE and hotspot. *(New capability vs HikRAD; decide v1 vs later in §10.)*
- [H] **Service-aware reply pools** — a hotspot session never inherits the PPPoE profile's pool. When no pool applies, omit the pool intent entirely so the router falls back to its own local pool. (This was a real production bug; it must be a regression-locked rule.)
- [H] Subscriber `service_type ∈ pppoe | hotspot | dual` — hotspot-only accounts are full named subscribers (contact details, usage history, portal login), distinct from anonymous voucher cards. `dual` allows one extra concurrent hotspot session on top of the PPPoE session limit; hotspot usage counts toward validity but not the PPPoE data quota; hotspot has its own rate limit falling back to the profile's main rate.
- [H] **Passwordless hotspot subscribers**.

**Router (NAS) records**
- [B] CRUD: IP/hostname, shortname, type (`mikrotik` | generic RADIUS | **monitor-only**), shared secret (encrypted at rest), CoA port (default 3799), RouterOS API port/user/password (encrypted), SNMP community, location note.
- [S] **Monitor-only devices** — pinged and inventory-synced, no RADIUS role.
- [B] **Neighbour discovery** (MikroTik discovery protocol / IP-range scan) with add-by-IP and **one-click auto-add bootstrap** (create management user, push management IP, clear factory bridge).
- [S] Live per-row ping result with RTT; "Refresh info"; "View device".
- [H] **Read-only config inspection** over the API — current `/radius`, `/radius incoming`, `/ppp aaa`, hotspot profile AAA fields, walled-garden entries, RouterOS version — rendered as the diff source before any change.
- [B] **Push RADIUS config to the router**: creates/replaces a tagged RADIUS entry (service `hotspot,ppp`, timeout, CoA incoming accepted, our own tag so future updates are recognisable). Plus the always-available **copy-paste snippet** fallback covering every enabled service on that router.
- [H] Inline **"Save & detect"** during NAS creation — discover services with the credentials just entered, without forcing save-then-reopen.

**Router management UI (SAMM's biggest surface — match it)**
- [S] **Monitor grid**: reachable dot, RouterOS version, CPU load/cores, memory, session count, uptime; auto-refresh.
- [S] **Per-device page with tabbed configuration**, grouped:
  - *Monitoring*: **Overview** (device/system info, CPU/memory/session history charts) · **Interfaces** (live state + per-interface traffic history) · **Websites & Apps** (traffic monitor, application-aware QoS, DNS-based app/content filter).
  - *Manager*: **General** (identity, time/timezone/NTP, reboot/ping/tools).
  - *Network*: **Bridge** (create LAN bridges, assign ports) · **VLAN** · **IP** (static addresses, DHCP client, DHCP server + live lease table) · **DNS** (resolvers, cache, content filter).
  - *Services & maintenance*: **PPPoE** (servers, profiles, IP pools, accounting interim interval) · **Hotspot** (servers, profiles, user profiles, DHCP/IP pools, captive-portal login page) · **Internet** (WAN uplinks — ours editable, pre-existing shown read-only; default routes) · **QoS** (drag apps into priority slots, per-app caps + total ceiling, compiled into a tagged queue tree that removal strips cleanly) · **Firewall** (filter rules, security analysis, security modules, **auto-security snapshots with timed auto-revert**) · **Update** (channel, check/apply, package list) · **Wizard**.
- [S] **Guided setup wizard**, ~10 steps: detect existing config and skip what's done → bridge → bridge ports → DNS (presets + cache + optional filter) → NTP → internet/WAN (static/DHCP/PPPoE, multiple uplinks) → RADIUS pointing at us (auth/acct/CoA) → choose services (PPPoE / Hotspot / IPoE, optional **IPv6 dual-stack** with DHCPv6-PD prefix length /48–/64) → pools, profiles, lease times, firewall/NAT, **optional expired pool with redirect to our "subscription expired" page** → review the exact command list and push.
- [H] **Create / edit / adopt** hotspot and PPPoE servers from a panel form, planned and applied through the same preview/hash-gated pipeline, then auto-registered as service instances.
- [H] Router-field inputs are **dropdowns populated from what the router actually has** (interface names, server names, pool names), with a custom escape hatch — never blind free text.
- [S] **Hotspot login page management**: keep router defaults, apply a built-in template, or a custom saved design, with a visual editor; branded with the ISP logo/colours (§3.12); supports voucher-code login and the self-registration tab.

**IP pools & addressing**
- [B] Pool CRUD with utilisation %, exhaustion warnings, per-service assignment.
- [H] **Pool ownership**: system-managed pools are editable and **pushable** to the router's pool table; adopted router pools are read-only references (keeping the router's raw range syntax verbatim). Surface "named here, missing on the router" mismatches — the silent cause of "no address from IP pool" login failures.
- [H] **Address-list association on login** — a normal (non-expired) login can place the subscriber's IP into a named address list, driven per-profile with a per-subscriber override. Must never be emitted for the expired/walled-garden path, which already uses the same mechanism.
- [S] **IPv6 dual-stack** (opt-in): WAN address + delegated prefix in the Access-Accept, configurable prefix length, static prefix pinnable per business subscriber.
- [B] Static IP and static MAC per subscriber.

**CoA & live change**
- [B] CoA-Update to change speed live; CoA-Disconnect to drop a session.
- [S] **CoA outbox as a first-class screen**: states pending / sent / nack / done / failed, six counters, filter by status/action, search by username/NAS/reason, auto-refresh, sidebar badge, 24h-failure tracking on the dashboard; per-row retry, cancel, and **inspect the exact RADIUS attributes sent**.
- [S] **Hybrid strategy**: try CoA-Update; on repeated NACK, automatically fall back to Disconnect-Request so the subscriber reconnects with new settings. Works across RouterOS versions.
- [H] **Renew → CoA is the hero flow**: renewing a subscriber sitting in the expired pool restores full speed immediately, with no re-dial and no manual disconnect.
- [B] **Stale-session reaper** with a configurable timeout, synthesising a Stop with a distinct terminate cause, never silently deleting.
- [S] Accounting **interim-update interval** is pushed to routers and configurable centrally — document that limit enforcement stalls without it.

**Auth-time enforcement matrix** (all [B] unless noted)
- Credentials (PAP, CHAP, MS-CHAP — note the reversible-password requirement and its encryption-at-rest tradeoff), account status, expiry, quota, uptime cap, daily cap, simultaneous-session limit, MAC lock (`off | learn | fixed`, one-click reset), **router pinning / NAS scoping fail-closed** (subscriber assignment overrides profile assignment; empty = any), service-type match, speed-window resolution, licence subscriber cap.
- [H] Every rejection carries a **distinct, human-readable, localised reason** distinguishing NAS-config facts from account facts.
- [H] **Live auth debug tail** for a given username or NAS.

### 3.2 Plans / profiles

- [B] Name, price, billing period/duration, download/upload speed, simultaneous sessions (0 = unlimited).
- [S] **Four independent, optional limits evaluated in a fixed documented order**: **expiration** (calendar time) · **quota** (combined, download-only, or upload-only) · **uptime** (cumulative connected time) · **daily usage** (resets at a configured time). Each with an exhaustion action: switch to another plan, disconnect, throttle to a set speed, or move to the expired pool.
- [S] **Speed windows** — time-of-day overrides with day-of-week control, crossing midnight; highest speed wins when multiple match; throttled subscribers excluded.
- [B] Burst rate/threshold/time.
- [B] Auto-renew (start a fresh period on expiry) with bulk propagation of the setting.
- [B] IP pool, address list, expiry behaviour (hard block vs expired pool/walled garden), quota-exhausted behaviour.
- [S] **Seed plans shipped** so a fresh install is usable immediately (a set of tiers plus an "Expired" throttle plan).
- [S] **Duplicate a plan** (copies all settings, regenerates any codes).
- [S] Plan list shows subscriber count, active limits, and speed windows at a glance.
- [H] **Versioned cost price** — what the operator pays upstream, effective-dated and never overwritten, so re-pricing never rewrites past reported profit. A plan with no cost reports margin as *unknown*, never as 100% profit.
- [H] Per-subscriber overrides: custom rate limit, custom renewal price.
- [H] Profile **filter bar, multi-select + bulk actions, and CSV import** at parity with the subscriber list.
- [H] Profile-change scheduling (apply at next renewal vs immediately with proration) — *Could*.

### 3.3 Subscribers

- [B] CRUD: username (unique), password, first/last name, mobile, **email**, address, notes, owner/manager, plan, expiry (+ per-subscriber override), status, auto-renew, static IP, static MAC, MAC-lock mode, service type, NAS pinning, address-list override, session limit override.
- [S] **Batch / numbered account creation** — create a numbered run (001, 002, …) in one pass with a shared password, one plan, one expiry, one auto-renew choice, and a per-subscriber invoicing choice. *(HikRAD lacks this; hotspot operators rely on it.)*
- [B] **Global instant search** across username/name/phone from every screen, keyboard-first.
- [S] List shows: status dot, plan, **per-limit progress** (daily usage bar, quota/uptime/days remaining), and a **plan-swap badge** when a temporary throttle plan is in force.
- [B] Filters: all / active / suspended / expired / online, by plan, by NAS, by service type, by owner — **persisted per manager**.
- [B] Detail page combining live session state, usage graphs, session history, payment history, and the audit trail of changes.
- [B] Per-subscriber actions: edit, enable/disable (disconnecting live sessions), **renew** (extends per plan rules, auto-generates an invoice when priced), **reset uptime / quota / daily counters**, usage view over 7/14/30/90 days, delete (with session disconnect).
- [S] **Two counter families kept distinct**: non-resettable lifetime "billing counters" vs resettable per-limit counters. This distinction must exist in the data model from day one.
- [B] Bulk operations: enable/disable, change plan, extend expiry, change owner, set service type, set address list, delete, export.
- [B] Session history per subscriber: start/stop, duration, bytes down/up, NAS, terminate cause.
- [B] **CSV/XLSX import wizard**: field mapping, preview with warnings, duplicate-username handling choice, dry run, per-row independence (one bad row never rolls back the rest), report.
- [S] **Migration from legacy systems as a named feature**: SAS4 / Radius Manager (MySQL dump), Super Speed Radius (MySQL dump), Pro Radius (XLSX/CSV). Import plans + subscribers (+ static IP/MAC, disabled status); explicitly do **not** import billing history/invoices/cards/tickets; prompt the operator to review imported plan prices before invoicing.

### 3.4 Hotspot cards / vouchers (merge HikRAD vouchers into SAMM's card model)

- [B] **Card groups**: name, description, optional username prefix, count (up to ~10,000 per group), code format (numeric / alphabetic / alphanumeric) and configurable length, price.
- [S] Per-group service limits independent of plans: download/upload speed (0 = unlimited), transfer limit (unlimited / combined cap / separate down+up caps in MB/GB/TB), uptime cap, **expiration counted from first login**, and a group **"valid until" shelf date**.
- [S] Lifecycle: unused → active → exhausted (quota/uptime/expiry) or disabled.
- [S] Group management: enable/disable all cards at once, extend "valid until", **add more cards to an existing batch**, reset an individual card's counters, **duplicate group** (same settings, fresh codes — one-click reorder).
- [B] **Print to PDF** for distribution, auto-sizing the ISP logo from branding settings; card designer is a later phase.
- [B] Redemption paths: operator, subscriber portal, and **hotspot login page**.
- [S] Per-card usage review; card traffic appears in reports (own tab) and in accounting.
- [H] Batch list showing used/unused, CSV export, single-use enforcement, code-expiry.
- [H] **Voucher code display normalisation** — separators and grouping must render exactly as printed (this was a real bug).

### 3.5 Self-service guest registration [S — new to us]

- Visitor connects, taps Register on the hotspot page, enters name + mobile, receives a passcode by SMS / WhatsApp / email, enters it, gets an account. The ISP keeps a named, exportable record instead of anonymous guests.
- Abuse controls: one live passcode per mobile number; re-asking during validity says "check your messages" instead of resending; a waiting period after expiry before a new request; validity counted from send time; **every attempt recorded against the device MAC**; a daily cap by licence tier.
- Setup path: enable a messaging channel first → enable self-registration and choose channels, passcode validity and re-ask cooldown → enable per login design and **push the page to the router** (and re-push after upgrades, since pages live on router flash).
- Full text customisation of the register tab, intro line, field labels, button, and every reply message.

### 3.6 Billing, accounting & payments

**Core ledger**
- [H] Append-only immutable ledger; balances always derived; discounts and adjustments are explicit entries; refunds/cancellations are reversing entries with a reason.
- [S] **Full double-entry bookkeeping** underneath — every action posts a balanced journal entry automatically, while operators work in plain terms (invoices, expenses, receipts, payments). *Adopt this; it subsumes HikRAD's ledger and unlocks the statements below.*
- [S] **Invoices**: customer, revenue category, issue/due dates, tax rate, line items; three charging modes — **full period, segmented/pro-rated, or no charge** (the last gated by a special permission); auto-generated on priced renewals; filter by unpaid/partial/due/paid; PDF.
- [S] **Expenses** (category, vendor, amount, incurred/due dates) with the same lifecycle.
- [S] **Receipts** (money in) and **payments** (money out), moving invoices/expenses toward paid.
- [S] **Financial statements, exportable**: Profit & Loss, Balance Sheet, Cash Flow, Aging (receivables/payables by bucket), and Advanced (General Ledger, Trial Balance, Journal, activity log).
- [S] Cash and bank accounts, **capital accounts** (owner contributions/withdrawals), **fixed assets with depreciation**.
- [H] **Printable receipts** in Arabic and English templates with receipt numbers.

**Currency & pricing**
- [H] **Single-book currency model** — one operator currency (default IQD), one balance per manager. Foreign amounts convert **at entry time** at an admin-entered rate, stamping the entered amount, entered currency and rate id on the ledger row; a refund reverses the stored home-currency amount exactly, never re-converted. **No online rate feed** — rates are admin-entered only. *(HikRAD built per-currency wallets and then reversed it; do not repeat that — see A4.)*
- [S] Currency code + symbol + tax on/off/label/rate in settings.
- [H] Locale-correct money formatting including Eastern-Arabic numerals.

**Manager / agent / reseller money**
- [H] Manager balances; renewals performed by an agent deduct from their balance; admins top up; every movement is a ledger transaction.
- [S] **Agent wholesale model**: agent buys at wholesale and resells at retail; **commission percentage** per agent; **credit limit** controlling how far a prepaid balance may go negative; the wholesale price hits the agent's balance on renewal while the retail invoice is issued to the end customer and payable to the agent.
- [H] Flat two-level reseller pricing (no sub-reseller tree): versioned wholesale price per plan, optionally overridden per subscriber; retail and wholesale are two explicit ledger legs, never one row carrying both meanings.
- [H] **Reseller data isolation is a commercial-severity contract** — a reseller never sees the owner's plan cost or another reseller's wholesale price. Omit the fields from their API responses; do not null them.
- [H] Global and per-site **overheads** (uplink, staff, power, rent), never blended with each other; gross margin per plan stays exact and is never adjusted by overheads; net margin is a separate, clearly-labelled period figure.
- [H] Margin derived on the ledger from a `cost_at_sale` stamp; never a separately editable number.

**Undo & approvals [S — new to us, high value]**
- A **five-minute undo window** on actions that move money or state, with redo, and an explicit refusal when the referenced record no longer exists, was recreated, changed after the action, or the window has closed. **Both the original action and its undo stay in the audit log** — reversal never deletes.
- **Agent approval gate**: actions that cost the agent money (activate, renew, plan change, collect payment) execute immediately; actions that return money (undo, delete subscriber, void invoice) park as **requests requiring superadmin approval**. Requests queue in an approvals screen; approval replays the exact same code path the agent would have run (one implementation, not two); agents see their pending requests and may withdraw them; decisions are kept in history with an optional reason.

**Payment collection**
- [H] **Manual payment providers** — a catalogue of named providers (mobile-money, bank, etc., **name only, no API**), with **per-manager receiving accounts** and per-manager method enablement. A subscriber only ever sees a method their *owning manager* has both enabled and configured an account for, with an instance-level default account used only for unowned subscribers. No cross-manager fallback, ever.
- [H] **Unified Pay screen** in the portal listing every enabled method as tiles; provider submissions carry amount, reference, date, note and **file attachments** (receipt photo/PDF, size/type limited, served back to reviewers only as forced downloads).
- [H] **Payment tickets**: every submission is a ticket with a full timeline (submitted, attachments, provisional grant, approved/rejected with reviewer note, who and when). The first submission per payment cycle grants an **immediate 1-day provisional renewal**; a rejected submission may be resubmitted immediately but earns no further trial day until an approval resets eligibility. Owner queues see everything; scoped agents see only their own.
- [H] **Telecom scratch-card payments** with manual verification, as one method tile sharing the same ticket machinery.
- [S] **Online payment gateways: Stripe (cards), PayPal, Binance/USDT (TRC20/BEP20/ERC20 + Pay ID)** — the ISP uses their own accounts, money goes directly to them; test/sandbox modes; optional webhooks; paid invoices reconnect expired subscribers automatically; each payment posts one balanced double entry recording the method. **Internet-dependent — must degrade gracefully and never be on a daily-operation path.** *(HikRAD built and then deleted an e-wallet gateway layer with no merchant account behind it — see A3. Only plan this phase once real credentials exist.)*
- [B] Promo pricing / temporary plan price override with dates — *Could*.

### 3.7 Monitoring, live data & reports

- [H] **Lossless accounting pipeline**: ack only after durable enqueue; disk-backed buffer that drains on recovery; idempotent dedup keyed on (NAS, session id, record type, event time); **audit counters exposed on a health page** proving zero loss at any moment.
- [B] **Live sessions**: every online session with username, framed IP, NAS, plan, **effective speed after speed windows with a throttled badge**, live down/up counters, session duration, start time; ≤2s freshness; search by username/IP/NAS, filter by router, "only throttled" toggle, page-size selector; **per-row disconnect** with confirmation and a recorded reason.
- [B] **Dashboard**: period selector; clickable KPI tiles (online now, active subscribers, expired, open tickets, routers reachable, unpaid invoices); users/sessions/expired trend chart; **pipeline health panel** (CoA queue depth, failed CoA in 24h, pending audit actions, throttled sessions, active plans); recent-activity tables (recent CoA outbox, recent admin actions, recent subscribers, router health).
- [H] **Customisable dashboards**: a permission-gated widget catalogue (deny-by-default, checked by permission string), an add-widget gallery with descriptions, per-manager layout (order/size/visibility) persisted with the manager's preferences, reset-to-default, always single-column on phones.
- [B] **Usage graphs**: per-subscriber daily/monthly down/up and session timeline; per-NAS and whole-network aggregates; retention policy (raw sessions ≥12 months, daily rollups ≥3 years, configurable).
- [S] **Reports**: date presets + explicit ranges; subscriber/session/expired trend lines; **daily traffic stacked bars (up vs down)**; **top-10 lists by download and upload, today and this month**; **traffic by entity** with separate **Users** and **Cards** tabs (today MB / month GB, searchable, paginated, rows linking to detail).
- [H] Financial reports (revenue by day/month/manager/plan/method, **agent collection & settlement**), subscriber reports (new/expired/expiring/inactive-N-days/actives by plan), usage reports (top consumers, per-NAS totals). All filterable, exportable to CSV/XLSX, printable — and **every reported figure must reconcile to the ledger exactly**.
- [B] **Scheduled digests** (daily summary to the owner) — *Could/Should*.
- [B] **NAS & device health**: ICMP always, SNMP when configured (CPU, memory, uptime, port traffic); per-device status page with probe history; **non-NAS monitored devices** (APs, switches, servers) probed by the same engine with their own alert rules and no RADIUS role.
- [B] **System self-monitoring**: RADIUS up/throughput/reject rate, API health, DB health, queue depth, disk space, on one admin health page.
- [B] **Alerts engine**: rules for NAS down/up, device down/up, RADIUS failure spike, accounting backlog, low disk, users expiring in N days, low agent balance; per-rule routing and **quiet hours**.

### 3.8 Notifications & support

- [S] **Notification Center with six tabs**: **Overview** (live queued/sending/sent-today/failed/skipped/opted-out counts, channel state, recent feed) · **Channels** · **Messages** (editable per-event templates) · **Rules & timers** (enable/disable per rule, "ignore opt-out" for transactional messages, timing such as days-before-expiry, batch size and tick interval) · **Broadcast** (one-off sends to a subscriber set, through the same throttled queue) · **Outbox** (every message with delivery status and failure reason).
- [S] **Channels**: Email (a notification account kept separate from the password-recovery SMTP account), Telegram (bot token), SMS (generic HTTP gateway or a named provider), WhatsApp (official Cloud API, optionally an unofficial QR-linked bridge). **All channel secrets encrypted at rest.** Subscribers opt in/out per channel from the portal.
- [B] **Automatic events**: renewal reminder before expiry, expiry notice, quota warning, payment receipt, plan renewed, payment-ticket state changes (both to the subscriber and to the owning manager), alert notifications to staff.
- [H] Web Push to the panel (badge counts for queues needing attention).
- [S] **Support tickets** *(new to us)*: queue with number, customer, subject, priority (normal/moderate/critical), status (open / in progress / resolved / closed / withdrawn), opened and last-updated dates, message count; filter chips and search; created by admins for any customer or by subscribers from the portal and the Telegram bot; **public replies vs internal staff-only notes**; status/priority changes; subscriber withdrawal; sidebar badge of open tickets.
- [S] **Telegram self-service bot** *(new to us)*: check usage and invoices, submit and follow tickets, receive notifications.

### 3.9 Subscriber portal

- [B] Login; status, expiry, **consumed data** (never display the plan's quota ceiling or remaining balance — deliberate product rule), current speed, usage graphs over 7/14/30/90 days, session history, payment history, account details.
- [B] Renewal: redeem voucher/card code, pay via any enabled method, **online gateway payment of unpaid invoices with immediate reconnection**.
- [S] Invoices list with PDF download.
- [S] Submit and track support tickets; manage notification channel opt-ins.
- [H] Self-service account maintenance (own password, contact details with a phone-confirmation flow).
- [H] Fully localised with ISP branding; **installable PWA** with app-shell caching, an offline state, and Web Push where the platform allows.

### 3.10 Managers, roles, security

- [S] **Permission model: three levels per area — not allowed / view / edit** — plus special money rights: **"allow no-invoice"** (make service changes without billing, default off) and **"allow agents approval"**. Simpler and more teachable than a long permission-string list; keep permission checks by string internally, never by role name.
- [S] Built-in roles: **Superadmin** (full access; admins/tools/API areas are superadmin-only and cannot be granted to custom roles) and **Agent** (locked role: prepaid balance + commission split + the approval gate).
- [S] **Data visibility scope**: "see all customers & cards" on/off; when off, the role is **scoped to specific routers (By-NAS)** and sees only subscribers and cards pinned to them — the mechanism for branches, franchises and resellers.
- [S] **Principle to encode explicitly:** whatever a role is denied, every admin keeps their own dashboard and profile — a login that lands on a permission error is a broken login, not a secure one.
- [B] Manager contact fields, disable + guarded hard delete (managers with ledger history can only be disabled).
- [B] TOTP 2FA (optional per account, enforceable), login rate limiting and lockout, active-session list with revoke, optional per-manager IP allowlist.
- [B] **Audit log**: actor, timestamp, target, action type, applied state, **field-level before/after diff**, human-readable per-action titles in every supported language, filterable, exportable, API-readable, append-only.
- [S] **REST API** with scoped keys and role-based access, documented, capable of exporting raw accounting data (journal, chart of accounts, trial balance, P&L, balance sheet) as JSON or CSV for BI pipelines.
- [H] Per-manager **preferences** (language, theme, numerals, landing page, page size, notification routing), self-only, presentation-only by construction — a preference may never carry a permission, scoping, or money rule.

### 3.11 System, settings & platform

- [S] **Hot-reloadable tunables**, effective on the next tick with no restart: worker tick intervals, accounting interim interval pushed to routers, daily reset time, server timezone, CoA default port, CoA retry count before disconnect fallback, stale-session timeout.
- [B] **Languages**: English, Arabic (RTL), Kurdish Sorani (RTL) are required; SAMM additionally ships Turkish, French, Spanish, German, Russian, Persian — plan the string layer so adding a language is a JSON file, and decide the v1 set in §10. True RTL with logical properties; usernames/MACs/IPs/numerics stay LTR islands inside RTL pages. **No hardcoded user-visible strings — a CI-fatal check from phase 1.**
- [B] **Themes**: dark / light / system.
- [B] **Tools**: bulk changes, bulk delete, export/import (subscribers, plans, routers as CSV/XLSX), legacy migration, **one-click backup with download to the operator's computer**, and restore from an uploaded file.
- [S] **Maintenance CLI** (`<product>-tools`): one-keystroke health check, backup, restore, admin password reset, service status, log tail.
- [B] **WireGuard VPN**: server keypair generation, listen port, tunnel and client address ranges, peer management with downloadable configs, QR codes, and generated RouterOS commands — for remote admin and for reaching routers behind the server. Document the "regenerating server keys invalidates every deployed client config" trap.
- [B] **Cloudflare Tunnel**: paste a Zero Trust token, connector configured automatically, no inbound firewall port ever opened, status states (running/stopped/not configured/not installed), token stored outside the database. Only the web surface is ever tunnelled — never RADIUS or CoA.
- [B] **Licensing**: signed, offline-validated licence bound to a server fingerprint; a **tier catalogue** (name → max subscribers → feature entitlements including white-label, §3.12); **real enforcement of the subscriber cap** as a hard wall with an overage buffer and an in-band renew/upgrade prompt, enforced at subscriber creation and at CSV import; grace and expired-grace behaviour that **never stops RADIUS auth or accounting** and only gates panel writes; the machine's **current fingerprint surfaced in the licence page and the first-run wizard** with a per-component drift diagnostic; separate self-service tier limits if that feature is licensed independently; an **interactive vendor-side key issuance CLI** (never shipped to customers, §6.3) that emits a byte-identical blob to the flag-driven form.
- [B] **First-run wizard**: admin account, ISP identity, first router (with inline service detection), first plan, licence activation — with **working Back navigation** and every step actually persisting on Next.
- [S] Optional, clearly-disclosed, easily-disabled **install telemetry** (install id, version, tier, coarse country) sent only on update checks — decide with the owner (§10).

### 3.12 White-label / rebranding for licensed users [locked requirement]

The product must be **rebrandable by a licensed customer**, not merely "logo-configurable". Treat this as a first-class feature area with its own FRs, and design it in from phase 1 rather than retrofitting it (retrofitting branding was one of HikRAD's most expensive mistakes — A7).

**The rebrandable surface** — one canonical identity resolver, consumed everywhere, never re-read ad hoc by individual screens:
- Instance name, legal/company name, support phone and email, tax/registration number.
- Logo (upload validated server-side: content-sniffed, size- and dimension-limited, stored on disk — never inlined into a settings row), favicon, and a login-screen/hero image.
- Accent/brand colour and light+dark palette derivation from it.
- Consumed by: panel shell and panel login, portal shell and portal login, **both PWA manifests and icon sets** (regenerated on logo change), the **hotspot captive-portal page**, **printed receipts, invoices and voucher/card PDFs**, notification templates (email/Telegram/SMS/WhatsApp headers and signatures), report headers, and the browser tab title of both apps.
- Custom domain support and the documentation link target (a rebranded install should not deep-link to our wiki under our name unless the tier allows it).

**Entitlement model** — decide with the owner (§10.1), recommended default:
- **Standard tiers:** full ISP branding as above, **plus** a small, fixed product attribution mark on the panel/portal shells and login screens only — never on the ISP's own printed documents to their subscribers.
- **White-label tier:** the attribution mark is removed by the **licence entitlement itself**, not by a settings toggle. The removal must be driven by a signed licence field so it cannot be flipped by editing the database or a config file, and the check lives in the same verified path as the subscriber cap.
- The product surface must never expose a UI option to hide attribution — an unentitled install has no path to it at all.

**Planning constraints:**
- No string, image, colour, or URL that a rebrand must change may be hardcoded in a component. The i18n check (A6) and a branding-hardcode grep gate should both run in CI.
- A rebrand must require **zero rebuild** — it is data plus uploaded assets, applied at runtime, so one signed binary serves every customer. Never plan a per-customer build.
- Document the exact rebranding procedure for a licensed operator as an ops page.

---

## 4. What the platform must *not* do (v1 non-goals)

State these explicitly in the PRD so scope creep has something to bounce off:
- **No Docker, Compose, or container distribution of any kind** (§2.1). The RouterOS-container deployment SAMM offers is out of scope for the same reason; revisit only as a separate future product line.
- No cloud/SaaS multi-tenant hosting.
- No native mobile apps (PWAs only; a store wrapper is a later decision).
- No non-MikroTik vendor *certification* (the architecture stays vendor-neutral, but only MikroTik is tested and supported).
- No FTTH/OLT management, no postpaid invoicing beyond the invoice model above, no CRM.
- No infinite reseller tree (flat two levels, deliberately).
- No DRM, obfuscation, or anti-debug work. On-prem software cannot be made tamper-proof; protection is source-withholding + signing + contract + support cut-off (§6), not technical DRM.
- No publicly documented third-party API stability guarantee in v1 (the API exists and is versioned; public docs come later).

---

## 5. Non-functional requirements

- **Performance:** at 5,000 subscribers / ~2,000 concurrent sessions with 5-minute interims (~7 accounting packets/sec sustained, 50/sec burst): auth decision under 100 ms p99, accounting queue depth near zero, panel pages under 1.5 s, live-session updates within 2 s end to end.
- **Reliability:** RADIUS auth keeps working when the panel is down; accounting survives a restart of any single component; unclean shutdown never corrupts data; target 99.9% on a single server.
- **Hardware:** the 5k tier runs on 4 vCPU / 8 GB RAM / 200 GB SSD.
- **Security:** see §6 — it is large enough to be its own section, and it is a phase-1 concern.
- **Usability:** every daily operator task within three clicks of the dashboard; a new front-desk operator productive within an hour from a one-page guide; the panel is fully usable on a phone (field agents work from phones).
- **Localisation:** all strings externalised; true RTL; per-locale numerals and currency.
- **Offline resilience:** as §1.
- **Maintainability:** monorepo; migrations automated and idempotent; seeded demo data; CI running unit + integration tests including a **RADIUS packet-level harness that simulates a MikroTik NAS** (PAP/CHAP accept and reject cases, sustained-load mode, CoA listener, neighbour-discovery announce), plus a clean-OS installer run.

---

## 6. Security posture & source-protection procedure

Write `docs/ops/security-and-source.md` covering both halves below. **Both are phase-1 constraints**, not release-day chores: an architecture that leaks source or secrets cannot be patched later without rework.

### 6.1 Product security (what protects the customer's install)

- **Credentials at rest:** admin/manager passwords hashed with a modern memory-hard KDF. Subscriber RADIUS passwords must be **reversibly** stored because CHAP/MS-CHAP requires cleartext at auth time — encrypt them (authenticated encryption, key in server config outside the database), confine decryption to the authorize code path only, and document the tradeoff in the PRD rather than hiding it.
- **Everything else secret is encrypted at rest too:** RADIUS shared secrets, RouterOS API credentials, SNMP communities, SMTP/Telegram/SMS/WhatsApp tokens, gateway API keys, tunnel tokens.
- **Transport:** TLS on every web surface, with a self-signed default so a fresh install is never plaintext, and a documented path to a real certificate. RADIUS and CoA are never exposed through the tunnel.
- **Web layer:** OWASP ASVS L2 as the target. Rate-limited and lockout-protected login on panel and portal, CSRF protection, strict content-type handling that does not accidentally block legitimate multipart uploads (a real HikRAD bug — A14), secure cookie flags, security headers, and no user-supplied file ever served executable or inline (forced-download disposition).
- **Authorization:** deny-by-default permission checks by string on every endpoint; data scoping (owner and by-NAS) enforced server-side in the query, never in the UI; the reseller/agent isolation contract of §3.6 treated as a security boundary, not a display preference.
- **Audit:** append-only at the database level (revoke update/delete), covering every mutating action with actor, target, and before/after.
- **Uploads:** content sniffing by magic bytes, size and dimension ceilings, SVG script rejection, and storage outside any executable path.
- **Backups contain secrets** — say so explicitly, and state whether backups are encrypted and how the key is handled.
- Plan a **security review as an explicit gate item** on the phases that introduce auth, payments, uploads, and the updater.

### 6.2 Source protection (what protects us)

The rule is simple and absolute: **no customer, reseller, or white-label licensee ever receives source code, and no shipped artifact contains it.**

- **Two repositories.** A **private source repository** (never granted to a customer or reseller, under any support scenario) and a **separate public, source-free releases repository** holding only the thin bootstrap install script and signed release assets. No `.go`/`.py`/`.ts`/`.tsx`/`.sql` source file, no internal doc, and no `.git` directory ever appears in a release artifact.
- **Customers receive compiled artifacts only:** stripped server binaries, a production frontend build, migrations, the installer, and runtime config templates. **No source maps in the shipped frontend build.** No debug symbols. No build toolchain requirement on the customer's server — this is a second reason to prefer a compiled single-binary backend (§10.2).
- **CI enforces it.** The release job runs a **manifest audit that fails the build** if a bundle contains any source-file extension, `.map` file, `.git` directory, internal document, test fixture, or vendor-only tool. This gate is written in phase 1, before the first release exists — not after the first leak.
- **Signing.** The release bundle carries a checksum manifest and a **detached signature verified against a public key embedded in the installer before any byte is used**; a tampered or unsigned bundle is refused with no partial effect. The **release-signing key is separate from the licence-signing key**; both private keys live only in the CI secret store and an offline backup, never in the repo, never on a customer machine, and never in a bundle.
- **Vendor-only tooling** (licence issuance CLI, key management, internal scripts) lives outside the shipped tree, is never packaged, and has no runtime or product surface.
- **Support procedure without source access:** define how a support engineer diagnoses a customer install — log bundles, health export, a diagnostic command in the maintenance CLI — so no situation ever creates pressure to hand over source or grant repo access.
- **Accept the residual risk honestly.** On-prem binaries can be reverse-engineered. The mitigations are commercial: no source, signed updates, licence-gated updates and support, and a fingerprint-bound licence. Do **not** plan obfuscation, packers, or anti-debug work — HikRAD explicitly ruled these out as cost without benefit, and that decision stands (§4).
- **White-label does not weaken any of this** (§3.12): a rebrand is data and uploaded assets applied at runtime by the same signed binary every other customer runs. Never plan a per-customer source drop, a per-customer build, or a customer-side rebuild step.

### 6.3 Licence and key handling

- Licences are signed offline, bound to a server fingerprint, and carry tier, subscriber cap, feature entitlements (including white-label), and validity.
- The entitlement check and the cap check run in the same verified path; a customer editing the database or a config file must not be able to grant themselves an entitlement.
- Fingerprint drift must produce a **clear per-component diagnostic**, because the common failure is a re-issued key signed against a stale fingerprint that lands straight back in grace.
- Grace semantics are a product promise: **RADIUS auth and accounting never stop**; only panel writes are gated. No feature added later may violate this.

### 6.4 Updater and privileged-surface hardening

- The updater agent listens on a **unix socket only**, never TCP, authenticated by a per-install token, exposing a **fixed small verb set** with no verb ever accepting an argument that reaches a shell.
- It shells into the same maintenance CLI path the operator would run, rather than reimplementing backup/apply/health-check/rollback.
- A lock makes a second concurrent update impossible from either the panel or the CLI, and rollback must complete autonomously even if the panel and the updater are both dead.

---

## 7. Installation procedure — plan this as a first-class deliverable

Write `docs/ops/install.md` as a **specification of the installer's contract**, not as a tutorial. **Native only — no container option appears anywhere in it** (§2.1).

**7.1 Install matrix** — each with the exact end-user command, prerequisites, and the resulting access URL and default credentials:
- **A — one command** (recommended, production): a single `curl … | sudo bash` that fetches the latest signed release, verifies it, extracts it, and runs the full installer unattended. Provide a **mirror URL** for networks where the primary host is blocked.
- **B — manual bundle** (air-gapped / offline sites): download a versioned archive out-of-band, verify, extract, run the installer inside it. This path must work with **no internet access at all** on the target server.
- **C — ISO appliance** (later phase, not v1): a bootable image that installs the OS and the product on first boot, with a published checksum, default OS and panel credentials, and the maintenance CLI available at the console.

**7.2 Installer contract** — properties every path must satisfy:
- **Idempotent**: re-running it upgrades in place. It never overwrites existing config, never regenerates existing secrets, and preserves the HTTP port chosen at first install.
- **Additive to the host**: an existing nginx keeps its existing sites; an existing PostgreSQL is detected and used or clearly refused with a reason; if port 80 is taken, prompt for an alternative and remember it.
- **Non-interactive mode via environment variables** — at minimum: database password (first install only), tunnel token, HTTP port, verbose output, colour off. Any prompt must occur *before* installation begins, never halfway through.
- **What it provisions**: FreeRADIUS with the database backend and dynamic clients (validated by the RADIUS config checker before the service is enabled), PostgreSQL with schema and all migrations applied, the application binaries and compiled translations, the reverse proxy, the systemd units, and the optional VPN/tunnel binaries (installed but inert until configured).
- **Service accounts and permissions**: each unit runs as a dedicated non-root user; data, upload, backup and spill directories are created and owned correctly **by the installer on both fresh install and upgrade** (A11).
- **A printed summary at the end**: URL, default credentials with a change-immediately warning, and config file locations.
- **Upgrade path**: fetch the new release and re-run the same installer — re-applies migrations, reloads RADIUS and proxy config, restarts units. Plus a **panel-triggered one-click update** with live progress that survives the API service restarting mid-update, and an **auto-update toggle**.
- **Update safety**: pre-update backup, health gate after apply, **automatic rollback on failure that completes even if the panel and updater are both dead**, and the concurrency lock of §6.4.
- **Uninstall** and **factory-reset** (wipe data, keep the install) as supported, documented commands.
- **Release integrity**: as §6.2 — signature verified before any byte is used.
- **First-run**: the wizard in §3.11, reachable immediately after install.

**7.3 Ports and firewall** — enumerate them: RADIUS auth and accounting (UDP 1812/1813), CoA (UDP 3799), HTTP/HTTPS, VPN (UDP, configurable). Note that the Cloudflare Tunnel option opens none.

**7.4 Time-to-first-authenticated-user target**: a mid-level network tech, on a clean server, from nothing to a first authenticated PPPoE session **in under 30 minutes** following the doc. Make this a measured acceptance item of the install phase, not an aspiration.

---

## 8. GitHub, commits and releases — plan this explicitly

Write `docs/ops/release-and-commits.md` pinning:

**8.1 Repository layout**
- The two-repo split of §6.2 (private source, public source-free releases). State the access policy in writing: who has access, what a reseller gets, what a support case gets.
- Recommend a monorepo for the source: backend, frontend workspaces, deploy config, scripts, migrations, docs.
- Branch protection on the default branch, required CI, no direct pushes, and no force-push.

**8.2 Branching and commits**
- Work on a branch per phase; merge to the default branch through a PR that references the phase and its FR IDs.
- **Conventional Commits** (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`, `build:`, `ci:`, `security:`) with the FR ID in the scope or body, e.g. `feat(hotspot): per-service pool resolution (FR-62, FR-64)`.
- **One logical change per commit.** Migrations, the code that depends on them, and the contract doc that describes them go in the **same** commit — never split across commits that can be checked out independently.
- Every phase ends with a commit containing its written `gate-result.md`.
- Every bug found — at any time, by anyone — gets a row in `docs/ops/known-issues.md` **in the same commit as its fix**, or immediately if it is not being fixed yet.
- **Never commit secrets, licence keys, signing keys, customer data, or generated environment files.** Ship an example env file only. Add a secret-scanning CI job and a pre-commit hook; a leaked key means rotating it, not deleting the commit.
- CI must be green before merge. Define the CI jobs in phase 1: backend build/vet/test against a real database service container, **frontend full build (type-check including test files and bundler resolution — not a bare type-check flag; A8)** plus lint plus the localisation check plus the branding-hardcode check, shell/installer self-tests **including an idempotency run and a clean-OS install run**, the RADIUS harness smoke suite, the vendor-isolation grep gate, the native-control grep gate, secret scanning, and the **release-manifest source-leak audit** of §6.2.

**8.3 Releases**
- Semantic versioning; the version lives in one file that the build stamps into every binary and shows in the panel.
- A version tag triggers the release workflow: build the stripped binaries and the production frontend (no source maps), assemble the signed offline bundle containing everything an offline install needs, run the source-leak manifest audit, sign, attach bundle + signature to a GitHub Release in the **releases** repo, and publish the bootstrap install script there.
- A **pre-tag release checklist** as a mandatory gate: migrations applied cleanly on a restored realistic dataset, backup/restore round-trip verified, update-and-rollback verified, install idempotency verified, source-leak audit passed, signature verified from a clean machine, changelog written.
- **Documentation is a GitHub wiki**, with contextual "learn more" deep links from panel screens that hide gracefully when offline — and that respect the white-label tier (§3.12).

---

## 9. Rules for phase gates (encode these in `00-execution-plan.md`)

1. A phase's gate is a **numbered list of verifiable items**, each with the command or observation that proves it. "Reviewed the code" is not an item.
2. **Anything that needs hardware, a browser, a merchant account, or a real customer dataset must be named as such at kickoff**, with the exact fallback evidence that will be accepted. A phase may not invent a "documented-pending" exemption after the fact.
3. **Migration numbering is one linear sequence across the entire product's life.** Every new migration takes the next free number above the current maximum. Ranges are budgets, never reservations — a lower number is silently skipped on an already-updated database, which is a silent data bug.
4. A phase may not leave a screen, endpoint, or code path that no test ever exercises **through the real HTTP/middleware stack**.
5. Frozen contracts are amended in a later brief, in writing, with both sides updated in the same commit — never silently.
6. Any phase touching auth, money, uploads, the updater, or the release pipeline carries an explicit **security review item** in its gate (§6.1).

---

## 10. Decisions to put to the owner before finalising the plan

Ask these in batches with a recommended default. Do not guess.

| # | Decision | Recommended default |
|---|---|---|
| 1 | **Product name and domain**; and the **white-label entitlement model** — is attribution removed by licence tier, always present, or never present? | Licence-tier entitlement: standard tiers keep a fixed shell/login mark, white-label tier removes it via a signed licence field (§3.12) |
| 2 | **Backend language/runtime** (the existing code is Go; SAMM is Python/FastAPI) | Keep Go — single stripped static binary, no runtime on the host, best fit for the native installer *and* for source protection (§6.2) |
| 3 | **Reuse the existing HikRAD codebase or start from an empty repo?** | Start a fresh private repo; port proven subsystems (RADIUS policy engine, accounting pipeline, vendor adapter, migrations) deliberately rather than importing wholesale |
| 4 | **Accounting design**: durable queue vs in-database accumulation vs both | Durable queue for the loss guarantee + in-DB fast path for limit evaluation; planner must justify |
| 5 | **IPoE / DHCP subscriber support in v1?** | Yes — real market segment, cheap once the service-instance model exists |
| 6 | **IPv6 dual-stack in v1?** | Model it in v1, ship wizard support in v1.1 |
| 7 | **Currency model**: single operator currency (recommended) vs multi-currency | Single book, home currency configurable, entry-time conversion at admin-entered rates |
| 8 | **Online payment gateways in v1?** — only plan the phase if real Stripe/PayPal/Binance credentials exist | Defer to a later phase, gated on credentials |
| 9 | **Support tickets and Telegram bot in v1?** | Tickets yes; Telegram bot v1.1 |
| 10 | **Self-service guest registration in v1?** | Yes — headline hotspot feature |
| 11 | **Full double-entry accounting in v1**, or a simpler append-only ledger first? | Double-entry from day one — retrofitting is far more expensive |
| 12 | **Language set at v1** (en/ar/ku required; tr/fr/es/de/ru/fa optional) | en/ar/ku at v1; adding a language stays a JSON-file operation |
| 13 | **Licence tiers**: names, subscriber caps, which features each tier gates (white-label, self-service caps, API access), pricing model | Four tiers plus unlimited; caps enforced with an overage buffer; white-label as a top-tier entitlement |
| 14 | **Install telemetry** — ship it, opt-in or opt-out? | Ship it opt-out, disclosed in installer output, one toggle away — and disabled by default on white-label installs |
| 15 | **QoS / application-aware shaping and firewall auto-security** — v1 or later? | Later phase; large, and not on the daily path |
| 16 | **ISO appliance** — build it, and when? | After v1, once the native installer is proven in the field |
| 17 | **Target for the first pilot** and the date, because that sets what "v1" actually means | — |

---

## 11. Anti-patterns — mistakes made building HikRAD that must not be repeated

State these in the execution plan so every phase is held to them.

- **A1 — No phase is "complete" with unverified legs.** HikRAD closed several gates with "documented-pending" hardware, browser and migration-rehearsal items that were never revisited. Name every such item at kickoff with its accepted fallback evidence (§9.2), or move it out of the phase.
- **A2 — Migration numbers are a linear sequence, never reserved ranges.** A migration numbered below the database's current version is silently skipped on upgraded installs.
- **A3 — Never build an integration you cannot test end to end.** HikRAD built a full e-wallet gateway layer for gateways it never had merchant accounts for, then deleted all of it. Gate integration phases on credentials existing.
- **A4 — Decide the money model once, up front, with the owner.** HikRAD built per-currency wallets across a whole phase, then reversed it with an irreversible migration one release later. Get §10.7 answered before any billing code is planned.
- **A5 — Build the design system and control library in phase 1, not last.** HikRAD shipped bare native selects/checkboxes/radios everywhere, needed a whole late phase to replace them, then *another* program to modernise the screens. From phase 1: design tokens, a shared control set, a CI gate failing the build on any bare native control outside the library, and label-to-input association **by construction** rather than per call site.
- **A6 — Localisation and branding are phase-1 constraints, not later sweeps.** CI-fatal checks for hardcoded strings *and* hardcoded brand strings/colours/URLs from the first commit. Watch two specific traps HikRAD hit: a key defined as both a string and an object node (dotted lookups dead-end and the UI shows raw key names), and styling against a design token that was never defined (the element renders invisible).
- **A7 — One canonical reader per cross-cutting concern.** HikRAD's branding was read three different ways by three surfaces, all against a settings key that never existed — broken on every install for months. Identity/branding, settings, permissions and money formatting get one resolver each, consumed everywhere. This is also what makes §3.12 white-labelling cheap instead of a rewrite.
- **A8 — The frontend gate must be a full build**, not a bare type-check flag — the bare flag misses test files and bundler resolution.
- **A9 — Keep the document set small and navigable.** HikRAD accumulated ~130 planning documents and a project-instructions file that grew into a wall of prose. Target: one master PRD, ~8–10 sub-PRDs, one execution plan, one brief per phase, a short ops set. Phase briefs record decisions and frozen contracts, **not** narrative history.
- **A10 — Sequential single-agent phases.** The multi-agent parallel role split was planned, tried, and abandoned.
- **A11 — Fix install-critical paths on the *upgrade* path too, not only fresh installs.** HikRAD's logo upload failed with a permission error on every existing install because the fix only touched the fresh-install script.
- **A12 — A permission-denied landing page is a broken login.** Every role, however restricted, must land somewhere usable.
- **A13 — Every bug gets a known-issues row, from day one**, checked before debugging anything.
- **A14 — Global middleware must be verified against every content type it will meet.** HikRAD's file-upload feature was silently rejected by a global JSON-only middleware and never worked in the field, because every test bypassed the real router.
- **A15 — Do not ship a deployment mechanism the operator has to debug.** Docker was the largest single source of install trouble; the native installer must not reintroduce equivalent complexity by other means (no custom package manager, no bespoke process supervisor — use systemd).

---

## 12. Deliverable checklist for the planning session

Before declaring the plan done, verify and show:

- [ ] Master PRD written: overview, problem, personas, goals, non-goals, success metrics, complete FR list with MoSCoW priorities, NFRs, architecture, phasing, risks, decisions log with every §10 answer recorded.
- [ ] Sub-PRDs written, with **every FR owned by exactly one file** and a coverage audit proving no gaps and no double ownership.
- [ ] **Feature-coverage table**: every row of §3 → FR ID → sub-PRD → phase. Zero orphans.
- [ ] Execution plan with phase order, dependencies, migration budget, gate rules (§9), and anti-patterns (§11).
- [ ] One brief per phase with scope, frozen contracts, dependencies, migration numbers, gate checklist, and commit plan.
- [ ] `docs/ops/install.md` per §7 — native only, with the under-30-minutes acceptance item.
- [ ] `docs/ops/security-and-source.md` per §6 — both the product-security posture and the source-protection procedure, including the CI source-leak audit.
- [ ] `docs/ops/release-and-commits.md` per §8.
- [ ] `docs/ops/known-issues.md` template in place.
- [ ] A written **white-label rebranding procedure** for licensed operators (§3.12).
- [ ] A short "what is deliberately NOT in v1" section the owner can sign off on.
