# HikRAD — Your 11 Questions, Answered Against the Actual Plan

> Written 2026-07-10, after Phases 1–2 completed. Every answer below is checked against the master PRD, the sub-PRDs, the phase briefs, and the code that already exists — not guessed.
>
> **Updated 2026-07-11 — decisions applied to the repo (master PRD now v1.3):**
> - **Q1 →** Hotspot management (hotspot-only subscriber accounts with full details, multi hotspot/PPPoE servers on one router) is **planned as v2** — it reworks Phase-2-built code (Decision 24). Brief + AI kickoff prompt: [docs/v2/01-hotspot-management.md](v2/01-hotspot-management.md). v1 keeps the PPPoE-first model; anonymous hotspot access via vouchers.
> - **Q2 →** AsiaHawala (Asiacell) and Areeba adapters: **v2** (blocked on merchant accounts, zero core changes needed). *(Superseded 2026-07-17, PRD Decisions 29/30: gateway adapters withdrawn — the brief was removed — and replaced by **manual payment providers**: named providers, per-manager receiving accounts, portal transfer-proof with attachments, 1-day provisional, human review. See [docs/v2/02-manual-payment-providers.md](v2/02-manual-payment-providers.md).)*
> - **Q3 →** Telecom scratch cards are now **in v1 as FR-59** (Decision 22): subscriber submits the card code in the portal → instant **1-day trial internet** → admin manually verifies the card in a panel queue → approve = full renewal, reject = reversal + deactivation, with subscriber notifications at every step. No carrier API needed. Lands in Phase 4 (backend + portal) and Phase 5 (panel verification queue).
> - **Q6 →** Wireless-AP/infrastructure device monitoring is now **in v1 as FR-60** (Decision 23) — it rides the Phase-3 probe engine (`monitored_devices`, `device_down|device_up` alerts, device health cards).
> - **Q7 →** An **execution-efficiency protocol** was added to [docs/phases/00-team.md](phases/00-team.md) (binding for Phases 3–5): agents load only their own task file + cited contracts, scriptable gate items are automated into `scripts/gate-phase-N.sh` by the implementing agent, one session runs the gate, handoffs capped at 20 lines, no contract re-derivation. Phase briefs 3–5 carry matching notes.
> - **Phases 1–2 verified against the code** — accurate, no rework needed: [docs/verification-phases-1-2.md](verification-phases-1-2.md).

---

## Q1 — Is PPPoE/Hotspot management separated or merged? Can I create a Hotspot-only user?

**Current design: merged, PPPoE-first.** Every subscriber you create is a PPPoE account. Hotspot is an *add-on* per subscriber:

- The subscriber form has an `allow_hotspot` toggle (off by default). This is FR-58 / Decision 19, and it's already implemented and enforced in the Phase-2 policy engine ([backend/internal/subscribers/api.go](../backend/internal/subscribers/api.go) has the field; the authorize path rejects Hotspot logins with `service_not_allowed` when the flag is off).
- When the toggle is **on**, the same username/password also works on Hotspot NASes: +1 session beyond the PPPoE limit (max one concurrent Hotspot session), Hotspot usage counts against expiry but **not** the data quota, and speed uses the profile's Hotspot-specific rate (falls back to the main rate).

**You cannot create a Hotspot-only subscriber account in v1 — that user type doesn't exist in the data model.** So to answer your question directly: no, you are not "required to create a PPPoE one" in the sense of extra work — creating a subscriber *is* creating a PPPoE user, one form, and Hotspot is just the checkbox. But there is no "Hotspot-only" mode on that form.

The intended way to serve walk-in / hotspot-only customers in v1 is **vouchers** (FR-22 + FR-18): you generate voucher batches and customers log in at the MikroTik Hotspot login page (branded template, Phase 3) with a voucher code — no subscriber record needed.

