# Phase v2-7 — One-Click Update From The Panel

Source brief: [docs/v2/07-one-click-updater.md](../../07-one-click-updater.md). Requirements FR-86–88 (PRD Decision 40; sub-PRD [01-platform-install-licensing.md](../../../prd/01-platform-install-licensing.md)). Owned entirely by sub-PRD 01, same no-cross-domain-split precedent as FR-81–83/84.

**Build-order note:** this phase runs immediately, ahead of v2-6 in the execution plan's row order, per this session's explicit instruction (Decision 40). v2-6 (per-manager preferences)'s PRD amendment and phase brief were already committed (Decision 39, `phase-v2-6-preferences/00-phase.md`) but its feature code was never started — it stopped at the standing kickoff protocol's Step 3 (present the phase brief, await owner confirmation) and stays there, deferred, not cancelled — the same posture Decision 38 already established for this exact pair when v2-5 jumped both of them. This phase builds on v2-5's bundle/registry plumbing, as that phase's own dependency note anticipated.

## 1. Problem (restated from the source brief)

The panel runs inside a container that an update replaces — it cannot restart the stack that contains itself. v1.1 shipped a guided screen (Settings > System) that shows the version and walks the operator through `hikrad update` by hand; this phase makes the "Update now" button actually run it, which needs a privileged host-side helper. That helper is real attack surface (it can trigger a privileged host action from a web request) and real engineering (the update itself must survive its own trigger's container dying mid-flight) — not a UI tweak, which is why it was deferred behind v2-5's bundle/registry work.

## 2. Scope for this implementation pass

1. **Host daemon** (`hikrad-updaterd`, new `scripts/hikrad-updaterd`) — a small Go binary (own `backend/cmd/hikrad-updaterd/` package, built and installed as a static binary by `install.sh`/`hikrad update` themselves, *not* shipped as a container image — it must be able to update every container, including any that hosts itself, so it lives on the host). Listens on a unix socket, token-authenticated, four verbs, wraps `hikrad update` as a child process. (C1/C2/C3)
2. **systemd unit** — `hikrad-updaterd.service`, installed/enabled by `install.sh` (both source and bundle delivery modes), `Restart=on-failure`. (C1)
3. **`hikrad-api` relay** — new `internal/updates` module: four `/api/v1/system/update/*` routes, `system.update` permission, SSE progress relay, audit logging. (C4/C5)
4. **Panel** — `SystemSettings.tsx` gains "Check for update" / "Update now" behind the permission gate, double-confirm, live SSE progress, reconnect/poll fallback on stream loss. Trilingual strings in the existing `settings` locale namespace. (C6)
5. **Gate** — `scripts/gate-v2-phase-7.sh`; `gate-result.md`.

Commit in reviewable chunks along the order the kickoff prompt specified: **daemon → API relay → panel UI**, plus a preceding **install.sh/systemd wiring** chunk the daemon depends on, and a closing **docs + gate** chunk — matching the boundary style every prior phase has used (crypto tooling / bash installer / Go binaries got separate commits in v2-5; schema+backend / panel / portal+CSV / gate in v2-6).

## Migration budget

**None anticipated.** This phase adds no schema — the update lock is a file (`flock`), not a row, and "who triggered an update" rides the existing append-only `audit_log` via `auth.Audit` exactly as every other mutating endpoint already does. The repo's current max migration is **0588** (v2-2's tail); v2-6's phase brief has *reserved but not created* 0589–0590. Per the standing linear-numbering rule, if implementation discovers a genuine need for a schema change, it takes the next free number above whatever the actual max is **at that time** — verify first, do not assume 0589 is free (v2-6 may have claimed it by then).

## Frozen contracts

### C1. Daemon binary, socket, and systemd unit

