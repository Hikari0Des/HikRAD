# Phase v2-5 — Closed-Source Distribution & Licensing Hardening

Source brief: [docs/v2/05-closed-source-distribution.md](../../05-closed-source-distribution.md). Requirements FR-81–83 (PRD Decision 38; sub-PRD [01-platform-install-licensing.md](../../../prd/01-platform-install-licensing.md)). Owned entirely by sub-PRD 01, same as v1's original FR-49–53 — no cross-domain split.

**Build-order note:** this phase runs immediately, ahead of v2-6/v2-7 in the execution plan's row order, per this session's explicit instruction (Decision 38). v2-6 (preferences) and v2-7 (one-click updater) are deferred, not cancelled; v2-7 will build on this phase's registry/bundle plumbing rather than the other way around, so the standing dependency note "v2-7 before or with v2-5's registry work" is superseded — v2-5 first.

**Kickoff blocker, resolved by the owner 2026-07-18 (binding, not re-litigated by this brief):**
1. **Registry backend: GHCR, bundle-primary, no per-customer registry credential in v1.** Push images to GitHub Container Registry (`ghcr.io/hikrad/*`, private, free with the existing private repo). Registry-pull mode exists in `install.sh`/`hikrad update` as a vendor/dev-side convenience only; the **signed offline bundle (FR-81.2) is the mandatory, always-available customer path** and alone satisfies NFR-7. No per-customer registry-pull credential is issued or bound to a license in this phase (sub-PRD 01 FR-82.3 amended in place, same commit as this file). Revisit only if a future need for direct `docker pull` at customer sites outweighs the per-sale ops cost of provisioning it.

## 1. Problem (restated from the source brief)

`install.sh`/`hikrad update` build images **from source on the customer's server** today — the whole repo ships to every install, and the license check lives in readable Go source a customer could patch out and rebuild. There is no artifact pipeline producing versioned, distributable release bundles at all; "release" currently means "git checkout". This phase changes the delivery model itself: compiled, signed images/bundles replace source checkout, and license verification extends (informationally) to every binary.

## 2. Scope for this implementation pass

1. **Release signing** — an Ed25519 keypair distinct from the license-issuance keypair (different concern: this one authenticates *build artifacts*, not *customer entitlements*); public key embedded in the installer scripts, private key held offline (mirrors `scripts/license-tool`'s existing dev-key ritual). (C1)
2. **Bundle format** — `hikrad-vX.Y.Z.tar`: images, a rendered `image:`-only `compose.yml`, runtime config (`freeradius/`, `caddy/`), scripts, migrations, a checksum manifest, and a detached signature. (C2)
3. **CI release job** — builds/tags/pushes the four HikRAD images to GHCR and assembles + signs the bundle as a workflow artifact, triggered on a version tag. (C3)
4. **Installer/updater bundle mode** — `install.sh --bundle <path>` / `hikrad update --bundle <path>` verify-then-extract-then-use, with the existing source-build path kept behind an explicit dev-only flag; `install.meta` gains delivery-mode fields. (C4)
5. **Compose rendering** — a small script that turns the dev `deploy/compose.yml` (with `build:`) into the bundle's `compose.yml` (with `image:` tags, `build:` stripped for the four HikRAD services only). (C5)
6. **License boot verification** — `hikrad-acct`/`hikrad-monitor` call the existing `platform.RefreshLicenseCache` at boot and on the same 10-minute ticker `hikrad-api` already runs; **informational only**, per FR-82.2's hard boundary. (C6)
7. **Repo/business hygiene** — `docs/ops/release-checklist.md` gains a signing/tagging/push section. (C8)
8. **Docs** — `docs/ops/install-guide.md`, `update.md`, `release-checklist.md` rewritten for the new delivery model; bugs found land in `docs/ops/known-issues.md` (standing rule).
9. **Gate** — signature round-trip + tamper-refusal (scriptable, no VM needed), compose-rendering correctness, license boot-verification non-blocking property, full regression of every existing gate/CI leg; a clean-VM no-source install and a real-bundle tamper-refusal rehearsal are **human/hardware legs**, documented-pending like Phase 5's own residual items — see "Human/hardware legs" below.

Commit in reviewable chunks along these boundaries (signing tool + embedded key / bundle builder + compose renderer / installer+updater bundle mode / license boot verification in acct+monitor / docs) — this phase touches three very different layers (crypto tooling, bash installer, Go binaries) and each deserves its own reviewable commit, same reasoning as every prior phase's chunking.

## Migration budget

**None anticipated.** This phase adds no schema — the registry-credential DB column contemplated by the source brief's FR-B is explicitly out of scope per the resolved kickoff blocker (C7), and license boot verification (C6) reuses `internal/platform/license`'s existing table/cache verbatim. The repo's current max migration is **0588** (v2-2's tail); if implementation discovers a genuine need for a schema change, it takes the next free number above whatever the max is *at that time* (the standing linear-numbering rule — do not assume 0589 is still free by the time this phase is actually built, verify first).

