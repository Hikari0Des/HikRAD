# HikRAD — Phase & Agent Runbook (copy-paste commands + prompts)

> Written 2026-07-11. How to launch every agent of every phase and how to close each phase with its integration gate. Phases 1–2 are ✅ complete (kept here for reference/re-runs). All prompts already encode the execution-efficiency protocol from [00-team.md](00-team.md) — don't add extra context to them.
>
> **Mechanics:** open one terminal per agent at the repo root, run `claude`, paste the agent's prompt, let it work to completion. Agents of the same phase run **in parallel** (their paths are disjoint). The gate runs **after all agents finish**, in ONE fresh session. Stack commands (`make up`, harness) run in your WSL2 checkout (Windows bind-mounts break FreeRADIUS/Caddy permissions).

---

## The loop for every phase N

```sh
# 1. Pre-flight (previous gate must be green, tree clean)
git status                      # must be clean; commit anything pending
make test                       # previous phase still green

# 2. Launch agents — one terminal each, at repo root:
claude                          # then paste that agent's prompt from below

# 3. When ALL agents report done → one fresh terminal:
claude                          # paste the phase's GATE prompt from below

# 4. Gate GREEN → close out:
git add -A && git commit -m "Phase N complete: <one-line summary>"
```

---

## Phase 1 — Foundation & Contracts ✅ DONE (gate passed 2026-07-09)

Re-run only if rebuilding from scratch. Agents: `agent-1-platform-security`, `agent-2-radius-nas`, `agent-3-backend-business`, `agent-4-frontend-panel`, `agent-5-frontend-portal` in `docs/phases/phase-1-foundation/`. Use the Phase-3 prompt template below with those file names and `scripts/gate-phase-1.sh`.

## Phase 2 — AAA Core & Lossless Pipeline ✅ DONE (gate passed, verified 2026-07-11)

Same — agents in `docs/phases/phase-2-aaa-pipeline/`: `agent-1-platform-security`, `agent-2-radius-nas`, `agent-3-accounting-monitoring`, `agent-4-backend-business`, `agent-5-frontend-panel`. Verification baseline: [docs/verification-phases-1-2.md](../verification-phases-1-2.md).

---

## Phase 3 — Billing, Security & Monitoring (MVP gate) ⬅ NEXT

Pre-flight extra: none (Phase-2 gate recorded green).

### Agent 1 — Platform & Security (roles, 2FA, sessions, audit viewer)
```text
You are Agent 1 (Platform & Security) for HikRAD Phase 3.
Your complete briefing: docs/phases/phase-3-billing-security-monitoring/agent-1-platform-security.md.
Follow the execution-efficiency protocol in docs/phases/00-team.md: read ONLY your task file, the contracts it cites in docs/phases/phase-3-billing-security-monitoring/00-phase.md, and CLAUDE.md — plus only the specific sub-PRD sections your task file points at.
Rules: stay strictly inside your exclusive paths and migration range 0210–0219; implement frozen contracts, never re-negotiate them (if one is wrong, stop and report in one line); other agents work in this repo concurrently — check git status before writing near an ownership boundary and never touch files outside your paths.
Work to your task file's definition of done, tests green (go test ./...). Append your machine-checkable gate legs to scripts/gate-phase-3.sh (create if missing; POSIX sh; each check prints a label + PASS/FAIL).
Finish with a status note ≤ 20 lines (built / deviations / seams left) written to docs/phases/phase-3-billing-security-monitoring/status-agent-1.md.
```

### Agent 2 — RADIUS & NAS (enforcement worker, hotspot template, debug stream)
```text
You are Agent 2 (RADIUS & NAS) for HikRAD Phase 3.
Your complete briefing: docs/phases/phase-3-billing-security-monitoring/agent-2-radius-nas.md.
Follow the execution-efficiency protocol in docs/phases/00-team.md: read ONLY your task file, the contracts it cites in docs/phases/phase-3-billing-security-monitoring/00-phase.md, and CLAUDE.md — plus only the specific sub-PRD sections your task file points at.
Rules: stay strictly inside your exclusive paths and migration range 0220–0229; vendor neutrality (FR-17) — MikroTik-specific attribute names only in internal/radius/vendor/; implement frozen contracts, never re-negotiate; concurrent agents — check git status near boundaries.
Work to your definition of done, tests green incl. harness where your task says so. Append your machine-checkable gate legs to scripts/gate-phase-3.sh.
Finish with a status note ≤ 20 lines written to docs/phases/phase-3-billing-security-monitoring/status-agent-2.md.
```

