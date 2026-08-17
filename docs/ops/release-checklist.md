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

## Signing & bundle (v2 phase 5, FR-81; reworked off Docker images v4-2, FR-119)

- [ ] `VERSION` file at the repo root matches the tag being cut (e.g. `v4.2.0`).
- [ ] `scripts/build-release-bundle.sh` produced binaries for `hikrad-api`,
      `hikrad-acct`, `hikrad-monitor`, `hikrad-updaterd` (`bin/`) and the
      frontend build (`www/`) — no OCI image in the Linux bundle at all
      (Decision 57; `.github/workflows/release.yml`, triggered by pushing
      the tag).
- [ ] `HIKRAD_RELEASE_SIGNING_KEY` repo secret is set to the **real** offline
      release keypair, not `scripts/release-signing/dev-release-key.pem`
      (dev key is fine for a rehearsal tag only — see
      `scripts/release-signing/README.md` for the one-time rotation ritual
      before the first commercial shipment).
- [ ] The built bundle's `SHA256SUMS.sig` verifies against the public key
      embedded in `scripts/verify-bundle.sh`, checked on a machine **other**
      than the one that built it (catches "verifies because it's the same
      disk" false confidence): `bash scripts/verify-bundle.sh <extracted-dir>`.
- [ ] A copy of the bundle with a single byte flipped anywhere in it is
      refused by both `install.sh --bundle` and `hikrad update --bundle`
      before anything is extracted into place.
- [ ] The bundle is fully offline-installable: `tar -tf hikrad-vX.Y.Z.tar`
      shows `bin/`, `www/`, `freeradius/`, `nginx/`, `systemd/`,
      `migrations/`, and `scripts/` — confirm before publishing, not after a
      customer reports a failed offline install.
- [ ] The RouterOS container image (`deploy/routeros/build.sh`, both amd64
      and arm64) built successfully and is attached to the same release —
      this is the ONE remaining OCI image in the pipeline (Decision 58);
      confirm it is NOT pushed to any registry, only attached as a
      downloadable release asset.
- [ ] Repo visibility of the **canonical** repo (`Hikari0Des/HikRAD`) is
      confirmed **Private** (account-level setting, not enforceable by a
      commit — FR-83.1). **Found public 2026-08-16** — full source readable
      via anonymous `git clone`/`raw.githubusercontent.com`, no auth
      required; flip it via repo Settings → Danger Zone → Change visibility
      → Private before cutting any real release. Do not rely on this
      checklist alone to catch it again — nothing in CI enforces it (GitHub
      has no API-visible signal a workflow can gate on for its own repo's
      visibility).
- [ ] `Hikari0Des/hikrad-releases` (the public releases-only mirror, Decision
      56) exists, is genuinely public, and contains **zero source** — spot
      check its file listing, it should only ever have `install.sh` at its
      root plus whatever Release assets exist. If it doesn't exist yet,
      `.github/workflows/release.yml`'s mirror-publish steps will warn and
      skip (release still succeeds, just customer-facing distribution won't
      have this version).
- [ ] `HIKRAD_RELEASES_REPO_TOKEN` repo secret (on the canonical private
      repo, used to publish to the mirror) is a **fine-grained PAT scoped
      only to `Hikari0Des/hikrad-releases`** (`contents: write`) — never a
      classic PAT or one with access to the private repo itself.
- [ ] The bundle is published as a GitHub Release asset on **both** the
      private canonical repo (internal record) and the public
      `hikrad-releases` mirror (what `scripts/quick-install.sh`'s
      `curl \| sudo bash` one-liner actually fetches) — never uploaded
      anywhere else.

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