- **Binary:** `backend/cmd/hikrad-updaterd/main.go`, a new Go module command (same build pattern as `hikrad-api`/`hikrad-acct`/`hikrad-monitor`, but compiled as a **host** binary, not a container image — `install.sh`/`hikrad update` build or extract it to `/usr/local/bin/hikrad-updaterd` the same way they already place the `hikrad` CLI wrapper). Bundle mode ships a prebuilt `hikrad-updaterd` binary alongside `scripts/` in the release tarball (C2 of v2-5's bundle layout gains one more file: `scripts/hikrad-updaterd`, a static binary, not a shell script — same directory, different content); source mode builds it with `go build` like any other `cmd/`.
- **Socket:** `$HIKRAD_ROOT/data/updater/updater.sock` (created by the daemon on start, owned by root — the daemon runs as root via systemd, no `User=`, since it must be able to run `hikrad update` itself). **Amended during implementation (2026-07-18):** the directory and the socket file are both `0777`, not `0770` as originally sketched here — the daemon (root) and `hikrad-api`'s container (its own unrelated non-root uid) are never the same uid or group, so a `770` directory would make the container's `connect()` fail with a plain permission error regardless of the token, the exact class of bug already documented for the pre-existing `radius-control` bind mount. Same reasoning as that mount: no secret lives in this ephemeral IPC path, so the shared token (checked per-request, FR-86.2) is the real security boundary, not filesystem permissions — `0777` here is consistent with `radius-control`'s own precedent, not a new exception. See `docs/ops/known-issues.md`.
- **Bind mount:** `deploy/compose.yml`'s `hikrad-api` service gains `${HIKRAD_DATA_DIR:-./data}/updater:/var/run/hikrad-updater` (read-write) — same shape as the existing `radius-control` mount two lines above it. Registry/bundle-mode compose renders identically (C5 of v2-5's renderer touches nothing here — this is a `volumes:` line on an existing service, not a `build:`/`image:` stanza).
- **Unit:** `/etc/systemd/system/hikrad-updaterd.service`:
  ```ini
  [Unit]
  Description=HikRAD host update daemon
  After=network.target docker.service

  [Service]
  Type=simple
  ExecStart=/usr/local/bin/hikrad-updaterd
  EnvironmentFile=%HIKRAD_ROOT%/.env
  Restart=on-failure
  RestartSec=2

  [Install]
  WantedBy=multi-user.target
  ```
  (`%HIKRAD_ROOT%` is a placeholder `install.sh` substitutes with the real path — systemd unit files cannot read shell variables, so `install.sh` writes the resolved unit via a heredoc, same idiom it already uses for `install.meta`.) `install.sh` writes this file, then `systemctl daemon-reload && systemctl enable --now hikrad-updaterd` — idempotent, runs on repair too (same convention as the acct-spill/control-socket/self-signed-cert fixups in §3d/3d1/3d2/3e).
- **Token:** `HIKRAD_UPDATER_TOKEN`, generated by `gen-env.sh` (`openssl rand -base64 32`, same recipe as `HIKRAD_JWT_SECRET`) and written to `.env` alongside the other secrets — read by both the daemon (via its systemd `EnvironmentFile`) and `hikrad-api` (via the existing compose `environment:` block) from the **same** `.env`, so no separate provisioning step exists.

### C2. Socket protocol — verbs, auth, framing

Newline-delimited JSON, one request per line, one or more response lines per request (streaming verbs emit multiple lines before their final line). A connection handles exactly one request; the caller closes and reopens for the next (no multiplexing — simplicity over throughput, this is an operator-triggered, once-in-a-while protocol).