### Agent 3 — Accounting & Monitoring (probes, devices FR-60, alerts, dashboard API)
```text
You are Agent 3 (Accounting & Monitoring) for HikRAD Phase 3.
Your complete briefing: docs/phases/phase-3-billing-security-monitoring/agent-3-accounting-monitoring.md (note task 2b: FR-60 monitored devices — same probe engine, second target kind).
Follow the execution-efficiency protocol in docs/phases/00-team.md: read ONLY your task file, the contracts it cites in docs/phases/phase-3-billing-security-monitoring/00-phase.md, and CLAUDE.md — plus only the specific sub-PRD sections your task file points at.
Rules: stay strictly inside your exclusive paths and migration range 0230–0239; implement frozen contracts (incl. the device_down|device_up rule types), never re-negotiate; concurrent agents — check git status near boundaries.
Work to your definition of done, tests green. Append your machine-checkable gate legs (incl. gate item 8's device probe check) to scripts/gate-phase-3.sh.
Finish with a status note ≤ 20 lines written to docs/phases/phase-3-billing-security-monitoring/status-agent-3.md.
```

### Agent 4 — Backend Business (renewals, ledger, balances, vouchers, refunds)
```text
You are Agent 4 (Backend Business) for HikRAD Phase 3.
Your complete briefing: docs/phases/phase-3-billing-security-monitoring/agent-4-backend-business.md.
Follow the execution-efficiency protocol in docs/phases/00-team.md: read ONLY your task file, the contracts it cites in docs/phases/phase-3-billing-security-monitoring/00-phase.md, and CLAUDE.md — plus only the specific sub-PRD sections your task file points at.
Rules: stay strictly inside your exclusive paths and migration range 0200–0209; money tables are append-only (DB-level REVOKE), balances always ledger-derived; the C2 renewal endpoint is THE single money path — every renewal source converges on it; implement frozen contracts, never re-negotiate; concurrent agents — check git status near boundaries.
Work to your definition of done, tests green (incl. the balance≡ledger property test and voucher double-redeem race test). Append your machine-checkable gate legs to scripts/gate-phase-3.sh.
Finish with a status note ≤ 20 lines written to docs/phases/phase-3-billing-security-monitoring/status-agent-4.md.
```

### Agent 5 — Frontend Panel (dashboard, renew dialog, billing/security/alerts UIs)
```text
You are Agent 5 (Frontend Panel) for HikRAD Phase 3.
Your complete briefing: docs/phases/phase-3-billing-security-monitoring/agent-5-frontend-panel.md (note the FR-60 devices section added to task 6).
Follow the execution-efficiency protocol in docs/phases/00-team.md: read ONLY your task file, the contracts it cites in docs/phases/phase-3-billing-security-monitoring/00-phase.md, and CLAUDE.md — plus only the specific sub-PRD sections your task file points at.
Rules: frontend/panel/** only (frontend/shared read-only); no hardcoded user-visible strings — locale JSON en/ar/ku, i18n:check must stay green; CSS logical properties for RTL; consume only the frozen C2/C3/C5/C6/C7 endpoints; concurrent agents — check git status near boundaries.
Work to your definition of done: component tests green (npx vitest run), npm run build, npm run i18n:check, the ≤3-click renew measurement documented. Append lint/build/test/i18n legs to scripts/gate-phase-3.sh.
Finish with a status note ≤ 20 lines written to docs/phases/phase-3-billing-security-monitoring/status-agent-5.md.
```

### Phase 3 — INTEGRATION GATE (one fresh session, after all 5 finish)
```text
You are the integration-gate runner for HikRAD Phase 3.
Read docs/phases/phase-3-billing-security-monitoring/00-phase.md (gate items 1–8) and the five status-agent-*.md notes there. Do not re-read agent task files or sub-PRDs.
1. Run: go vet ./... , go test ./... (backend), npm run lint/build/test/i18n:check --workspaces (frontend).
2. Run scripts/gate-phase-3.sh; write any missing scriptable legs yourself, then re-run.
3. Non-scriptable items: bring the stack up (make up && make seed), use backend/test/harness (smoke + coa-listen) as the NAS, and walk gate items 1, 4, 6 end to end — for item 6 simulate NAS-down by stopping the harness/target; for item 8 use any pingable host as the monitored device. Record evidence (command output, screenshots for UI legs).
4. Fix small integration seams directly (gate-runner exception: you may touch any path). If a frozen contract itself is wrong, STOP and report — that triggers the amend-and-restart rule, not a silent fix.
Deliver: a table of gate items 1–8, each PASS/FAIL with one line of evidence, then a GREEN/RED verdict. If GREEN: update the "Current state" line in CLAUDE.md to include Phase 3, and write the table to docs/phases/phase-3-billing-security-monitoring/gate-result.md.
```