**If you want true Hotspot-only subscriber accounts** (named accounts with profiles/expiry that can *only* use Hotspot), that's a real product change: a `service_type` field (`pppoe | hotspot | both`) replacing the current boolean, plus policy-engine and UI changes. It's a modest amendment (Decision log entry + FR-58 rework, best slotted before Phase 3 starts since Phase 3 touches subscriber billing) — say the word and it can be planned in.

---

## Q2 — Does this support ZainCash / Qi / Asiacell / Zain / Areeba payment gateways?

> **Superseded 2026-07-17 (PRD Decisions 29/30):** gateway-API integrations are withdrawn entirely — merchant accounts/API docs never materialized. The replacement is **manual payment providers** ([docs/v2/02-manual-payment-providers.md](v2/02-manual-payment-providers.md)): the owner adds providers by name, subscribers transfer to their manager's account and submit proof with attachments, get 1 day provisional, and the owning manager approves for the full month. The table below is kept as the historical answer.

Per FR-23 and Decision 8, v1 ships a **pluggable `PaymentGateway` interface** (Phase 4, contract C3 — `CreatePayment / VerifyCallback / QueryStatus` per adapter) with these adapters:

| Gateway | Status in the plan |
|---|---|
| **ZainCash** (Zain Iraq's e-wallet) | ✅ Planned, first live adapter by default (Phase 4). "Zain" payments in Iraq = ZainCash, so yes. |
| **Qi (Qi Card)** | ✅ Planned v1 adapter (subject to getting a merchant account — see below). |
| **FastPay** | ✅ Planned v1 adapter. |
| **AsiaHawala / Asiacell** | ❌ Not planned for v1. Addable later as one adapter file — no core changes. |
| **Areeba** (card processor) | ❌ Not planned for v1. Same: addable as an adapter post-v1. |
| **Mock gateway** | ✅ Always ships — full payment lifecycle for demo/CI, so the flow is testable with zero merchant accounts. |

Two important honest caveats from the PRD itself:

1. **Merchant-account reality decides what actually ships live** (Risk #1, Open Question 1): ZainCash/FastPay/Qi require merchant onboarding, and their sandboxes/docs are weak. The plan explicitly allows shipping v1 with *any subset* live — vouchers + manual/agent cash collection guarantee payments are never a launch blocker.
2. The whole point of the interface is that **adding Asiacell/Areeba later is one adapter package** under `backend/internal/billing/gateways/<name>/` implementing three methods, plus config — see Q11 for exactly how you'd do that with AI.

---

## Q3 — Will it support Zain/Asiacell voucher-code (scratch card) payment?

**No, and it's not in the plan.** Two different things are easy to conflate here:

- **HikRAD vouchers (FR-22, Phase 3)** — vouchers *you* (the ISP) generate in the panel: batches of single-use codes tied to a profile, printable/exportable, redeemable at the front desk, in the subscriber portal, or at the Hotspot login page. This *is* the "buy a card at the corner shop" workflow — the shop sells **your** cards instead of telecom cards. ✅ Coming in Phase 3 (generation/redemption) + Phase 4 (portal redemption UI).
- **Telecom scratch cards (Zain/Asiacell airtime)** used as payment for internet — ❌ not supported and realistically can't be: the carriers don't expose public APIs to redeem airtime credit into a third-party merchant. Where this exists it goes through commercial aggregators with per-country contracts. If you ever get access to such an aggregator, it would plug into the same `PaymentGateway` interface as any other adapter — but don't plan around it.

---

## Q4 — Will the frontend stay a generic web UI, or become a modern, easy UI/UX?

Modern UX is literally the product's reason to exist — "beat SAS4 decisively on UX" is the market wedge (PRD §1–2), and it's enforced by requirements, not left to taste:

- **NFR-5:** every daily operator task ≤ 3 clicks from the dashboard; keyboard-first global search; a new front-desk operator productive in one hour.
- **Phase 3** delivers the screens with the most UX weight: dashboard (online count + sparkline, revenue, NAS health cards), the one-click renew dialog, ledger/voucher UIs.
- **Phase 5, Agent 4 is an explicit polish phase:** "≤3-click + empty-state polish (NFR-5)" is a named task and a v1 gate item — not an afterthought.
- The stack (Tailwind + Radix primitives, true RTL, skeleton loading states, live SSE data) is the same foundation modern SaaS panels are built on.

Honest framing: what exists after Phase 2 is a *functional* foundation — correct, fast, RTL-ready, but not yet "wow." The plan guarantees the UX **behaviors** (clicks, search, live data, empty states). If after Phase 5 you want a distinctive visual identity beyond clean-and-usable — a real design system with brand tokens, illustrations, refined data-viz — that's a focused design pass worth doing as its own mini-phase (an AI agent + the `dataviz`/design skills can do this systematically across the panel). Recommended, not currently a numbered phase.

---

## Q5 — Will it have router monitoring with up/down status and alerts?

**Yes — this is a core v1 Must, landing in Phase 3 (the next phase), Agent 3:**

- **FR-34:** every NAS gets an ICMP probe (latency/packet loss) always, plus SNMP (CPU, memory, uptime, port traffic) when you configure a community string; each NAS gets a status page with probe history.
- **FR-36:** alerts engine with rules for **NAS down / NAS back up**, RADIUS failure spikes, accounting backlog, low disk, expiring-users digest, low agent balance — delivered via in-app, **Telegram**, email (SMTP), and **WhatsApp**, with per-rule routing and quiet hours.
- **FR-32:** dashboard shows per-NAS health cards (red with downtime duration when down — PRD key flow 3).
- **FR-35:** the system also monitors *itself* (FreeRADIUS throughput, DB, queue depth, disk) on an admin health page.
- When a NAS comes back, an all-clear fires and a reconciliation pass flags sessions that lost their Stop records — tied into the lossless-accounting guarantees you already built in Phase 2.

---

## Q6 — Will it monitor wireless access points?

**Not in v1.** Monitoring scope (FR-34) covers **NAS devices** — routers that talk RADIUS to HikRAD — plus HikRAD's own health (FR-35). Standalone wireless APs (bridges, CPEs, sector antennas) that aren't RADIUS clients have no home in the current data model; there is no "generic monitored device" entity.

Don't work around it by registering APs as fake NAS entries — that pollutes the NAS table, the wizard, and RADIUS client config.

**The good news:** the Phase-3 monitor service (`hikrad-monitor`) is exactly the machinery needed — ICMP/SNMP probes, hypertable probe history, alert rules. Extending it with a `monitored_devices` table ("infrastructure device: name, IP, SNMP, location, no RADIUS role") is a natural, low-risk post-v1 addition (or a Phase-3 amendment if you want it sooner, since Agent 3 (Accounting & Monitoring) will be building the probe engine anyway that phase — that's the cheapest moment to add it). For a WISP, monitoring the backhaul APs is a very reasonable ask — flag it before Phase 3 kicks off if you want it in.

---

## Q7 — What is the correct way to initialize and finish each phase, step by step?

This is codified in [docs/phases/00-team.md](phases/00-team.md) ("How to run it") and it's what you effectively did for Phases 1–2. The full loop for Phase N:

**Before starting**
1. Confirm the Phase N−1 **integration gate is green** and everything is committed (`git status` clean). Later phases assume earlier gates passed.
2. Read `docs/phases/phase-N-<slug>/00-phase.md` completely: the goal, the **frozen contracts** (API shapes, schema, events), the per-agent path map, the migration number ranges, and the gate checklist.

**Running the phase**
3. Spawn **one coding agent per `agent-M-*.md` file, in parallel** — each task file is a complete standalone briefing. Practically: one Claude Code session per agent (separate terminals, or worktrees if you want harder isolation). Each agent must read: its task file → the sub-PRDs it cites → `CLAUDE.md`.
4. Each agent stays strictly inside its **exclusive paths** and its **migration number range** (e.g. Phase 3 will assign ranges like `02xx` per agent — the phase brief is authoritative). Cross-agent needs go only through the frozen contracts.
5. If a frozen contract turns out to be wrong mid-phase: **stop the affected agents, amend the phase brief with a dated note, restart those tasks.** Never let two agents silently agree on a different shape (this rule saved you in Phase 2 — e.g. Agent 4 delivering the quota view + seeded NAS that Agent 3 flagged).
6. Each agent finishes with its own definition-of-done: tests green (`go test ./...`, `npx vitest run`, `npm run i18n:check`), and a written status note of what it built + what seams it left for others.

**Closing the phase**
7. Merge everything, then run the **integration gate checklist** in `00-phase.md` top to bottom on the real stack (`make up && make seed`, harness runs, end-to-end flows). Every item must pass — the gate is the phase's exit exam.
8. Fix integration seams (this is normal — Phase 2 had several flagged cross-agent gaps that were resolved at the gate).
9. Commit, update `CLAUDE.md`'s "Current state" line, and record a phase-status memory/note. Only then open Phase N+1.

**Your next concrete step:** read [docs/phases/phase-3-billing-security-monitoring/00-phase.md](phases/phase-3-billing-security-monitoring/00-phase.md) and spawn its 5 agents.

---

## Q8 — How will I install it for production, and how do I test the whole thing on my current rig right now?

**Production (this is Phase 5's deliverable — FR-49/50/51, success metric M4: fresh install to first authenticated PPPoE user in < 30 minutes):**
1. Get a clean **Ubuntu 22.04/24.04 LTS** server (4 vCPU / 8 GB RAM / 200 GB SSD covers ~5k subscribers, NFR-3), on-premise at the ISP.
2. Run the single **`install.sh`** — it provisions Docker, generates secrets, brings up the whole Compose stack (Postgres+Timescale, Redis, FreeRADIUS, the three Go services, Caddy with TLS).
3. Open the panel → **first-run wizard**: admin account, ISP branding, license key (offline-validated, bound to the server fingerprint), first NAS (with the copy-paste RouterOS snippet or auto-discovery), first profile.
4. Point your MikroTik at it; scheduled backups (`FR-51`) and the update mechanism preserve data across versions.

Until Phase 5 lands, none of that installer polish exists yet — production deployment before then isn't supported.

**Testing everything on your current rig, today:**
Your machine already runs it (this is how the Phase-2 gate was verified). Recap, including the Windows-specific gotcha:
1. Use **Docker Desktop with the WSL2 backend, and keep the repo + data on the WSL2-native filesystem** (e.g. `~/HikRAD` inside Ubuntu), *not* a Windows-mounted path — Windows bind-mounts break FreeRADIUS/Caddy file permissions (learned the hard way in Phase 1).
2. `make up` — generates `deploy/.env` if missing, builds and starts the full stack.
3. `make seed` — loads demo data (admin login, profiles, subscribers, a NAS).
4. Open the panel (Caddy on localhost), log in with the seeded admin, look at Live Sessions.
5. You don't need a real MikroTik: the **packet harness simulates one** —
   `cd backend && go run ./test/harness -addr 127.0.0.1:1812 -secret testing123` (5-case PAP/CHAP smoke), or `-rate 50 -duration 30s` for a load test. Sessions appear live in the panel as the harness sends accounting.
6. One-command full check: `make -C backend test-harness-smoke` (brings up the stack, seeds, runs the harness); `make test` for the suites.
7. `make down` stops everything; data persists under `deploy/data/`.

---

## Q9 — Will MikroTik discovery work like Winbox, with a fast, detailed setup wizard?

**Yes — and the Winbox comparison is technically exact.** Phase 2 already built the discovery half:

- [backend/internal/radius/discover.go](../backend/internal/radius/discover.go) listens for **MNDP** (MikroTik Neighbor Discovery Protocol) broadcasts — the *same protocol Winbox's Neighbors tab uses* — plus FR-56 allows IP-range scanning for routed networks. Discovery is strictly **read-only**: it never writes anything to a router.
- Discovered routers **pre-fill the FR-14 NAS wizard** (identity, IP, board info), which generates the copy-paste RouterOS config snippet.
- Same Winbox caveat applies: MNDP only sees routers on L2-adjacent networks; for remote sites you use the IP-range scan or add manually.
- You can test it without hardware: `go run ./test/harness -mode mndp-announce -duration 8s` simulates a broadcasting router.

The wizard gets its full power in **Phase 4 (contract C6, FR-56.2–56.4): RouterOS API auto-setup** — you supply router credentials (encrypted at rest), HikRAD shows a **mandatory diff/preview** of exactly what it will add, then applies over the RouterOS API. Hard safety guarantees: **additive-only** HikRAD-scoped entries, never overwrites/deletes existing config, any conflict aborts the entire apply with a report, and apply refuses if the router changed since the preview (hash check). Validated against both ROS 6.49 and 7.x before being enabled per version. The copy-paste snippet always remains as the fallback. The panel UI for this and the first-run wizard polish land in Phase 5.

---

## Q10 — Portal should show consumed data only, not the quota limit ✅ *(PRD amended today)*

You're right that this needed changing — the PRD as written (FR-41.2) specified a "quota remaining progress bar" on the portal home screen, which contradicts what you want. Since Phase 4 hasn't started, this was cleanly amendable, and **it's now done** (2026-07-10, Decision 21 in the master PRD Decisions Log):

- **FR-41 (amended):** the portal shows **consumed data** (plain figure/trend), status, expiry/days left, current speed, usage graphs, payment history, and the subscriber's own subscription/account details — and **never displays the plan's quota ceiling or remaining balance** anywhere. The backend still enforces quotas exactly as before (FR-10 behaviors are untouched); the portal API simply doesn't expose the ceiling (`GET /api/v1/portal/me` reshaped in the Phase-4 contract: `usage:{used_down, used_up, used_total}` — no `total`/`remaining` fields).
- **FR-44 (promoted Could → Should):** the subscriber can log in and **update their own password and contact details**. Password change re-encrypts the RADIUS credential (with a UI warning that the PPPoE login password changes too) and invalidates the policy cache; detail edits are limited to subscriber-safe fields (phone, contact info, language) — never profile/expiry/MAC/status — and are audit-logged. New endpoint `PUT /api/v1/portal/me` added to the frozen Phase-4 contract.

Files updated: `docs/PRD.md` (FR-41, FR-44, Noor persona, Decision 21), `docs/prd/07-subscriber-portal-pwa.md`, `docs/phases/phase-4-portal-payments-pwa/00-phase.md` + agent-3 + agent-4 task files (all with dated amendment notes, per the frozen-contract amendment rule).

---

## Q11 — How do I add new features in the future, the same all-AI way?

The method you used *is* the repeatable pipeline. Two tiers, depending on feature size:

### Big features (new domain, multi-agent — e.g. "reseller tree", "FTTH module")
Rerun the exact machinery that built HikRAD:
1. **Amend the master PRD** — describe the feature to the `project-planner` agent (or `/plan-prd`), interrogating every ambiguity. Output: new FR-xx numbers + a new Decisions Log row (user-confirmed choices only). The Decisions Log is what stops future AI sessions from re-litigating your choices.
2. **Update the owning sub-PRD** (or create a new one) — `/split-prd` / the `prd-splitter` agent keeps the ownership audit intact: every FR owned by exactly one sub-PRD, contracts pinned in its §4.
3. **Plan the execution** — `/plan-team` / `team-architect` produces a new phase folder: `00-phase.md` with **frozen contracts**, disjoint path ownership, fresh migration number ranges, and an integration gate; plus one task file per agent.
4. **Run it** exactly like Q7: parallel agents, frozen contracts, gate at the end.

### Small features (one domain — e.g. "Areeba payment adapter", "AP monitoring", "new report")
One Claude Code session is enough — the architecture was deliberately built so features drop in without touching shared code:
1. Point the session at the **owning sub-PRD** and `CLAUDE.md` and state the requirement. Still add a PRD line + Decision entry first — 5 minutes that keeps the doc chain truthful (today's Decision 21 is the template).
2. The conventions do the rest, and the agent must follow them:
   - New backend domain → one package with an `init()` registering an `httpapi.Module`, one blank-import line in `modules.go` — no shared route file edits.
   - New payment gateway → one package under `billing/gateways/<name>/` implementing the 3-method `PaymentGateway` interface.
   - New alert type / probe → a rule + channel in the Phase-3 alerts engine.
   - Migrations from an unused number range; append-only money/audit tables; `auth.Audit()` on mutations; permission strings `<module>.<verb>`; vendor-specific RADIUS attributes only in `internal/radius/vendor/`; all UI strings in locale JSON (×3 languages, `i18n:check` is CI-fatal).
3. Verify like a phase gate in miniature: `make test`, harness run if RADIUS-adjacent, and an end-to-end pass on the running stack.

**What makes this keep working long-term** (worth protecting): the document chain (PRD → sub-PRD → phase brief → task file) means any future AI session can be handed one file and full context; the Decisions Log prevents drift; frozen contracts + path ownership let sessions run in parallel safely; and CI + the integration gates catch what individual agents miss. Keep those four habits and the "completely with AI" method scales to v2 and beyond.

---

## Quick reference — where each answer lands in the roadmap

| # | Topic | Verdict | When |
|---|---|---|---|
| 1 | Hotspot-only users | v1: merged PPPoE-first model (built); full **Hotspot management** (hotspot-only accounts + multi-service routers) = **v2**, brief in `docs/v2/01` | Phase 2 ✅ / v2 |
| 2 | Gateways | ZainCash ✅ Qi ✅ FastPay ✅ (merchant-account-dependent) in v1; AsiaHawala/Areeba = ~~v2 adapters~~ **withdrawn 2026-07-17** → replaced by manual payment providers, brief in `docs/v2/02-manual-payment-providers.md` (Decisions 29/30) | Phase 4 / v2 |
| 3 | Telecom scratch cards | ✅ **In v1 as FR-59** (Decision 22): manual admin verification + 1-day trial window, offline-capable | Phase 4 + 5 |
| 4 | Modern UI/UX | Core product goal, enforced by NFR-5 + Phase-5 polish gate; optional extra design pass recommended | Phases 3 + 5 |
| 5 | Router up/down alerts | ✅ ICMP/SNMP + alerts (Telegram/WhatsApp/email/in-app) | Phase 3 |
| 6 | Wireless AP monitoring | ✅ **In v1 as FR-60** (Decision 23): `monitored_devices` on the Phase-3 probe engine | Phase 3 |
| 7 | Phase workflow | Gate-checked loop per `00-team.md` + **execution-efficiency protocol** (scripted gates, context discipline) | Ongoing |
| 8 | Install & local testing | Production: Phase-5 `install.sh` + wizard; today: WSL2 + `make up/seed` + packet harness | Phase 5 / now |
| 9 | Winbox-like discovery | ✅ MNDP (same protocol as Winbox) built; API auto-setup with diff/preview in Phase 4; wizard UI polish Phase 5 | Phase 2 ✅ / 4 / 5 |
| 10 | Portal hides quota limit | ✅ **PRD amended today (Decision 21)** — consumed data only + self-service details/password (FR-44 → Should) | Phase 4 |
| 11 | Future features via AI | Same pipeline: PRD amendment → sub-PRD → phase/task files → parallel agents → gate; small features = one session + conventions | Ongoing |
