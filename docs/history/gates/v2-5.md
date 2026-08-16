# Phase v2-5 — Closed-Source Distribution & Licensing Hardening — Integration Gate Result

Run date: 2026-07-18. Executed **solo + sequential** per Decision 25 / the v2
execution plan, re-ordered ahead of v2-6/v2-7 by explicit owner request
(Decision 38) — this session's original request, immediately after v2-2. One
kickoff blocker (registry backend for the optional dev-side pull path) was
resolved by the owner before any code — GHCR, bundle-primary, no per-customer
registry credential in v1 — see PRD Decision 38 and this phase's own
`00-phase.md` header.

Verification environment: no Postgres/Redis dependency for this phase's own
work (release signing, bundle assembly, installer scripting, and license
boot-verification wiring are DB-independent) — `go build`/`go vet`/`go test`
ran against the existing unit suite (DB-gated legs elsewhere in the repo
self-skip, unaffected either way). `scripts/gate-v2-phase-5.sh`'s scripted
legs use throwaway fixtures (a scratch Ed25519 keypair, a fake bundle
directory) rather than real multi-GB images, per the phase brief's own design
— proving the mechanism does not require building the full image set.

`scripts/gate-v2-phase-5.sh`: **all scripted legs PASS, 0 FAIL.**

## Gate items 1–7 (scriptable)

| # | Item | Result | Evidence |
|---|---|---|---|
| 1 | **Signature round-trip + tamper-refusal, no VM needed (AC-81b)** | **PASS** | `openssl` Ed25519 keygen/sign/verify proven directly in the gate script against a scratch keypair; `scripts/verify-bundle.sh` tested by hand against three cases before commit — a valid fake bundle (accept), tampered file content with the original `SHA256SUMS`/signature left in place (checksum leg catches it), and a fully-regenerated `SHA256SUMS` with no re-signing (signature leg catches it, proving an attacker can't just "fix" the manifest without the private key). |
| 2 | **Compose rendering correctness (C5)** | **PASS** | `render-release-compose.sh` run against the real `deploy/compose.yml`: output has `image:` (no `build:`) for exactly the 4 HikRAD services, `docker compose config --quiet` accepts it with dummy env vars, and a `diff --strip-trailing-cr` against the source shows only the 4 intended `build:`→`image:` replacements — every other line (postgres, redis, freeradius, healthchecks, volumes, `deploy.resources`, comments) byte-identical. |
| 3 | **No new blocking license path (AC-82a, C6's hard boundary)** | **PASS** | Grep legs confirm `hikrad-acct`/`hikrad-monitor` each call `platform.RefreshLicenseCache` at boot, start a `time.NewTicker`, and contain **no** `license.State`/`CachedLicenseState()` reference paired with `exit`/`os.Exit`/`return err` — i.e. no code path exists that could make either binary refuse to start or stop processing on license state. `go build`/`go vet` clean. |
| 4 | **`licenseGate` scope unchanged** | **PASS** | `internal/httpapi`'s existing `TestLicenseGate*` suite passes unmodified — this phase touched zero lines of `license_gate.go`. |
| 5 | **Dev-mode regression (AC-82b)** | **PASS** | Full backend `go build ./...` / `go vet ./...` / `go test ./...` clean (all packages `ok` or self-skip, none newly failing). Replicated the CI `scripts` job's exact install.sh idempotency sequence by hand (`HIKRAD_SKIP_OS_CHECK=1 HIKRAD_SKIP_DOCKER=1`, `--no-start`, second run exits 2, `.env` byte-identical before/after) — source-mode installs (no `--bundle`) are unaffected by construction, not by a special case. |
| 6 | **Installer bundle-mode plumbing (C4)** | **PASS** | `install.sh --bundle <tar>` run end-to-end against a real signed test bundle (built via `scripts/build-release-bundle.sh` with `HIKRAD_SKIP_IMAGE_BUILD=1`) into a sandboxed `HIKRAD_ROOT`: verified, staged into `release/`, `install.meta` correctly records `HIKRAD_CHECKOUT`/`HIKRAD_COMPOSE_FILE`/`HIKRAD_DELIVERY_MODE=bundle`, `freeradius/clients-generated.conf` chowned at the right (staged) path, rendered `compose.yml` carries 8 `image:` lines (4 rendered + 4 pre-existing third-party). Repair path (`install.meta` already present) correctly re-derives `RELEASE_DIR`/`CHECKOUT_DIR` without needing `--bundle` passed again — traced by code review; the interactive "type 2" prompt itself isn't exercisable non-interactively in this harness (this repo's test infra has no pty), so this specific sub-path is a **light residual** — the underlying variable resolution it depends on is the same code every other item here exercises directly. |
| 7 | **Docs accuracy** | **PASS** | `docs/PRD.md` carries FR-81–83 and Decision 38 (done in Step 1, before any code). Sub-PRD 01 and the index updated to 83/83 FR coverage. `docs/ops/install-guide.md`/`update.md`/`release-checklist.md` rewritten for the bundle delivery model (spot-checked against the actual `install.sh`/`hikrad` flags, not skimmed). `docs/ops/known-issues.md` carries this phase's own bug (below), dated 2026-07-18. |

## GREEN / RED verdict

**GREEN — 7/7 scripted items.** Two human/hardware legs (below) are
documented-pending, same sanctioned pattern as the v1 Phase 5 gate's
restore-round-trip item.

## Bugs found and fixed

- **`hikrad update`'s image rollback was silently a no-op.** The pre-update
  snapshot loop computed a `ref` variable meant to capture each service's
  actual resolved image reference via `docker compose config --images |
  grep "^${svc}"` — but that command's output has no service-name prefix, so
  the grep never matched and `ref` was always empty *and never used anyway*:
  the loop unconditionally wrote a hardcoded, unrelated
  `"hikrad-$svc:latest=$id"` line instead. `rollback_update_images` then
  re-tagged old image ids onto a name `docker compose up` never actually
  resolves any service to, in either build mode. Found while wiring bundle
  mode's rollback story (a bundle's `compose.yml` pins a real, meaningful
  version tag per release, which made the pre-existing gap concretely
  observable rather than latent). Fixed: the snapshot now reads each image's
  own current `RepoTags[0]` via `docker inspect` — correct in both source
  mode's `build:`-synthesized name and bundle mode's pinned registry tag.
  Logged in `docs/ops/known-issues.md`.
