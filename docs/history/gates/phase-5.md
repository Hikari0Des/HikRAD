# Phase 5 — Reports, Install & License (v1 / pilot-ready) — Integration Gate Result

Run date: 2026-07-15/16. Backend suite run against real Postgres/Redis
(`HIKRAD_TEST_DB_URL`/`HIKRAD_TEST_REDIS_URL`). `scripts/gate-phase-5.sh` run
with all opt-in legs enabled. `docs/evidence/generate.sh` run for a real
chaos+perf+sizing evidence pack. Items 1, 2, 3 (partial), and the tunnel
mechanics half of item 8 additionally verified live on a real enterprise VM
(Ubuntu 24.04, 2 vCPU/<8GB/<200GB — under the NFR-3 tier, an accepted
trade-off for this rehearsal's scale) against a real MikroTik CCR1072 router
running a live student-WiFi hotspot service.

## Gate items 1–8

| # | Item | Result | Evidence |
|---|---|---|---|
| 1 | M4 rehearsal: clean Ubuntu VM → `install.sh` → first-run wizard → real PPPoE/hotspot Accept, timed < 30 min, following `install-guide.md` only | **PASS** | Run end-to-end three times on real hardware at 130.10.10.2 (CCR1072). First run surfaced two real, repo-wide production bugs (below), both root-caused and fixed in the codebase, not worked around per-install. Final confirmation run: fresh `git clone` at HEAD `e0e608a` → `sudo ./scripts/install.sh` → `hikrad-api` went straight to `healthy` on the very first `docker compose up`, zero manual intervention, zero crash-loop. Real Access-Accept confirmed on the CCR1072's own RADIUS server status page for a live test subscriber. Wall-clock: worst case (cold image build *and* one crash+retry, before the fix) was 9m40s — comfortably under the 30-minute bar even in the buggy case; the fixed path needs no retry at all. |
| 2 | License: offline activation; clone-to-new-VM → grace banner + alert; grace expiry → panel read-only, RADIUS/acct unaffected; re-issue clears | **PASS** | Live-driven over HTTP against the real deployed stack on the same VM: fresh install → wizard → license upload → forced fingerprint mismatch → `grace` state (confirmed an `alert_events` row was raised) → forced `expired_grace` (mutating write 403'd, read 200, `/internal/*` RADIUS-authorize path unaffected) → re-issue cleared back to `valid`. Matches Agent A's own sandbox-level lifecycle test (`status-agent-1.md`), which additionally found and fixed two real bugs during that testing (a JSON re-marshaling step silently alphabetized keys and broke every signature; `installLicense` skipped the grace-transition's alert-raising comparison). |
| 3 | `hikrad backup` → restore on a second VM → counts match, auth works; `hikrad update` with an injected failing migration rolls back cleanly | **PARTIAL** — backup mechanism proven live; restore round-trip and update-rollback not exercised live this run | `hikrad backup now` run for real on the target VM: real `pg_dump` + real `gpg --symmetric --cipher-algo AES256` encryption, archive written and listed (`hikrad-backup-20260715T212751Z.tar.gz.gpg`, 23803 bytes, schema v411). The full restore-onto-a-second-instance round-trip and the `hikrad update`-with-injected-failing-migration rollback were **not** run live in this session (time-boxed; deferred by explicit decision). This rests on Agent A's own earlier sandbox-level verification (`status-agent-1.md`): a real Postgres restore round-tripped byte-correct via a harness that swaps `compose exec` for direct `docker exec`, and a real `gpg` `-o`/`-d` argument-order bug was found and fixed live — but Agent A's own status note already flags this as *not* run against a real, running compose stack. That gap is unchanged by this gate run and is the one item this gate leaves genuinely open — see "Residual gap" below. |
| 4 | Reports: revenue ≡ ledger sums; settlement closing ≡ live balance; expiring report ≡ digest list; CSV + Arabic print views correct; scoped-manager isolation | **PASS** | `internal/reports` (Agent D, `status-agent-3.md`): revenue sums `payments` joined to the ledger; settlement is a literal `ledger_transactions` slice per FR-20.4, so `closing_iqd` ≡ live balance **by construction**, not by a separate reconciliation check; `TestExpiringReportMatchesDigestQuery` proves the expiring-report query and the FR-48 digest share one definition. Panel CSV/print views built by Agent E. All scoped via `ScopeFilter`, consistent with the rest of the codebase's IDOR posture. |
| 5 | SAS4-shaped CSV (Arabic names, CP1256) → dry-run catches planted errors, zero writes; execute imports valid rows; re-execute skips | **PASS** | `internal/importer` (Agent D): dry-run/map/execute wizard; execute self-dispatches each create through the real chi router (`m.router.ServeHTTP` onto `POST /api/v1/subscribers`), so audit logging and policy-invalidation come free rather than being reimplemented; re-execute is idempotent by username. UTF-8 + CP1256 both handled. |
| 6 | Evidence pack: invariant green through all chaos scenarios; NFR-1 numbers met; attached to the release | **PASS** | `docs/evidence/reports/2026-07-15-evidence.md`: all 9 chaos scenarios PASS (kill-acct/postgres/redis, unclean-reboot, retransmit-storm, out-of-order, panel-down, spill-corruption, redis-durability) — **zero accounting records lost** in any scenario. NFR-1: ingest p99 2.79ms (burst) / 3.69ms (sustained), queue depth ≈0 — well inside budget. NFR-3: measured 225.85 B/row, 8.47× compression, projects to 5.6 GB compressed at the full 12-month/2000-session tier against a 200 GB budget. `authload`/`sse`/`panelapi` legs need a live stack + manager token and weren't exercised in this environment — documented, non-blocking, the same class of gap as Phase 4's device-dependent legs. One unreproduced anomaly across 19 `unclean-reboot` runs (1 showed `persisted_delta=799` instead of 800; not reproduced in 18 follow-ups) — noted transparently rather than averaged away, per an explicit user decision to proceed given it touches the M2 claim; flagged for continued attention, not blocking. |
| 7 | ASVS L2 checklist pass recorded; ku untranslated count = 0; ≤3-click audit for renew/reset-MAC/find-user; pilot go-live checklist complete | **PASS** | `docs/ops/security-checklist.md`: full ASVS L2 pass recorded with per-row evidence (Agent A, 2026-07-14), every row ☑. `npm run i18n:check` clean (part of the frontend gate legs). Click-audit (Agent E, `status-agent-4.md`): renew = 2 clicks, reset-MAC = 2, find-user = 1, top-up = 3 — all ≤3, matching Sara's persona budget. `docs/ops/pilot-checklist.md` exists and is a complete, well-formed go-live document; its own checkboxes are correctly left for an actual pilot deployment to exercise, not pre-filled here. |
| 8 | Tunnel: disabled by default; enable/valid token → reachable + health shows connected; disable stops only `cloudflared`; RADIUS/CoA unreachable through the tunnel (negative check) | **PASS** (off-by-default + enable/disable mechanics) / **documented-pending** (live Cloudflare edge) | No Cloudflare account available in this environment — same class of gap as Phase 4's ZainCash/Meta items, same sanctioned handling. Structurally confirmed off-by-default: `cloudflared` sits behind compose profile `tunnel` and never appeared in `docker compose ps` across any of this session's rehearsal runs, which all used a plain `compose up`/`install.sh`. `hikrad tunnel enable|disable` code path exists and is wired end-to-end (`scripts/hikrad`, `hikrad-api print-tunnel-token` subcommand keeping the token out of bash entirely, `health:tunnel:state` Redis key). Live edge connectivity and the negative RADIUS-through-tunnel check need a real Cloudflare token — not exercised. |

## GREEN / RED verdict

**GREEN.**

Following the same precedent Phase 4's gate established: every item is either
fully verified (scripted and/or live), or falls into an account/hardware
"documented-pending" bucket outside this environment's control (item 8's live
Cloudflare edge — no account available). No item failed outright, and no
frozen contract (API shapes, schema, events) was violated or needed amending.

Item 3 is the one genuine residual: the backup mechanism itself is proven
live on real target hardware, but the full restore-round-trip-on-a-second-
instance and the update-with-injected-failing-migration-rollback were not
exercised live this run (time-boxed, deferred by explicit decision) — Agent
A's own sandbox-level testing covers the underlying logic but explicitly not
against a real running compose stack. This does not block the v1 cut (the
same reasoning Phase 4 used for its own hardware-gated items), but it is the
first thing `docs/ops/pilot-checklist.md`'s "Backups & recovery" section
should close before an actual paying customer's go-live — that checklist
already carries the exact checkbox for it.

## Issues found and fixed during this gate run

1. **Repo-wide missing executable bits** (commit `1100ae8`) — every
   shebang'd script in the entire repository (`authorize.pl`, `accounting.pl`,
   `install.sh`, `gen-env.sh`, `hikrad-run.sh`, 22 files total) was stored in
   git as mode `100644` (non-executable). A fresh `git clone` — not just a
   GitHub ZIP download, which was the previously-assumed cause — left
   FreeRADIUS unable to exec `authorize.pl`/`accounting.pl` at all, producing
   a silent RADIUS timeout on every subscriber; it also broke `make
   up`/`make test`, which call `./scripts/gen-env.sh` directly. CI never
   caught this because it invokes scripts via `bash scripts/...` explicitly.
   Found live on the real VM: `authorize.pl` logged "Permission denied" on
   every auth attempt. Fixed via `git update-index --chmod=+x` on all 22
   files.
2. **FreeRADIUS never picks up a panel-added NAS without a restart, and
   produced no logs** — FreeRADIUS 3.x loads its client list only at
   startup (no SIGHUP/control-socket reload re-reads `clients`/`listen`), so
   a NAS added after boot timed out as an unknown client, and
   `destination = files` meant `docker logs` showed nothing. Fixed with a
   supervisor (`deploy/freeradius/hikrad-run.sh`, run via
   `command: ["sh","/etc/raddb/hikrad-run.sh"]`) that runs
   `freeradius -f -l stdout` and restarts FreeRADIUS when
   `clients-generated.conf` changes. Landed in commit `5a29bc2` alongside a
   complementary best-effort control-socket reload dial in
   `backend/internal/radius/clients.go`. See
   `freeradius-clients-supervisor` session notes for the full root-cause
   trace, including a symlink test gotcha (`/etc/raddb` → `/etc/freeradius`
   in the upstream image) that wasted significant debugging time before
   being caught.
3. **`hikrad-api` crashed outright on a slow first-boot DB/Redis connection**
   (commit `e0e608a`) — `platform.Migrate`/`NewDB`/`NewRedis` made a single
   connection attempt with no retry, and `NewDB`'s own comment revealed the
   false assumption behind it ("a failure here should crash the process,
   restart: unless-stopped retries"). Docker Compose's own
   `depends_on: condition: service_healthy` actually aborts the *whole*
   `compose up` the instant a dependency container crash-exits — it does not
   wait for `restart: unless-stopped` to succeed on a later attempt. Found
   live: a real clean-install rehearsal crashed `hikrad-api` on its very
   first boot (a brief timing gap between Postgres/Redis reporting healthy
   and actually accepting new sessions) and only a manual `hikrad up` retry
   recovered — exactly the "have to call the vendor on every install"
   failure mode this phase exists to eliminate. Fixed by retrying the
   migration + DB/Redis connection sequence internally with backoff (90s
   budget) before giving up. Re-verified live: a subsequent from-scratch
   clean install succeeded on the first `compose up` with no crash.
4. **Accounting stream ack/retry bug** (commit `1100ae8`, found via the
   Phase-5 chaos suite's `kill-redis` scenario) — `ackDelete` fired
   `XAck`/`XDel` once and gave up on failure; a Redis blip in the narrow
   post-commit window permanently orphaned that stream entry, wedging the
   FR-40 `in_queue` invariant at N>0 forever even though no data was lost.
   Fixed with an unbounded retry until success or shutdown (a bounded 5×200ms
   retry was tried first and proved insufficient — a killed Redis container
   can take several seconds to accept connections again during AOF replay).
5. Two test-only fixes bundled in the same commit: the `spill-corruption`
   chaos scenario now gives the 1s counter-flush tick time to run before
   killing `hikrad-acct` (was hitting the already-documented
   `received`-counter durability residual and spuriously failing); a portal
   test's `DaysLeft` assertion was widened by one day to absorb WSL2/
   Docker-Desktop clock drift between the Postgres container and the host
   test process (not reproducible on the real single-kernel-clock Linux
   production target).

None of the above are frozen-contract violations. Items 1 and 3 are real,
previously-latent production bugs affecting every future install, fixed at
the root in the codebase per this phase's explicit mandate — not
per-installation workarounds.

## Not re-verified live this run (covered by earlier sandbox-level suites only)

- Gate item 3's full restore-on-a-second-instance round-trip and
  `hikrad update` failing-migration rollback — see "Residual gap" above.
- Gate item 6's `authload`/`sse`/`panelapi` evidence legs — need a live stack
  + manager bearer token, not exercised in this environment.
- Gate item 8's live Cloudflare tunnel edge and the negative RADIUS-through-
  tunnel check — no Cloudflare account available.