---

## Phase 4 — Portal, Payments & PWA (4 agents — E is not staffed)

Pre-flight extra: Phase-3 gate-result.md must exist and be GREEN.

### Agent 1 — RADIUS & NAS (ROS matrix, CoA hardening, NAS API auto-setup)
```text
You are Agent 1 (RADIUS & NAS) for HikRAD Phase 4.
Your complete briefing: docs/phases/phase-4-portal-payments-pwa/agent-1-radius-nas.md.
Follow the execution-efficiency protocol in docs/phases/00-team.md: read ONLY your task file, the contracts it cites in docs/phases/phase-4-portal-payments-pwa/00-phase.md (esp. C6), and CLAUDE.md — plus only the specific sub-PRD sections your task file points at.
Rules: exclusive paths + migration range 0320–0329; auto-setup is additive-only with mandatory preview and conflict-abort (FR-56); RouterOS API client code only inside the vendor adapter (FR-17); frozen contracts implemented, never re-negotiated; concurrent agents — check git status near boundaries.
Work to your definition of done, tests green. Append your machine-checkable gate legs to scripts/gate-phase-4.sh.
Finish with a status note ≤ 20 lines written to docs/phases/phase-4-portal-payments-pwa/status-agent-1.md.
```

### Agent 2 — Accounting & Monitoring (Web Push, digests, subscriber WhatsApp)
```text
You are Agent 2 (Accounting & Monitoring) for HikRAD Phase 4.
Your complete briefing: docs/phases/phase-4-portal-payments-pwa/agent-2-accounting-monitoring.md.
Follow the execution-efficiency protocol in docs/phases/00-team.md: read ONLY your task file, the contracts it cites in docs/phases/phase-4-portal-payments-pwa/00-phase.md (esp. C4, C7, and C8's billing.card_payment notifications), and CLAUDE.md — plus only the specific sub-PRD sections your task file points at.
Rules: exclusive paths + migration range 0330–0339; delivery isolation — one dead channel never delays another (NFR-7); consume D's billing.renewed and billing.card_payment events for FR-55/FR-59 subscriber messages; frozen contracts implemented, never re-negotiated; concurrent agents — check git status near boundaries.
Work to your definition of done, tests green (WhatsApp path provable against a request-capture fake if Meta onboarding is pending). Append your machine-checkable gate legs to scripts/gate-phase-4.sh.
Finish with a status note ≤ 20 lines written to docs/phases/phase-4-portal-payments-pwa/status-agent-2.md.
```

### Agent 3 — Backend Business (portal API, gateways, scratch cards FR-59)
```text
You are Agent 3 (Backend Business) for HikRAD Phase 4.
Your complete briefing: docs/phases/phase-4-portal-payments-pwa/agent-3-backend-business.md (note tasks 3b PUT /portal/me and 4b scratch-card payments per contract C8).
Follow the execution-efficiency protocol in docs/phases/00-team.md: read ONLY your task file, the contracts it cites in docs/phases/phase-4-portal-payments-pwa/00-phase.md (C1-D, C2, C3, C8), and CLAUDE.md — plus only the specific sub-PRD sections your task file points at.
Rules: exclusive paths + migration range 0300–0309; portal /me exposes consumed data ONLY — no quota total/remaining fields anywhere (Decision 21); IDOR rule absolute — subscriber identity from token only; card codes sealed, never in payloads/logs; every renewal source converges on the Phase-3 renewal path; frozen contracts implemented, never re-negotiated; concurrent agents — check git status near boundaries.
Work to your definition of done, tests green (intent state machine races, callback replay idempotency, trial/approve/reject math for FR-59). Append your machine-checkable gate legs (esp. gate items 2, 6, 10) to scripts/gate-phase-4.sh.
Finish with a status note ≤ 20 lines written to docs/phases/phase-4-portal-payments-pwa/status-agent-3.md.
```