## Frozen contracts

### C1. Release signing keypair (distinct from the license-issuance keypair)
A **new** Ed25519 keypair, generated and verified via `openssl` (already a hard dependency of `install.sh` for self-signed TLS cert generation — no new tool required):
```sh
openssl genpkey -algorithm ED25519 -out release-signing-key.pem   # PRIVATE — offline, never committed
openssl pkey -in release-signing-key.pem -pubout -out release-public-key.pem
openssl pkeyutl -sign   -inkey release-signing-key.pem -rawin -in SHA256SUMS -out SHA256SUMS.sig
openssl pkeyutl -verify -pubin -inkey release-public-key.pem -rawin -in SHA256SUMS -sigfile SHA256SUMS.sig
```
(`-rawin` is required for Ed25519 — it signs the message directly with no separate digest step, unlike RSA/ECDSA `dgst -sign`. Verified working against this repo's OpenSSL on both the dev machine and the Ubuntu 22.04/24.04 targets `install.sh` already supports — OpenSSL 3.0+ ships Ed25519 support on both.)

**Why a second keypair, not the license one:** `license.ProductionPublicKeyB64` authenticates *customer entitlements* (long-lived, one per sale, verified inside compiled Go); this new key authenticates *this vendor's own build artifacts* (one keypair total, identical in every install, verified by a bash script before any HikRAD binary exists to do it in Go). Mixing the two would couple unrelated rotation/compromise domains for no benefit.

**Embedding:** the public key (PEM, not base64-only, so `openssl pkeyutl -pubin` can consume it directly with no decode step) is embedded as a heredoc constant inside a **new** `scripts/verify-bundle.sh` — the single source of truth for verification logic, called by both `install.sh` and `scripts/hikrad`'s `cmd_update`, and directly by the gate script for the tamper-refusal leg. Do not duplicate the PEM literal in more than one file.

**Dev key ritual (mirrors `scripts/license-tool/README.md`):** a checked-in dev-only keypair ships until the first commercial bundle; `docs/ops/release-checklist.md` gains a one-time "generate the real offline release key, embed the new public key in `scripts/verify-bundle.sh`, rebuild" step before that first shipment (parallel to the existing license-tool ritual, not a new pattern).

### C2. Bundle layout
```
hikrad-vX.Y.Z.tar
├── manifest.json              # {version, git_commit, built_at, schema_version (max migration number),
│                               #  images: [{name, tag, digest}, ...]}
├── images/
│   ├── hikrad-api.tar          # `docker save` output, one file per image (uncompressed — docker save's own format)
│   ├── hikrad-acct.tar
│   ├── hikrad-monitor.tar
│   ├── hikrad-caddy.tar
│   ├── timescaledb.tar         # pinned third-party images, bundled too — NFR-7 "fully offline" means
│   ├── redis.tar                #   every image compose needs, not just HikRAD's own four.
│   └── freeradius.tar
├── compose.yml                 # rendered by C5 — image: tags, no build: for the 4 HikRAD services
├── freeradius/                 # runtime CONFIG (bind-mounted by compose, same as today's deploy/freeradius/) —
│                                #   not "source" in the FR-81 sense; ships regardless of build mode, unchanged
├── caddy/                      #   layout from today's deploy/caddy/ (Caddyfile only; TLS/data dirs stay under $HIKRAD_ROOT/data)
├── scripts/                    # install.sh, hikrad, gen-env.sh, verify-bundle.sh
├── migrations/                 # backend/migrations/*.up.sql — also baked into the hikrad-api image already;
│                                #   shipped again here so the checksum manifest covers them independent of
│                                #   Docker layer trust (a customer can inspect SQL text without pulling an image)
├── SHA256SUMS                  # `sha256sum` format, one line per file above, relative paths
└── SHA256SUMS.sig              # detached Ed25519 signature over SHA256SUMS (C1)
```
`cloudflared` is deliberately **not** bundled: it is optional (off by default, FR-57), already internet-dependent by nature, and `hikrad tunnel enable` already pulls it separately today — bundling it would contradict its own "never required for daily operation" contract for no benefit.

**Cross-cutting rule this table encodes:** "no source tree" (FR-81.1/81.4) means *no Go/frontend source a customer could read or patch to alter licensed behavior* — it does not mean "zero non-binary files." `freeradius/`/`caddy/` config and `migrations/*.sql` are runtime artifacts the stack has always required present on disk (bind-mounted, or — for migrations — needed by `hikrad restore`'s `max_available_schema_version` today even in source mode); shipping them in the bundle is not a regression of the closed-source goal.

### C3. CI release job
New job in `.github/workflows/ci.yml` (or a sibling `release.yml` — implementer's call, not frozen here), triggered on `v*` tags only (never on every push/PR — this is expensive and produces real artifacts):
1. Build the four images (reusing `deploy/docker/*.Dockerfile`, `HIKRAD_VERSION` from the tag).
2. Tag and push each to `ghcr.io/hikrad/<image>:vX.Y.Z` (GHCR credentials via `GITHUB_TOKEN`'s `packages: write` permission — no new secret needed for the push side). **No `:latest` tag is ever pushed** — every install/update pins an exact version (matches FR-51.4's existing "pulls pinned new image tags" language).
3. `docker save` each of the four plus the pinned third-party images into `images/*.tar`.
4. Run C5's compose-renderer, copy `freeradius/`, `caddy/`, `scripts/`, `migrations/`, write `manifest.json`.
5. `sha256sum` everything into `SHA256SUMS`; sign it with the release private key (`HIKRAD_RELEASE_SIGNING_KEY`, a **new** GitHub Actions repo secret, base64-encoded PEM — the only new secret this phase introduces) via C1's `openssl pkeyutl -sign`.
6. `tar` it all into `hikrad-vX.Y.Z.tar`, upload as a workflow artifact / GitHub Release asset (not pushed anywhere public — the repo stays private per FR-83.1, and a private repo's Release assets are private too).

### C4. Installer/updater bundle mode + `install.meta` deltas
`install.sh` gains `--bundle <path>` (parallel to `hikrad update`'s existing, already-partially-wired flag — see `scripts/hikrad`'s `cmd_update`): extracts the bundle to a **scratch temp dir first**, runs `scripts/verify-bundle.sh <scratch-dir>` (checksum + signature check against the embedded C1 key), and only on success copies `compose.yml`/`freeradius/`/`caddy/`/`scripts/`/`migrations/` into place and `docker load`s the image tars. A failed verification leaves the live install (if any) completely untouched — nothing is copied, nothing is loaded, the scratch dir is discarded (`trap ... RETURN`, same idiom `cmd_backup_now`/`cmd_restore` already use). This satisfies FR-81.3's "no partial effect" without needing true streaming verification inside `tar`.

`hikrad update --bundle <path>` (already exists as a stub per `cmd_update`'s current `docker load -i "$bundle"` — that line trusted an unsigned tar) gains the same verify-first step, and additionally **stages the new release into a fresh versioned directory** (`$HIKRAD_ROOT/release-vX.Y.Z/`, not overwriting the currently-running `release/`) so a failed `compose up`/health-wait can roll back at the **file level** too, not just the image-tag level `rollback_update_images` already handles — the existing image-id rollback stays as the fallback for registry-pull mode, but bundle mode gets the stronger guarantee for free since the old directory is simply still there. Exact retention (keep how many old `release-vX.Y.Z/` dirs) is an implementer's call, not frozen here — `hikrad update`'s existing pre-update backup is still the authoritative safety net regardless.

`install.meta` (written by `install.sh`, read by `hikrad`) gains:
```
HIKRAD_DELIVERY_MODE=bundle|registry|source   # how this install's images were obtained
HIKRAD_RELEASE_DIR=$HIKRAD_ROOT/release       # bundle/registry mode only — where compose.yml/freeradius/caddy live
```
`HIKRAD_COMPOSE_FILE` for a bundle/registry-mode install points at `$HIKRAD_RELEASE_DIR/compose.yml`, not a source checkout's `deploy/compose.yml` — every existing `scripts/hikrad` function that resolves paths off `HIKRAD_COMPOSE_FILE`/`HIKRAD_CHECKOUT` (backup's caddy-dir copy, `max_available_schema_version`'s migrations glob, restore's caddy restore) continues to work unchanged **as long as bundle/registry installs populate the same relative layout** (`migrations/` and `caddy/` siblings of `compose.yml`) that source mode's `backend/`/`deploy/caddy/` provide today — this is why C2's bundle layout mirrors `deploy/`'s shape rather than inventing a new one.

**Source-build mode (dev-only, unchanged):** `install.sh`/`hikrad update` with no `--bundle`/`--registry` flag keep today's exact behavior — `git pull` + `docker compose build` — gated by nothing new; it is simply what happens when neither new flag is given, so `make up` from a checkout and every existing CI leg (`scripts` job's install.sh idempotency test) are unaffected by construction, not by a special-cased flag.

### C5. Compose rendering (`scripts/render-release-compose.sh`, new)
Takes `deploy/compose.yml` and `HIKRAD_VERSION` + `HIKRAD_REGISTRY` (default `ghcr.io/hikrad`), emits a variant where the four HikRAD services' `build: {context, dockerfile, args}` stanzas are replaced with `image: ${HIKRAD_REGISTRY}/<service>:${HIKRAD_VERSION}`, and every other stanza (postgres, redis, freeradius, caddy's `depends_on`/`volumes`/`healthcheck`/`ports`/`deploy.resources`, `cloudflared`) is byte-identical to the source file. A gate leg (below) asserts both properties by diffing the two files with the four `build:` blocks stripped from the comparison.

### C6. License boot verification in `hikrad-acct`/`hikrad-monitor` (FR-82.1/82.2)
Both `main.go`s gain, right after their existing `pgxpool.New`/`redis.NewClient` setup (before `svc.Run`/`monitorsvc.Run`):
```go
platform.RefreshLicenseCache(ctx, db, log)          // boot
go func() {
    ticker := time.NewTicker(10 * time.Minute)       // identical cadence to setupapi.Module (hikrad-api)
    defer ticker.Stop()
    for range ticker.C {
        platform.RefreshLicenseCache(ctx, db, log)
    }
}()
```
No new function, no new package — `platform.RefreshLicenseCache` is already exported and already does exactly this in `internal/platform/setupapi/module.go`; this phase's only change is calling it from two more `main()`s. **Nothing else changes.** `RefreshLicenseCache`'s own contract (load → evaluate against live fingerprint → persist a transition → log) is untouched, and critically it **never returns an error the caller could act on by exiting** — it already swallows/logs every failure internally (see its existing doc comment). This phase must not add any `if state == expired_grace { os.Exit(...) }`-shaped code anywhere in either binary; the gate greps for exactly that pattern (below).

### C7. Registry — GHCR, dev/vendor-only (resolved kickoff blocker)
`ghcr.io/hikrad/{hikrad-api,hikrad-acct,hikrad-monitor,hikrad-caddy}:vX.Y.Z`, pushed by C3. `install.sh --registry`/`hikrad update --registry` (if implemented at all this phase — genuinely optional, since C4's bundle mode alone satisfies every acceptance criterion below) pull by exact tag using whatever ambient Docker credentials the operator's own machine has (`docker login ghcr.io` run manually by the vendor/dev, never automated, never customer-facing). **No registry credential is issued with a license, stored in the license payload, or referenced by `internal/platform/license` in any way this phase.** If registry mode isn't implemented at all in this pass, that is an acceptable, explicitly-sanctioned scope cut — document it as such in `gate-result.md` rather than leaving it silently half-done.

### C8. Repo/business hygiene (FR-83)
`docs/ops/release-checklist.md` gains a new section, ordered before the existing "Sign-off" section:
```markdown
## Signing & registry (v2 phase 5)
- [ ] Release images built + pushed to ghcr.io/hikrad/* at the exact tag being cut, no `:latest` pushed.
- [ ] Bundle built; `SHA256SUMS.sig` verifies against the embedded public key in `scripts/verify-bundle.sh`
      on a machine OTHER than the one that signed it (catches "verifies because it's the same disk").
- [ ] A single bit flipped anywhere in a copy of the bundle is refused by `install.sh --bundle`/`hikrad update
      --bundle` before anything is extracted into place.
```
Repo visibility: no code change — an operational confirmation ("repo is Private", checked in the same section) since GitHub repo visibility is account-level, not something a commit can enforce.

## Integration gate

Green when all scriptable legs pass (`scripts/gate-v2-phase-5.sh`) and the two human/hardware legs are either exercised or explicitly documented-pending in `gate-result.md` (same sanctioned pattern as Phase 5's own gate, restore-round-trip item):

1. **Signature round-trip + tamper-refusal, no VM needed (AC-81b)** — `scripts/verify-bundle.sh` accepts a correctly-signed throwaway checksum file and rejects one with a single flipped byte, using nothing but `openssl` and a scratch dir (no Docker, no real images required — proves the mechanism in isolation).
2. **Compose rendering correctness (C5)** — `render-release-compose.sh`'s output has `image:` and no `build:` for the four HikRAD services, and is otherwise byte-identical to `deploy/compose.yml`.
3. **No new blocking license path (AC-82a, C6's hard boundary)** — a grep leg over `backend/cmd/hikrad-acct/` and `backend/cmd/hikrad-monitor/` asserting neither file conditions an `os.Exit`/`return err`-from-`main` on `license.State`/`CachedLicenseState()`; both binaries call `platform.RefreshLicenseCache` at boot (grep for the call site) and start a ticker (grep for `time.NewTicker` in the same file/vicinity).
4. **`licenseGate` scope unchanged** — the existing `internal/httpapi/license_gate_test.go` suite still passes unmodified (this phase must not touch that file's behavior at all).
5. **Dev-mode regression (AC-82b)** — the four pre-existing CI jobs (`backend`, `frontend`, `scripts`, `harness-smoke`) stay green exactly as today; the `scripts` job's install.sh idempotency leg in particular proves source-build mode (no `--bundle`/`--registry` flag) is untouched.
6. **Build + vet clean** — `go build ./...`, `go vet ./...` across `backend/`; `bash -n` over every new/changed script.
7. **Docs accuracy** — PRD carries FR-81–83 (already done, this file's preceding commit); `docs/ops/install-guide.md`/`update.md`/`release-checklist.md` describe the bundle/registry flow as actually implemented (spot-checked, not skimmed — same standard `release-checklist.md` already holds itself to); `docs/ops/known-issues.md` carries any bug found while building, dated.

**Human/hardware legs (documented-pending is an acceptable gate outcome for these, per the Phase-5 precedent — note them explicitly in `gate-result.md`, do not silently skip):**
8. **Clean-VM no-source install (AC-81a)** — a real Ubuntu 22.04/24.04 VM with no Go toolchain and no HikRAD git checkout, running `install.sh --bundle hikrad-vX.Y.Z.tar` against a real signed bundle, ends with a healthy stack and a successful PPPoE Access-Accept (reuses the existing packet harness).
9. **Real-bundle tamper-refusal end-to-end** — the same VM, given a bundle with one byte flipped, refuses before touching the live install (item 1 proves the mechanism component-wise; this proves it wired into the actual `install.sh` flow against a real multi-GB tar, which behaves differently than a small scratch-file test in ways worth catching once for real — slow I/O, disk-space edge cases, tar extraction quirks).

## Open implementation questions for whoever builds this (not blocking, but worth a decision-log entry when resolved)

- Whether registry mode (C7) is built at all this phase, or the scope cut to "bundle only" — genuinely optional per C7's own text; if cut, say so plainly in `gate-result.md`.
- `release-vX.Y.Z/` retention count for bundle-mode updates (C4) — pick something, document it, it is not load-bearing for correctness (the pre-update backup remains the real safety net).
- Exact CI trigger mechanics for the release job (separate `release.yml` vs. a job inside `ci.yml` gated on `github.ref_type == 'tag'`) — either satisfies C3, pick the one that fits the existing workflow file's structure better.
