# Phase v2-7 — One-Click Update From The Panel — Integration Gate Result

Run date: 2026-07-18. Executed **solo + sequential** per Decision 25 / the v2
execution plan, re-ordered ahead of v2-6 by explicit owner request (Decision
40) — v2-6's docs were already committed but its feature code was never
started, and it stays deferred at that same point (see this phase's
`00-phase.md` header and PRD Decision 40).

Verification environment: `go build`/`go vet` ran repo-wide; DB-gated legs
(this package's own `internal/updates` permission tests, and a full
`go test ./...` regression pass) ran against real throwaway Postgres 16
(TimescaleDB image) + Redis 7 containers, not just self-skipped — set up
specifically to avoid the "unit suite self-skips DB tests and hides real
bugs" trap this repo has hit before. The daemon itself (`backend/cmd/
hikrad-updaterd`) was additionally **cross-compiled for linux/amd64 from
this Windows dev box and smoke-tested as a real binary inside a real WSL2
Ubuntu shell** — a real unix socket, a real `check` request/response
round-trip, and a real wrong-token refusal — not only the Go test suite's
in-process coverage. `install.sh`'s new steps were verified the same way:
a real (non-sandboxed) root shell in WSL2, three scenarios (no Go
toolchain present, a prebuilt binary present, and an idempotent repair
re-run), plus the `HIKRAD_UPDATER_TOKEN` backfill logic verified in
isolation.

`scripts/gate-v2-phase-7.sh`: **all scripted legs PASS, 0 FAIL** (run both
with and without `HIKRAD_TEST_DB_URL`/`HIKRAD_TEST_REDIS_URL` set — 25/25
checks green either way, the DB-gated permission legs simply self-skip
without them rather than failing).

## Gate items 1–10 (scriptable)

| # | Item | Result | Evidence |
|---|---|---|---|
| 1 | **Schema & build health** | **PASS** | `go build ./...` / `go vet ./...` clean repo-wide (not just the new package); `bash -n` clean on `install.sh`, `scripts/hikrad`, `scripts/build-release-bundle.sh`. No migration was needed (state lives in a lock file and the existing `audit_log`, not a new table) — confirmed the repo's max migration is still 0588, unchanged. |
| 2 | **Socket protocol — verbs only, token-gated (C2, FR-86.2/88.1)** | **PASS** | `TestUnauthorizedRefused` (wrong token on all four verbs, refused before the lock file is even created — asserted directly) and `TestBundlePathValidation` (traversal, outside-`incoming/`, wrong filename shape, and a shell-metacharacter-laden path all rejected as `invalid bundle_path` without a child ever spawning) both pass. Additionally smoke-tested against the **real cross-compiled Linux binary** in WSL2: a real `check` request round-trips correctly, a wrong token gets `{"ok":false,"error":"unauthorized"}` — not simulated. |
| 3 | **Lock semantics (C3, FR-86.4/88.2)** | **PASS** | `TestConcurrentUpdateLock` (two requests to the same daemon instance — the in-memory fast path — exactly one proceeds, the second gets `locked` naming the first requester) and `TestLockConflictDetectedFromChildOutput` (a *fresh* daemon instance with no in-memory state at all still correctly reports `locked` by scanning the child's own flock-failure output — the case that makes the guarantee survive a daemon restart) both pass. `scripts/hikrad`'s `cmd_update` now acquires the identical lock file path (`data/updater/update.lock`) as its very first action, confirmed by grep and by code review of both sides. |
| 4 | **No shell-reachable arguments (FR-88.1)** | **PASS** | Grep leg finds zero `exec.Command("sh"\|"bash", ...)` or string-concatenated `exec.Command` calls in `backend/cmd/hikrad-updaterd/` — the only `exec.Command` call site passes a literal argv slice. |
| 5 | **Autonomous rollback survives the daemon dying (FR-86.5/88.3)** | **PASS** | `TestRollbackSurvivesDaemonDeath` spawns the real daemon as a genuinely separate OS process, kills it mid-update, and confirms the child (standing in for `hikrad update`) keeps running to completion unattended. `TestRollbackStatusPersistsAcrossRestart` goes further: after the daemon is killed and the orphaned child finishes on its own, a **freshly started** daemon instance's `rollback-status` correctly reports the real outcome — proving the state-file reconciliation logic (`reconcileIfStale`, comparing the live `VERSION` file against the run's recorded previous/target version), not just in-memory state. |
| 6 | **`system.update` permission gate (C5, FR-87.1/AC-87a)** | **PASS** | Registration confirmed (module registry, all four routes behind `auth.Require("system.update")`). DB-gated: an `operator` manager gets `403` on all four routes and a no-token request gets `401` (distinguished, not conflated); `admin` (wildcard) and a plain `operator` granted `system.update` through the **existing** `manager_permission_overrides` mechanism both pass — proving it's an ordinary permission string, not admin-special-cased. Verified against real Postgres/Redis, not skipped. |
| 7 | **Audit logging (C4, FR-87.3)** | **PASS** | `POST /system/update` calls `auth.Audit`; `check`/`status`/`stream` (read-only) do not. |
| 8 | **SSE relay shape (C4)** | **PASS** | `TestSSERelayShape` drives `emitDaemonEvent` through a real `httptest.ResponseRecorder`/`http.Flusher` and asserts a `stage` Event becomes `event: progress`, `result{ok:true}` becomes `event: done`, `result{ok:false}` becomes `event: rolled_back` — the exact C4 mapping. |
| 9 | **Panel/build** | **PASS** | `SystemSettings.tsx` gates the new section on `PERM_SYSTEM_UPDATE` (`auth/permissions.ts`'s established `can()` pattern, not a magic string). `npm run build`/`lint`/`test`/`i18n:check` all green for the panel workspace (70 vitest tests, 0 lint errors, 0 hardcoded/missing i18n keys across en/ar/ku — the pre-existing `common.productName` brand-name note is unrelated); full `shared`+`panel`+`portal` workspace build also green. |
| 10 | **Docs accuracy** | **PASS** | PRD/sub-PRD 01 carry FR-86–88 (Step 1 commit, before any code). `docs/ops/update.md` gained a full section on the panel-triggered path, including the "existing installs need one `install.sh` run to provision it" caveat. `docs/ops/install-guide.md` updated. `docs/ops/known-issues.md`'s stale "v2 phase 5" mislabel is corrected, and every bug found during this phase (below) is dated and recorded. |

## GREEN / RED verdict

**GREEN — 10/10 scripted items.** Two human/hardware legs (below) are
documented-pending, same sanctioned pattern as v2-5's own gate.

## Bugs found and fixed

- **Updater socket directory was drafted `0770` root-owned — would have
  broken every real install.** The daemon runs as root (systemd, no
  `User=`); `hikrad-api`'s container connects as its own unrelated
  non-root uid. `770` grants the container no path to the socket at all,
  so every relay call would fail with a plain OS permission error
  regardless of the token. Caught during implementation, before the
  wiring commit, by recognizing it as the same class of bug already
  documented on this page for the pre-existing `radius-control` bind
  mount. Fixed to `0777` (directory in `install.sh`, socket file itself in
  `main.go` right after `net.Listen`) — same reasoning that mount already
  establishes. Phase brief C1 amended in place.
- **`compose.yml` originally required `HIKRAD_UPDATER_TOKEN` hard
  (`:?...`) — would have broken every real upgrade.** Production installs
  upgrade via `hikrad update`, which never re-runs `install.sh`; an
  existing install's `.env` has no such key, so `hikrad-api` would refuse
  to boot entirely over a convenience feature. Fixed: the env var is
  soft-optional in `compose.yml` (the relay reports "not configured"
  instead), and `install.sh` backfills a real token into an existing
  `.env` — idempotent, verified in isolation (append-once, stable across
  repeated runs) and end-to-end in a real WSL2 root shell against a
  synthetic pre-v2-7 `.env`.
- **The release-bundle builder never included `hikrad-updaterd` at all.**
  `install.sh` looks for a prebuilt binary at `$SCRIPTS_SRC_DIR/
  hikrad-updaterd` in bundle mode, but nothing in `scripts/
  build-release-bundle.sh` ever put one there — a real customer bundle
  would have silently shipped without the daemon, with `install.sh`
  falling through to its "no Go toolchain, no prebuilt binary" NOTE
  branch on every bundle-mode install (source-mode installs were
  unaffected, since they build it themselves). Found while checking
  whether `release-checklist.md` needed a matching update. Fixed:
  `build-release-bundle.sh` now cross-compiles `hikrad-updaterd` for
  linux/amd64 (`CGO_ENABLED=0`, matching every other HikRAD binary's
  static-binary posture) and stages it into the bundle's `scripts/`
  directory, gated on the same `HIKRAD_SKIP_IMAGE_BUILD` flag the
  existing image-build step already uses (a rehearsal bundle skips both,
  a real one builds both). Verified the cross-compile step itself
  succeeds from this Windows dev box and produces a valid Linux ELF
  binary.
- **Two pre-existing, unrelated test failures found while running the
  full regression suite** (`internal/monitorsvc`'s already-documented
  WhatsApp-fake timing flake, and two `internal/portalapi` tests still
  calling routes v2-2 retired) — both newly dated in
  `docs/ops/known-issues.md`, neither touched by this phase, confirmed by
  the fact that this phase's diff contains zero changes to
  `internal/monitorsvc`, `internal/portalapi`, or `internal/billing`'s
  gateway/card-payment code.

## Design refinement discovered while writing tests (not a bug, a
correction to my own draft before it was ever committed)

The phase brief's C3 draft first sketched the **daemon** acquiring the
update lock itself, before spawning the child. Writing
`TestConcurrentUpdateLock` surfaced why that's wrong: an OS-level `flock`
is tied to the process that opened it, so if the daemon (not the child)
held the lock, killing the daemon would **release the lock via the kernel
even though the child `hikrad update` process was still actually
running** — silently defeating the exact guarantee FR-86.5 exists to
provide. The shipped design instead has `scripts/hikrad`'s `cmd_update`
acquire its own lock (added this phase), held by the **child's** own file
descriptor for the child's own lifetime, completely independent of the
daemon; the daemon detects a conflict by scanning the child's first output
line rather than holding any lock of its own. This is what C3's original
text actually said in its closing sentence ("held... by the still-running
child holding its own fd") — treated as the authoritative reading, not a
deviation from it, but worth recording since the two readings genuinely
diverge and only one is safe.

## Human/hardware legs (documented-pending)

1. **Clean-VM update via button (item 11)** — a real install, admin clicks
   "Update now" in the panel, the panel reloads on the new version after
   reconnecting, and `hikrad backup list` shows the pre-update backup. Not
   exercised: this environment has no spare Ubuntu VM with a full,
   running HikRAD Docker Compose stack and a real signed bundle to update
   *to*. Everything the button depends on was proven independently and for
   real instead: the daemon binary (cross-compiled, socket-tested in
   WSL2), `install.sh`'s provisioning of it (also in WSL2, as root, with a
   stub binary through three scenarios), and the panel/relay/permission
   chain (DB-gated tests against real Postgres/Redis). Exercise on the
   next pilot-style rehearsal, alongside this repo's other
   already-tracked pending hardware legs (v2-5's clean-VM bundle install,
   v1 Phase 5's restore-round-trip).
2. **Broken-image autonomous rollback, panel dead throughout (item 12)** —
   same VM, a bundle engineered to fail health-check, the panel's own
   container forcibly killed mid-update, rollback still completes and is
   correctly reported on the panel's next reconnect. The mechanism
   (gate item 5, `TestRollbackSurvivesDaemonDeath`/
   `TestRollbackStatusPersistsAcrossRestart`) and the panel's reconnect/
   poll fallback (code-reviewed, `stream.go`'s `pollAndStream`) are both
   proven independently; only the "at real scale, against a real broken
   `hikrad-api` image, with the browser tab actually losing its
   connection" combination remains unexercised.

Both items are the same class of residual v2-5's own gate carried forward
for its clean-VM/tamper-refusal items — tracked, not silently skipped, and
owned by whoever runs the next live-hardware rehearsal.