### Agent 4 — Frontend Portal & Localization (portal UI, PWA, scratch-card flow)
```text
You are Agent 4 (Frontend Portal & Localization) for HikRAD Phase 4.
Your complete briefing: docs/phases/phase-4-portal-payments-pwa/agent-4-frontend-portal.md (note tasks 1b account self-care and 3b scratch-card flow).
Follow the execution-efficiency protocol in docs/phases/00-team.md: read ONLY your task file, the contracts it cites in docs/phases/phase-4-portal-payments-pwa/00-phase.md (C2, C3, C4, C5, C8), and CLAUDE.md — plus only the specific sub-PRD sections your task file points at.
Rules: frontend/portal/**, frontend/shared/**, plus ONLY the panel PWA exception paths (frontend/panel/public/**, frontend/panel/src/pwa/**); the portal never displays a quota ceiling or remaining balance (Decision 21); trilingual, true RTL, i18n:check green; test payment flows against D's mock simulator; concurrent agents — check git status near boundaries.
Work to your definition of done: vitest + build + i18n:check green, Lighthouse PWA checks pass for both apps. Append lint/build/test/i18n legs to scripts/gate-phase-4.sh.
Finish with a status note ≤ 20 lines written to docs/phases/phase-4-portal-payments-pwa/status-agent-4.md.
```

### Phase 4 — INTEGRATION GATE
```text
You are the integration-gate runner for HikRAD Phase 4.
Read docs/phases/phase-4-portal-payments-pwa/00-phase.md (gate items 1–10) and the four status-agent-*.md notes there. Do not re-read agent task files or sub-PRDs.
1. Run the full backend + frontend suites.
2. Run scripts/gate-phase-4.sh; add missing scriptable legs (mock-gateway lifecycle incl. 3× callback replay, IDOR/rate-limit scripted attempts, FR-59 API flow: submit→trial→approve/reject math, WhatsApp request-capture fake), re-run.
3. Non-scriptable: stack up, walk item 1 (real phone, Arabic RTL, incl. the FR-44 password-change leg), item 4/5 (PWA install + offline + push on Android), item 7/8 against the ROS matrix targets if available — else record as documented-pending exactly as the brief allows.
4. Fix small seams directly; contract-level problems → STOP and report (amend-and-restart rule).
Deliver: PASS/FAIL table for items 1–10 with one-line evidence each, GREEN/RED verdict. If GREEN: update CLAUDE.md current-state and write docs/phases/phase-4-portal-payments-pwa/gate-result.md.
```

---

## Phase 5 — Reports, Install & License (v1 cut — 4 agents)

Pre-flight extra: Phase-4 gate-result.md GREEN; have a clean Ubuntu VM (or WSL2 distro clone) ready for gate items 1–3.

### Agent 1 — Platform & Security (license, backup/update, install.sh, wizard backend, tunnel)
```text
You are Agent 1 (Platform & Security) for HikRAD Phase 5.
Your complete briefing: docs/phases/phase-5-v1-reports-install-license/agent-1-platform-security.md.
Follow the execution-efficiency protocol in docs/phases/00-team.md: read ONLY your task file, the contracts it cites in docs/phases/phase-5-v1-reports-install-license/00-phase.md (C1-A, C4, C5, C7), and CLAUDE.md — plus only the specific sub-PRD sections your task file points at.
Rules: exclusive paths + migration range 0410–0419; license validation fully offline; grace-expiry makes the panel read-only but NEVER touches RADIUS auth/acct; tunnel off by default and never fronts RADIUS/CoA (NFR-7); frozen contracts implemented, never re-negotiated; concurrent agents — check git status near boundaries.
Work to your definition of done, tests green. Append your machine-checkable gate legs to scripts/gate-phase-5.sh.
Finish with a status note ≤ 20 lines written to docs/phases/phase-5-v1-reports-install-license/status-agent-1.md.
```

### Agent 2 — Accounting & Monitoring (chaos/perf evidence pack)
```text
You are Agent 2 (Accounting & Monitoring) for HikRAD Phase 5.
Your complete briefing: docs/phases/phase-5-v1-reports-install-license/agent-2-accounting-monitoring.md.
Follow the execution-efficiency protocol in docs/phases/00-team.md: read ONLY your task file, the contracts it cites in docs/phases/phase-5-v1-reports-install-license/00-phase.md (C6), and CLAUDE.md.
Rules: exclusive paths (backend/test/chaos/**, backend/test/perf/**, accounting, monitorsvc, docs/evidence/**); the evidence pack must be scripted and reproducible — kill-DB/kill-acct/dup-storm/unclean-host chaos with the counter invariant proven, NFR-1 perf numbers at 5k/2k scale via the harness; concurrent agents — check git status near boundaries.
Work to your definition of done. Append the evidence-pack generation as a leg of scripts/gate-phase-5.sh.
Finish with a status note ≤ 20 lines written to docs/phases/phase-5-v1-reports-install-license/status-agent-2.md.
```

