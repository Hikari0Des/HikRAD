# HikRAD — Getting Started: Machine Setup & Phase-by-Phase Walkthrough

> How to go from "docs-only repo" to running code using Claude Code and the execution plan in [docs/phases/](phases/00-team.md). Written for a solo developer on Windows 11, 2026-07-08.

---

## Part 1 — Prerequisites (install once)

Your machine builds and runs the whole stack locally in Docker (the same Compose stack that later installs on the ISP's Ubuntu server). You need:

### 1.1 Required software

Run these in **PowerShell (as Administrator)**:

| # | Tool | Why | Install command |
|---|---|---|---|
| 1 | **Git** | Version control — the phase workflow is merge-based | `winget install Git.Git` |
| 2 | **WSL2** | Docker Desktop's engine; also the closest thing to the Ubuntu target | `wsl --install` (reboot when prompted) |
| 3 | **Docker Desktop** | Runs Postgres+Timescale, Redis, FreeRADIUS, Caddy, and the Go services | `winget install Docker.DockerDesktop` |
| 4 | **Go 1.22+** | Backend language — build/test outside Docker for speed | `winget install GoLang.Go` |
| 5 | **Node.js 20 LTS** | Frontend (React panel + portal, npm workspaces) | `winget install OpenJS.NodeJS.LTS` |
| 6 | **GNU Make** | The repo's task runner (`make up`, `make seed`, `make test`) | `winget install ezwinports.make` |

After installing, **close and reopen** your terminal, then verify:

```powershell
git --version; docker --version; go version; node --version; make --version
```

Start Docker Desktop once and confirm it says "Engine running" (Settings → General → "Use WSL 2 based engine" should be on).

### 1.2 Docker Desktop settings that matter

- **Resources → WSL integration**: enable for your default distro.
- **Resources → Memory**: give it at least **6 GB** (the stack + Timescale + chaos tests are memory-hungry; 16 GB total machine RAM is comfortable).
- UDP ports 1812/1813/3799 are published by the stack for RADIUS/CoA — nothing to configure now, but don't run another RADIUS server locally.

### 1.3 Optional (needed at later phase gates, not day one)

| Tool | Needed when | Notes |
|---|---|---|
| **MikroTik RouterOS CHR** (Cloud Hosted Router VM in Hyper-V/VirtualBox) | Phase 2 gate (validate the generated RouterOS config on a real ROS), Phase 4 (ROS 6.49 vs 7.x matrix) | Free license tier is enough. Until then, the packet harness simulates a NAS. |
| **A clean Ubuntu 24.04 VM** | Phase 5 gate (the timed `install.sh` < 30 min rehearsal, backup/restore to a second VM) | Hyper-V quick-create or VirtualBox. |
| **Telegram bot token** | Phase 3 gate (NAS-down alert to Telegram) | Create via @BotFather, 2 minutes. |

### 1.4 One-time repo setup

The project folder is not a git repository yet. In a terminal at `f:\Coding\HikRAD`:

```powershell
git init
git add .
git commit -m "Planning docs: PRD, sub-PRDs, phased execution plan"
```

Optionally create a private GitHub repo and push — recommended, because each phase ends with a merge and you'll want history/rollback:

```powershell
gh repo create hikrad --private --source . --push   # requires GitHub CLI (winget install GitHub.cli)
```

---

## Part 2 — What "spawn 5 parallel agents" actually means

Each file `docs/phases/phase-N-*/agent-M-*.md` is a **complete, standalone work order**: mission, exact file paths the agent may touch, ordered tasks, the API contracts it must honor, and a definition of done. "Spawn one agent per task file" means: **start one Claude Code session per file and tell it to implement that file.** Nothing more exotic than that.

Because the plan guarantees each agent's paths don't overlap and all shared interfaces are frozen in the phase's `00-phase.md`, the sessions can't collide — so they *may* run at the same time. You have three ways to do it, pick per your comfort:

| Mode | How | Pros / cons |
|---|---|---|
| **Sequential** (recommended to start) | One Claude Code session; do agent task 1, review, commit; `/clear`; do task 2; … | Simplest, easiest to review, no coordination. Slower wall-clock — fine for Phase 1. |
| **Parallel sessions** | Open 2–5 VS Code windows / terminals on the same folder, one Claude Code session each, give each a different task file. Commit per agent when done. | True parallelism; safe because paths are disjoint. You must babysit several sessions and approve their permission prompts. |
| **Subagents** | In one session, ask Claude: *"Spawn subagents in parallel, one per Phase-1 task file, each implementing its file exactly."* | Hands-off, but each subagent starts cold and burns significant usage; harder to review mid-flight. Try only after a sequential phase went well. |

If you use parallel sessions and want extra isolation, put each agent on its own git branch (`git switch -c phase1/agent-3`) and merge all branches at the end of the phase — but with disjoint paths, working on one branch is also fine.

**The rhythm for every phase is identical:**

```
run agent tasks (any order / in parallel)  →  review + commit each
        →  run the integration gate checklist from 00-phase.md
        →  fix failures  →  commit ("Phase N gate passed")  →  next phase
```

The **integration gate** is the numbered checklist at the bottom of each phase's `00-phase.md` (Phase 1 has 6 items). It defines "the phase is actually done" — merged and working end to end, not just "each agent finished typing." Never start Phase N+1 with a red gate.

---

## Part 3 — Step-by-step walkthrough

Prompts below are literal — paste them into Claude Code. Adjust file names per phase from the tables in [00-team.md](phases/00-team.md).

### Step 0 — Session hygiene (applies to every step)

- Start each agent task in a **fresh context**: new session or `/clear` first. The task files are written to be the agent's *only* briefing on purpose.
- After each task finishes: skim `git diff --stat`, run the tests it claims to have written, then commit:
  ```powershell
  git add . ; git commit -m "Phase 1 / Agent 3: API framework, core schema, seed"
  ```
- If an agent says a **frozen contract is wrong or missing something**: stop, edit the phase's `00-phase.md` yourself (or ask Claude to propose the amendment), commit the amendment, then restart the affected task. Never let two agents silently improvise different fixes.

### Step 1 — Run Phase 1 (Foundation & Contracts)

Recommended order if sequential (dependencies flow best this way): **Agent 1 (platform) → Agent 3 (backend) → Agent 2 (radius) → Agent 4 (panel) → Agent 5 (portal/shared)**. In parallel mode, order doesn't matter.

For each of the five files, paste this prompt (changing the file name):

```
Read docs/phases/phase-1-foundation/00-phase.md (the frozen contracts and path
ownership) and then docs/phases/phase-1-foundation/agent-1-platform-security.md.
Implement that agent task exactly as specified: only create/modify files within
its declared ownership, honor every frozen contract, and complete its
Definition of done including the tests. If a contract seems wrong or
incomplete, stop and tell me instead of improvising.
```

Repeat with `agent-2-radius-nas.md`, `agent-3-backend-business.md`, `agent-4-frontend-panel.md`, `agent-5-frontend-portal.md`.

### Step 2 — Close the Phase 1 gate

Fresh session, then:

```
Read docs/phases/phase-1-foundation/00-phase.md and execute its Integration
gate checklist item by item on the current repo state: bring the stack up,
run the seed, run the harness smoke test, verify panel and portal in all three
locales, and run the full CI test suite locally. Report each gate item as
pass/fail with evidence, and fix whatever fails.
```

Two gate items need **you**, not Claude: open `https://localhost/` in a browser, log in as the seeded admin, and switch the language to Arabic to eyeball the RTL flip; do the same for `/portal`. When all 6 items pass:

```powershell
git add . ; git commit -m "Phase 1 gate passed: stack up, static Access-Accept, trilingual shells"
```

### Step 3 — Update CLAUDE.md (once, after Phase 1)

The root [CLAUDE.md](../CLAUDE.md) lists commands as "planned". Prompt:

```
Phase 1 is merged. Update CLAUDE.md: replace the planned commands section with
the real, verified commands, and correct anything the scaffold contradicts.
```

### Step 4 — Phases 2 through 5: same loop

For each phase, the identical pattern with that phase's folder:

| Phase | Folder | Agents (task files) | Gate needs from you |
|---|---|---|---|
| 2 — AAA & pipeline | `phase-2-aaa-pipeline/` | 5 files | Nothing external (harness simulates the NAS); a real MikroTik/CHR validates the wizard snippet if you have one |
| 3 — Billing, security, monitoring | `phase-3-billing-security-monitoring/` | 5 files | Telegram bot token in settings for the alert gate item; unplug/stop the simulated NAS to trigger it |
| 4 — Portal, payments, PWA | `phase-4-portal-payments-pwa/` | 4 files | A real Android phone on your LAN for the PWA install/push gate items; CHR for the ROS matrix; e-wallet merchant creds only if you have them (mock gateway is the gate default) |
| 5 — v1: reports, install, license | `phase-5-v1-reports-install-license/` | 4 files | The clean Ubuntu VM for the timed install rehearsal + a second VM for restore; ideally a person other than you following the install guide |

Phase-2 example prompt (same shape as Phase 1):

```
Read docs/phases/phase-2-aaa-pipeline/00-phase.md and then
docs/phases/phase-2-aaa-pipeline/agent-3-accounting-monitoring.md.
Implement that agent task exactly as specified... (rest identical to Step 1)
```

And each phase ends with the same gate prompt pointed at that phase's `00-phase.md`, followed by a "Phase N gate passed" commit.

### Step 5 — When something goes wrong (it will)

- **A gate item keeps failing:** paste the failing item + error output into a fresh session and ask Claude to diagnose and fix it — that's normal integration work, budget time for it after every phase.
- **Two agents produced incompatible code despite the contracts:** the contract was under-specified. Amend `00-phase.md`, commit, re-run the *smaller* of the two affected tasks with the amended brief.
- **You want to change scope:** change the PRD layer first (master or sub-PRD), then the phase files, then the code — same order of truth as [CLAUDE.md](../CLAUDE.md) describes. Don't patch code against the docs.

---

## Part 4 — Practical tips

- **Permission prompts:** the first sessions will ask to approve `docker`, `go`, `npm`, `make` commands constantly. Approve with "always allow" for the safe read/build/test ones, or ask Claude to run `/fewer-permission-prompts` style setup once the repo has real activity.
- **One phase ≈ multiple long sessions.** Don't try to jam a whole phase into one context window; the plan is deliberately chunked so `/clear` between tasks costs you nothing.
- **Review discipline beats speed.** The two things worth actually reading in every diff: migrations (schema is expensive to unwind) and anything under `internal/billing` or the accounting pipeline (money and the zero-loss claim).
- **Windows note:** all runtime pieces live in Docker, so Windows vs Linux mostly doesn't matter — but if `make` or shell-script friction annoys you, cloning the repo inside WSL2 (Ubuntu) and running Claude Code there is the closest match to the production target. Either environment works with this walkthrough.