**Every request:**
```json
{"verb": "check|update|status|rollback-status", "token": "<HIKRAD_UPDATER_TOKEN>", "bundle_path": "..."}
```
`bundle_path` is present only for `update`, optional (omitted = registry/source mode, matching `hikrad update`'s own no-flag default). A missing/wrong `token` gets exactly one line back:
```json
{"ok": false, "error": "unauthorized"}
```
and the connection is closed immediately — before the lock is touched, before any subprocess is considered (FR-86.2, FR-88.1).

**`bundle_path` validation (FR-88.1's "no argument ever reaches a shell", applied to this one field):** must resolve (after `filepath.Clean`) to a path inside `$HIKRAD_ROOT/incoming/` and match `^hikrad-v[0-9]+\.[0-9]+\.[0-9]+\.tar$` by basename. A path failing either check is refused with `{"ok": false, "error": "invalid bundle_path"}` and never reaches `exec.Command`. The daemon invokes `hikrad update` (and, when `bundle_path` is set, `--bundle <path>`) via `exec.Command("hikrad", "update", "--bundle", bundlePath)` — an argv slice, never `sh -c` or string interpolation — so even a validated path can never be reinterpreted as shell syntax.

- **`check`** → one line:
  ```json
  {"ok": true, "current_version": "v2.6.0", "available_version": "v2.7.0", "delivery_mode": "bundle", "bundle_path": "/opt/hikrad/incoming/hikrad-v2.7.0.tar"}
  ```
  `available_version`/`bundle_path` are `null` when nothing newer is found. Source is a scan of `$HIKRAD_ROOT/incoming/` for the highest-semver `hikrad-v*.tar` filename greater than the repo-root `VERSION` file's value — **never** a network call (`git fetch`/registry probe are explicitly out of scope for `check`, matching NFR-7: production is bundle-primary post-v2-5, and a dev-mode `git fetch`-based check stays a manual CLI-only path, not wired into this daemon).
- **`update`** → a stream of progress lines, each one JSON object, followed by exactly one terminal line:
  ```json
  {"event": "stage", "stage": "backup", "ts": "2026-07-18T12:00:00Z"}
  {"event": "stage", "stage": "apply", "ts": "..."}
  {"event": "stage", "stage": "health_check", "ts": "..."}
  {"event": "result", "ok": true, "version": "v2.7.0", "message": "Update complete and healthy."}
  ```
  or, on failure:
  ```json
  {"event": "stage", "stage": "rolling_back", "ts": "..."}
  {"event": "result", "ok": false, "version": "v2.6.0", "message": "update rolled back to the previous version. If a migration partially applied, run: hikrad restore <archive>"}
  ```
  Stage names are derived from `hikrad update`'s own log lines (`cmd_update`'s `log "..."` calls) via line-prefix matching, not a new signal `cmd_update` has to emit — the daemon greps its child's stdout for known substrings (`"Pre-update backup"` → `backup`, `"Verifying release bundle"`/`"Building images"` → `apply`, `"hikrad-api did not become healthy"`/`"Applying update"` → `health_check`, `"rolling back"` → `rolling_back`, `"Update complete and healthy"` → the `result` line) — see C3 for why this is the deliberate seam rather than modifying `cmd_update`.
  If the request holding the connection disconnects mid-update (the expected case — the panel's own `hikrad-api` container may be replaced while its relay connection is open), **the child process is not killed**: `update` runs detached from the request's lifetime once started (FR-86.5) — a lost connection only means the caller stops *watching*, never that the daemon stops *doing*. A caller that reconnects uses `status`/`rollback-status`, not a second `update`, to resume watching (and a second `update` while one is in flight hits the lock, C3).
- **`status`** → one line, the daemon's current in-memory state (also durable across a daemon restart via a small state file `$HIKRAD_ROOT/data/updater/state.json`, written on every stage transition):
  ```json
  {"ok": true, "locked": true, "lock_owner": "cli|panel:<manager-email>", "stage": "idle|backup|apply|health_check|rolling_back|done|rolled_back", "started_at": "2026-07-18T12:00:00Z"}
  ```
- **`rollback-status`** → one line, the outcome of the most recently *completed* action (persists until the next `update` starts):
  ```json
  {"ok": true, "last_action": "update", "result": "success|rolled_back|failed", "version": "v2.7.0", "completed_at": "2026-07-18T12:05:00Z"}
  ```

### C3. Lock semantics (FR-86.4, FR-88.2)

A single `flock`-based advisory lock file at `$HIKRAD_ROOT/data/updater/update.lock`, held for the duration of one `hikrad update` invocation, acquired by **both**:
- `scripts/hikrad`'s `cmd_update` itself, wrapping its existing body (`flock -n "$lockfile" -c '...'` or the Go daemon's equivalent `flock(2)` call — implementer's choice of shell vs. syscall, the file path and non-blocking semantics are what's frozen) — a CLI-triggered update takes the same lock a daemon-triggered one would.
- The daemon's `update` verb, **before** spawning the `hikrad update` child.

Non-blocking acquisition (`LOCK_EX | LOCK_NB`): a caller that cannot acquire immediately is refused, never queued:
```json
{"ok": false, "error": "locked", "lock_owner": "cli", "started_at": "2026-07-18T12:00:00Z"}
```
This is why the daemon does not itself need to reimplement `cmd_update`'s lock — it acquires the *same* file lock `cmd_update` acquires, so whichever process (bare CLI or daemon-spawned CLI) gets there first genuinely excludes the other at the OS level, not just inside one process's memory. The lock is released when the `hikrad update` child exits (success, failure, or after rollback completes) — never held across daemon restarts by anything other than the still-running child holding its own fd (a crashed daemon does not orphan the lock past the child's own lifetime).

### C4. `hikrad-api` relay routes (`internal/updates`, new module)

New module registered the standard way (`Add(m Module)` in its `init()`, one blank import added to `backend/cmd/hikrad-api/modules.go`). All four routes require `auth.Require("system.update")` middleware (same pattern every other permission-gated route already uses).

- **`GET /api/v1/system/update/check`** → dials the socket, sends `check`, returns the daemon's JSON body as-is (`200`).
- **`POST /api/v1/system/update`** → body `{"bundle_path": "..."}` (optional). Dials the socket, sends `update`, and **returns `202 Accepted` immediately** with `{"status": "started"}` once the daemon's first line confirms the lock was acquired (or `409 {"error": {"code": "locked", ...}}` if the daemon's first line is the lock-refusal from C3 — the HTTP call does not block for the whole update). The relay keeps its own socket connection open in the background to buffer progress for C4's SSE route to attach to.
- **`GET /api/v1/system/update/stream`** (SSE) — same framing convention as the existing `internal/live` SSE feed (`Content-Type: text/event-stream`, frames `event: <name>\ndata: <payload>\n\n`, a `: ping\n\n` heartbeat comment on an idle ticker): re-emits each daemon `update` progress line as `event: progress`, and the terminal line as `event: done` (`ok:true`) or `event: rolled_back` (`ok:false`). If no update is in flight when a client connects, the daemon's `status` verb backfills one synthetic `progress` frame with the current state before falling through to heartbeat-only, so a page freshly loaded mid-update sees where things stand instead of an empty stream (mirrors `internal/live/sse.go`'s own snapshot-then-deltas shape).
- **`GET /api/v1/system/update/status`** → relays `status` and, when `stage` is `idle`, additionally folds in `rollback-status`'s fields — this is the single endpoint the panel polls on reconnect (FR-87.2) to answer "did the thing I lost the stream for actually finish, and how."