### Agent 3 — Backend Business (reports APIs, CSV import, digests)
```text
You are Agent 3 (Backend Business) for HikRAD Phase 5.
Your complete briefing: docs/phases/phase-5-v1-reports-install-license/agent-3-backend-business.md.
Follow the execution-efficiency protocol in docs/phases/00-team.md: read ONLY your task file, the contracts it cites in docs/phases/phase-5-v1-reports-install-license/00-phase.md (C1-D, C2, C3), and CLAUDE.md — plus only the specific sub-PRD sections your task file points at.
Rules: exclusive paths + migration range 0400–0409; report totals MUST equal ledger sums exactly (property-tested); reports read-only over existing data, all scoped via ScopeFilter; CSV import handles UTF-8 + CP1256 Arabic; frozen contracts implemented, never re-negotiated; concurrent agents — check git status near boundaries.
Work to your definition of done, tests green. Append your machine-checkable gate legs (item 4 property tests, item 5 import dry-run/execute/idempotency) to scripts/gate-phase-5.sh.
Finish with a status note ≤ 20 lines written to docs/phases/phase-5-v1-reports-install-license/status-agent-3.md.
```

### Agent 4 — Frontend Panel (reports/settings/import/wizard UIs, FR-59 queue, v1 polish)
```text
You are Agent 4 (Frontend Panel) for HikRAD Phase 5.
Your complete briefing: docs/phases/phase-5-v1-reports-install-license/agent-4-frontend-panel.md (note tasks 2b NAS auto-setup UI and 2c the FR-59 card-payment verification queue).
Follow the execution-efficiency protocol in docs/phases/00-team.md: read ONLY your task file, the contracts it cites in docs/phases/phase-5-v1-reports-install-license/00-phase.md (C2, C3, C4) plus Phase-4's C8 endpoint shapes, and CLAUDE.md.
Rules: frontend/panel/** only (resume src/pwa/ per F's README); ku untranslated count must reach 0; ≤3-click audit documented with click counts; read-only license mode must not break SSE views; concurrent agents — check git status near boundaries.
Work to your definition of done: vitest + build + i18n:check (0 untranslated, all three locales) green, Lighthouse retained. Append lint/build/test/i18n legs to scripts/gate-phase-5.sh.
Finish with a status note ≤ 20 lines written to docs/phases/phase-5-v1-reports-install-license/status-agent-4.md.
```

### Phase 5 — INTEGRATION GATE (v1 cut)
```text
You are the integration-gate runner for HikRAD Phase 5 — this gate IS the v1 release cut.
Read docs/phases/phase-5-v1-reports-install-license/00-phase.md (gate items 1–8) and the four status-agent-*.md notes. Do not re-read agent task files or sub-PRDs.
1. Full suites + scripts/gate-phase-5.sh (add missing scriptable legs: license-state API transitions, backup/restore/update round-trip, report≡ledger property tests, CSV import legs, evidence-pack generation, tunnel negative checks).
2. Item 1 (M4 rehearsal) needs a human: clean Ubuntu VM, install.sh → wizard → first PPPoE Accept via the harness, timed < 30 min following docs/ops/install-guide.md only — prepare the VM steps and prompt me to run/time it, then record the result.
3. Items 2–3 on VMs/clones as specified; item 7's ASVS checklist and ≤3-click audit compiled from the agents' evidence; item 8 tunnel checks.
4. Fix small seams directly; contract-level problems → STOP and report.
Deliver: PASS/FAIL table for items 1–8 with evidence, GREEN/RED verdict. If GREEN: update CLAUDE.md current-state to "v1 complete", write gate-result.md, and produce a release checklist (tag, evidence pack attached, pilot go-live checklist docs/ops/pilot-checklist.md).
```

---

## After v1

v2 features have their own self-contained kickoff prompts — see [docs/v2/00-v2-index.md](../v2/00-v2-index.md) (paste the prompt from the feature's file into a fresh session; no runbook entry needed until its phase brief is generated).