- **(Design-time, not shipped) a bundle-mode compose.yml overwrite risked
  clobbering a source checkout's `deploy/compose.yml`.** First draft of
  `hikrad update --bundle`'s staging logic reused `dirname
  "$HIKRAD_COMPOSE_FILE"` unconditionally as the copy target — fine for an
  install that started in bundle mode, but for a source-mode install that
  point is the live git checkout's own `deploy/` directory, and overwriting
  it mid-update would silently discard any local dev customization or
  uncommitted changes. Caught during design review before writing the code
  that would have shipped it. Fixed by always staging bundle-mode updates
  into a dedicated `$HIKRAD_ROOT/release/` directory regardless of the
  install's prior delivery mode, then repointing `install.meta` there —
  converting a source install to bundle delivery explicitly and safely
  rather than overwriting anything in place.

## Deviations from the brief

- **Registry pull mode (C7) was not implemented this pass** — explicitly
  sanctioned as an optional scope cut by the phase brief itself ("If
  registry mode isn't implemented at all in this pass, that is an
  acceptable, explicitly-sanctioned scope cut"). The signed offline bundle
  is the only supported customer-facing delivery path; `install.sh`/`hikrad
  update` have no `--registry` flag. `.github/workflows/release.yml` still
  pushes images to GHCR (vendor/dev convenience, per the resolved kickoff
  blocker), but nothing on the install/update side consumes them by pull.
- **`RELEASE_SIGNING_KEY_FILE`/GHCR push both default to the checked-in dev
  key and no real registry credentials** — expected for this phase; the
  real offline release keypair generation and `HIKRAD_RELEASE_SIGNING_KEY`
  secret provisioning happen once, before the first commercial shipment,
  per `scripts/release-signing/README.md`'s ritual (mirrors
  `scripts/license-tool`'s existing dev-key pattern).
- **`release-vX.Y.Z/` versioned-directory retention (mentioned as an open
  question in the brief) was resolved to the simplest option**: a
  one-deep `release/` + `release.rollback` backup, not a full versioned
  history. Satisfies the brief's actual requirement (a failed bundle update
  can revert its compose file, not just image ids) without unrequested
  complexity; the pre-update database backup remains the authoritative
  safety net regardless.

## Human/hardware legs (documented-pending)

1. **Clean-VM no-source install (AC-81a)** — a real Ubuntu 22.04/24.04 VM
   with no Go toolchain and no HikRAD checkout, `install.sh --bundle
   <signed bundle>` → healthy stack → real PPPoE Access-Accept via the
   packet harness. Not exercised: this environment has no spare Ubuntu VM
   and no built product images (`HIKRAD_SKIP_IMAGE_BUILD=1` was used
   throughout this phase's own testing specifically to avoid needing a full
   image build). Exercise on the next pilot-style rehearsal, alongside the
   pre-existing restore-round-trip/update-rollback items already tracked in
   `docs/ops/pilot-checklist.md`.
2. **Real-bundle tamper-refusal end-to-end** — the same VM, a real
   multi-GB bundle with one byte flipped, refused before touching the live
   install. The mechanism itself (gate item 1) and the wiring into
   `install.sh`'s actual flow (gate item 6, against a real though
   image-less bundle) are both proven independently; only the "at real
   multi-GB scale, on real disk I/O" combination remains unexercised.

Both items are the same class of residual the v1 Phase 5 gate carried
forward for its own restore-round-trip item — tracked, not silently
skipped, and owned by whoever runs the next live-hardware rehearsal.
