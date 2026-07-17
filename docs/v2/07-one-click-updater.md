# v2-07 — One-click update from the panel

> Owner request 2026-07-16 (item 1, second half). v1.1 shipped the guided path (Settings > System shows the version and walks the operator through `hikrad update`). This feature makes the button actually do it.

## 1. Problem

The panel runs inside a container that the update replaces — it cannot restart the stack that contains itself. A privileged host-side helper is required, which is why this was deferred: it is real attack surface and real engineering, not a UI tweak.

## 2. Requirements (draft — renumber as FR-6x at kickoff)

### FR-A — Host updater agent
- A small host daemon (`hikrad-updaterd`, installed by install.sh as a systemd unit) listens on a **local unix socket** bind-mounted into hikrad-api (never a TCP port). Its only verbs: `check`, `update`, `status`, `rollback-status`. It shells into the existing battle-tested `hikrad update` path (pre-backup → apply → health-gate → auto-rollback) rather than reimplementing it.
- Socket calls are authenticated with a per-install shared token from `.env` (updater refuses without it), and every invocation is audit-logged with the requesting manager.

### FR-B — Panel flow
- Settings > System gains: "Check for update" (registry/bundle-dir aware once v2-5 lands; before that, `git fetch` dry-run or bundle-dir scan) and an **Update now** button (admin-only, `system.update` permission, double-confirm with the pre-backup notice).
- Live progress: the panel streams updater status over SSE (staging → backup → apply → health-check → done/rolled-back); the UI survives its own container being replaced mid-update (reconnect + version comparison decides success).

### FR-C — Safety
- Concurrent-update lock (one at a time, lock owner recorded); `hikrad update` CLI and the daemon share the same lock.
- The daemon never accepts arbitrary commands/paths — verbs only, no arguments that reach a shell.
- If the API never comes back healthy, the daemon completes the rollback autonomously (it must not depend on the panel surviving).

## 3. Impact map

| Touched | Built in | Change |
|---|---|---|
| `scripts/install.sh`, new `scripts/hikrad-updaterd` | Phases 1/5 (A) | daemon install, systemd unit, socket + token |
| `deploy/compose.yml` | Phase 1 (A) | socket bind-mount into hikrad-api |
| `internal/platform` (or new `internal/updates`) | new | socket client, SSE progress relay, permission + audit |
| Panel SystemSettings | v1.1 | check/update buttons + progress stream |

## 4. Acceptance sketch

- Admin clicks Update now → sees progress → panel reloads on the new version; `hikrad backup list` shows the pre-update backup.
- A deliberately broken image rolls back autonomously with the panel offline; on reconnect the panel reports "rolled back to vX".
- A non-admin manager sees neither button; socket calls without the token are refused; two concurrent update requests → second gets "locked".

## 5. AI kickoff prompt (paste into a fresh Claude Code session at repo root)

```text
You are working in the HikRAD repo. v1 is complete; we are starting v2 phase 7: one-click update from the panel. You work SOLO — no parallel agents; execute sequentially (daemon → API relay → panel UI), committing in reviewable chunks.

Read, in this order and nothing else yet: CLAUDE.md, docs/v2/phases/00-v2-execution-plan.md, docs/v2/07-one-click-updater.md, scripts/hikrad (cmd_update + cmd_backup_now), scripts/install.sh, docs/ops/update.md, frontend/panel/src/pages/settings/SystemSettings.tsx.

Step 1 — Amend the docs (single commit): new FR rows + Decisions Log row in docs/PRD.md, update sub-PRD 01 (this is platform/ops surface), docs/prd/00-index.md.

Step 2 — Create docs/v2/phases/phase-v2-7-one-click-update/00-phase.md with frozen contracts (socket protocol verbs + auth, SSE progress event shape, lock semantics, systemd unit name/paths, new system.update permission string) and the integration gate (clean-VM update via button, broken-image autonomous rollback, token-refusal + lock tests; migration range 0560–0569 if any). Scriptable gate items → scripts/gate-v2-phase-7.sh.

Step 3 — Stop and present the phase brief for my confirmation before writing feature code.

Constraints: the daemon exposes verbs only — no argument ever reaches a shell; unix socket + token, never TCP; reuse the hikrad-update CLI path (do not reimplement backup/rollback); rollback must complete with the panel dead. Panel strings trilingual. Update every doc invalidated (update.md especially); record bugs in docs/ops/known-issues.md.
```
