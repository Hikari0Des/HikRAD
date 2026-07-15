# v1 release checklist

This is the internal go/no-go checklist for cutting the v1 release build,
distinct from [pilot-checklist.md](pilot-checklist.md) (which the ISP's
installer runs against the actual pilot server). Everything here should be
true of the exact commit being tagged.

## Gate status

- [x] Phase 1 integration gate — GREEN (`docs/phases/phase-1.../gate-result.md`
      per the phase directory naming at the time).
- [x] Phase 2 integration gate — GREEN.
- [x] Phase 3 integration gate — GREEN, 8/8
      (`docs/phases/phase-3-billing-security-monitoring/gate-result.md`).
- [x] Phase 4 integration gate — GREEN, 10/10
      (`docs/phases/phase-4-portal-payments-pwa/gate-result.md`).
- [x] Phase 5 integration gate — GREEN, 8/8
      (`docs/phases/phase-5-v1-reports-install-license/gate-result.md`). One
      residual: item 3's full restore-round-trip and update-rollback were
      not exercised against a live running compose stack — tracked in
      `pilot-checklist.md`'s "Backups & recovery" section, to be closed on
      the first real pilot install before that specific ISP goes live with
      unattended nightly backups as their only recovery path.

## Code health at the tag

- [ ] `go build ./...`, `go vet ./...`, `go test ./...` all green on the
      exact commit being tagged (backend).
- [ ] `npm run lint --workspaces --if-present`,
      `npm run build --workspaces --if-present`,
      `npm run test --workspaces --if-present`, `npm run i18n:check` all
      green (frontend).
- [ ] `scripts/gate-phase-5.sh` (and earlier phases' gate scripts, if still
      present) run clean with all opt-in legs enabled
      (`HIKRAD_TEST_DB_URL`/`HIKRAD_TEST_REDIS_URL` set).
- [ ] No uncommitted changes; `git log` for the tag matches what was
      actually tested — don't tag a commit that was never itself gated.

## Known-fixed production bugs to confirm are actually in the tag

These were found live against real hardware during the Phase 5 gate run and
are easy to accidentally tag before/after the fix lands — confirm the tagged
commit is a descendant of all three:

- [ ] `1100ae8` — repo-wide missing executable bits (every shebang'd script
      was git mode `100644`; breaks a fresh `git clone`'s FreeRADIUS
      `authorize.pl`/`accounting.pl` and `make up`/`make test`).
- [ ] `e0e608a` — `hikrad-api` startup retry (was crashing outright on a
      slow first-boot DB/Redis connection instead of retrying with backoff).
- [ ] `5a29bc2` — FreeRADIUS clients-file supervisor + control-socket reload
      (panel-added NAS previously required a manual FreeRADIUS restart) and
      the `acct-spill` root-owned bind-mount chown fix in `install.sh`.

## Evidence attached to the release

- [ ] `docs/evidence/reports/<latest>.md` generated against the exact tagged
      commit (not an older one) and attached wherever the release is
      published internally.
- [ ] `docs/ops/security-checklist.md` fully ☑ against the tagged commit.

## Docs consistency

- [x] `CLAUDE.md`'s "Current state" paragraph reflects Phases 1–5 complete.
- [ ] `docs/PRD.md`'s Decisions Log has no open decision that contradicts
      what actually shipped.
- [ ] `docs/ops/install-guide.md`, `admin-guide.md`, `backup-restore.md`,
      `update.md` all describe the CLI/API shapes as they exist in the
      tagged commit (spot-check one command from each against the real
      code, don't just skim for plausibility).

## Sign-off

- [ ] Whoever cuts the tag has read this entire checklist against the real
      commit, not from memory of an earlier pass.
- [ ] `docs/ops/pilot-checklist.md` handed to the first pilot ISP's installer
      along with `admin-guide.md`.
