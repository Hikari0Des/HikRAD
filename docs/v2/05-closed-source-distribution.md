# v2-05 — Closed-source distribution & licensing hardening

> Owner request 2026-07-16 (item 17): "what's the point of license if everyone can use it — make it a closed-source system, and I'm the one who gives licenses." v1's offline license already gates activation/grace, but customers receive the **source checkout** (compose builds images on-prem), so a determined customer can read or patch the checks. This feature changes the delivery model itself.

## 1. Problem

1. `install.sh`/`hikrad update` build images **from source on the customer's server** — the whole repo ships to every install.
2. The license check lives in readable Go source a customer could patch out and rebuild.
3. There is no artifact pipeline producing versioned, distributable release bundles at all — "release" currently means "git checkout".

## 2. Requirements (draft — renumber as FR-6x at kickoff)

### FR-A — Binary release pipeline
- CI release job builds and pushes **compiled multi-service images** (api/acct/monitor + prebuilt frontend in Caddy image) tagged `vX.Y.Z`, plus an **offline bundle** (`hikrad-vX.Y.Z.tar`: docker-save'd images + compose file + scripts + migrations checksummed and signed).
- `install.sh` and `hikrad update` gain an image-based mode (registry pull or `--bundle`) and **stop requiring the source tree**; the on-server footprint becomes: compose file, scripts, .env, data. Source-build mode remains available behind a flag for development only.
- Bundles are signed (minisign/cosign-style detached signature, public key baked into the installer); `hikrad update` refuses unsigned/tampered bundles.

### FR-B — Licensing hardening (honest scope)
- License keys become **per-customer signed artifacts** (already offline-verified) that also gate registry pull credentials — you issue both.
- Move the license check deeper: verify in all three binaries at boot (not just api), re-verify periodically at runtime, and bind to the existing machine fingerprint. Grace/expired-grace semantics unchanged (FR-50).
- Accept the limit and document it: compiled Go + no source raises the bar dramatically, but on-prem software can never be tamper-proof. No DRM rabbit holes (obfuscators, anti-debug) — enforcement is contractual + practical (no source, signed updates, support cut-off).

### FR-C — Repo/business hygiene
- The GitHub repo stays private; customers never get git access. Public artifacts: docs excerpts + marketing only.
- `docs/ops/release-checklist.md` gains the signing + registry steps; VERSION file drives image tags (already wired in v1.1).

## 3. Impact map

| Touched | Built in | Change |
|---|---|---|
| `.github/workflows` | Phase 1 (A) | release job: build, sign, push, bundle |
| `scripts/install.sh`, `scripts/hikrad` | Phases 1/5 (A) | image-mode install/update, signature verification, no-source footprint |
| `deploy/compose.yml` | Phase 1 (A) | image: tags with build: fallback for dev |
| `internal/platform/license` | Phase 5 (A) | boot-time check in acct/monitor, periodic re-verify |
| docs/ops | Phase 5 | release checklist, customer-install guide rewrite |

## 4. Acceptance sketch

- A clean Ubuntu VM installs and runs HikRAD from a signed bundle **with no Go toolchain and no source tree present**; `hikrad update --bundle` upgrades it; a bit-flipped bundle is refused.
- All three services refuse to run with a missing/invalid license beyond grace, on a fingerprint-changed machine, per FR-50 semantics.
- Dev workflow (`make up` from checkout) still works unchanged.

## 5. AI kickoff prompt (paste into a fresh Claude Code session at repo root)

```text
You are working in the HikRAD repo. v1 is complete; we are starting v2 phase 6: closed-source distribution & licensing hardening. You work SOLO — no parallel agents; execute sequentially (release pipeline → installer/update image-mode → license hardening → docs), committing in reviewable chunks.

Read, in this order and nothing else yet: CLAUDE.md, docs/v2/phases/00-v2-execution-plan.md, docs/v2/05-closed-source-distribution.md, docs/prd/01-platform-security.md FR-49/FR-50/FR-51/FR-52, scripts/install.sh, scripts/hikrad, .github/workflows/ci.yml, backend/internal/platform/license/.

Step 1 — Amend the docs (single commit): new FR rows + Decisions Log row in docs/PRD.md (delivery model changes from source-checkout to signed images/bundles), update sub-PRD 01 and docs/prd/00-index.md.

Step 2 — Create docs/v2/phases/phase-v2-6-closed-source/00-phase.md with frozen contracts (bundle layout + signature scheme, registry naming, install.meta deltas, license re-verify cadence) and the integration gate (clean-VM no-source install, tamper-refusal test, dev-mode regression; migration range 0570–0579 if any). Scriptable gate items → scripts/gate-v2-phase-6.sh.

Step 3 — Stop and present the phase brief for my confirmation before writing feature code.

Constraints: NFR-7 — installs/updates must work fully offline via bundles; no DRM/obfuscation/anti-debug work (explicitly out of scope); grace semantics (FR-50) unchanged. Update every doc invalidated (install-guide, update.md, release-checklist); record bugs in docs/ops/known-issues.md.
```