Every relay call other than `check`/`status`/`stream` (i.e., the `POST`) is audit-logged via `auth.Audit(ctx, "update", "system", "", nil, map[string]any{"bundle_path": ..., "result": ...})` — one entry when the update is requested (before/after showing the requested version), matching FR-87.3. `check`/`status`/`GET` calls are read-only and are not audited, consistent with `auth.Audit` being called only by mutating endpoints elsewhere in the codebase.

### C5. Permission string

`system.update` — a new permission string, added to nothing in `rolePermissions` (`backend/internal/auth/permissions.go`): the built-in `admin` role already grants every permission via its wildcard (`roleCan`'s `role == RoleAdmin` short-circuit), and no other built-in role (`operator`, `agent`) is granted it, matching FR-87.1's "admin-only by default." A tenant that wants to grant it to a custom DB-backed role can already do so through the existing role-editor (`role_permissions` table) with zero code change — same mechanism every other permission string uses.

### C6. Panel UI (`SystemSettings.tsx`)

- The existing "Check version" / guided-command section is unchanged (still shown as the documented fallback).
- New buttons, rendered only when `auth` includes `system.update` (existing `RequirePerm`/permission-check pattern): **"Check for update"** (calls C4's `check`, shows the result inline — "up to date" or "vX.Y.Z available") and **"Update now"** (disabled until a `check` has found something newer; opens a double-confirm dialog naming the pre-backup notice text, matching the source brief's FR-B).
- On confirm, `POST /system/update`; on `202`, open the SSE stream and render a stage list (backup → apply → health check → done), matching the existing toast/`LoadingState` patterns already used elsewhere in the panel (`SystemSettings.tsx` already imports `ErrorState`/`LoadingState`).
- **Reconnect behavior:** if the SSE connection drops (the expected case — the panel's own container was just replaced) and no `done`/`rolled_back` event was seen, the screen switches to a polling loop (`GET /system/update/status` every 3s, capped retry) until it gets a definitive `stage: done|rolled_back`, then shows a success or "rolled back to vX" banner — it never leaves the operator staring at a dead progress bar (FR-87.2).
- All new strings live in `frontend/shared/locales/{en,ar,ku}/settings.json` (the existing namespace `SystemSettings.tsx` already uses) — no hardcoded strings, `i18n:check` stays green.

## Integration gate

Green when all scriptable legs pass (`scripts/gate-v2-phase-7.sh`) and the human/hardware legs are either exercised or explicitly documented-pending in `gate-result.md` (same sanctioned pattern as v2-5's own gate):

1. **Build & syntax** — `go build ./...` / `go vet ./...` across `backend/` (including the new `cmd/hikrad-updaterd`); `bash -n` over `install.sh`, `scripts/hikrad`, and any new shell glue.
2. **Socket protocol — verbs only, token-gated (C2, FR-86.2/88.1)** — a Go test spins up the daemon against a scratch `$HIKRAD_ROOT`, dials the real unix socket, and asserts: a request with a wrong/missing token gets `{"ok":false,"error":"unauthorized"}` and the connection closes without touching the lock file; a `bundle_path` outside `incoming/` or not matching the `hikrad-vX.Y.Z.tar` shape is refused with `invalid bundle_path` and no subprocess is spawned (assert via a process-count/mtime check on a canary file, not a real `hikrad update` run).
3. **Lock semantics (C3, FR-86.4/88.2)** — two concurrent `update` requests against the same running daemon: exactly one gets past the lock, the second gets `{"ok":false,"error":"locked",...}` within the same test run (no polling/queueing observed). A second leg proves the CLI and the daemon share the *same* lock file path (grep + a live flock test: hold the lock via `flock` from the shell, then assert the daemon's `update` verb is refused too).
4. **No shell-reachable arguments (FR-88.1)** — a grep leg over `backend/cmd/hikrad-updaterd/` asserting every `exec.Command`/`exec.CommandContext` call site passes a literal argv (no `sh`, `bash -c`, or string-built command line feeding it) — mirrors v2-5's license-boot-verification grep pattern (gate item 3 of that phase).
5. **Autonomous rollback survives the daemon (FR-86.5, FR-88.3)** — a test that starts `update` against a deliberately-broken health check (reusing the existing `wait_for_api_health`-fails path `cmd_update` already exercises in its own logic), then **kills the daemon process** while the child `hikrad update` is still mid-flight, and asserts: the child completes its own rollback regardless (proven by polling the stack's image tags / `hikrad-api`'s health after the daemon is dead, not via the daemon at all — the daemon being unreachable is the point of this leg), and a **freshly restarted** daemon's `rollback-status` correctly reports `rolled_back` (proving the state file, not just in-memory state, survived).
6. **Permission gate (C5, FR-87.1/AC-87a)** — DB-gated: a manager without `system.update` gets `403` on all four routes; the admin role (wildcard) passes; a custom role granted `system.update` via the existing role editor also passes (proving the permission is a normal string, not special-cased).
7. **Audit logging (C4, FR-87.3)** — a `POST /system/update` call produces exactly one `audit_log` row naming the requesting manager and the requested version/bundle; `check`/`status`/`stream` calls produce none.
8. **SSE relay shape (C4)** — an integration test against a mocked daemon socket asserts the HTTP SSE stream emits `event: progress` frames matching the daemon's JSON lines verbatim and a terminal `event: done`/`event: rolled_back`, using the same `encodeSSE` framing `internal/live/sse.go` already tests.
9. **Panel/build** — `npm run build`/`lint`/`vitest` green across `frontend/panel`; the update buttons render only behind the `system.update` permission (a vitest asserting the gate, not just visual review); `i18n:check` green (0 hardcoded strings, 0 missing keys across en/ar/ku).
10. **Docs accuracy** — PRD/sub-PRD 01 reflect FR-86–88 (done in this brief's own preceding commit); `docs/ops/update.md` describes the panel-triggered path alongside the existing CLI runbook; `docs/ops/known-issues.md`'s stale "planned: v2 phase 5" row is corrected (done); any new bug found while building this phase gets its own dated row.

**Human/hardware legs (documented-pending is an acceptable gate outcome for these, per the v2-5/Phase-5 precedent — note them explicitly in `gate-result.md`, do not silently skip):**
11. **Clean-VM update via button (source brief §4, acceptance sketch)** — a real install, admin clicks "Update now" in the panel, the panel reloads on the new version after reconnecting, and `hikrad backup list` shows the pre-update backup.
12. **Broken-image autonomous rollback, panel dead throughout** — the same VM, a bundle engineered to fail its health check, triggered via the panel; the panel's own container is forcibly killed mid-update (simulating the real "the update replaced what I was talking to" case) and the rollback still completes and is correctly reported on the panel's next reconnect.

## Open implementation questions for whoever builds this (not blocking, but worth a decision-log entry when resolved)

- **`check`'s bundle-drop directory** (`$HIKRAD_ROOT/incoming/`) does not exist yet anywhere in the codebase — this phase introduces it as the place an operator drops a downloaded `hikrad-vX.Y.Z.tar` before clicking "Update now" (no in-panel upload is specified by the source brief; uploading a multi-GB bundle through the browser is a genuinely separate, larger feature). Document this clearly in `docs/ops/update.md` — it is a new manual step (`scp`/USB the file into `incoming/`) the operator must know about, or "Update now" will simply never find anything to offer.
- **Registry-mode `update`** (no `bundle_path`, pulling `ghcr.io/hikrad/*` by tag) is technically reachable through this daemon's protocol (an `update` request with no `bundle_path` just calls `hikrad update` with no `--bundle`, which per FR-82.3/v2-5 is a vendor/dev-only path) — worth an explicit decision at build time on whether the panel UI ever surfaces this option to a real customer, or whether `check`/`update` from the panel are bundle-only in practice (recommendation: bundle-only, consistent with v2-5's "signed bundle is the sole customer-facing path").
