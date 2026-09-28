# Closed-source distribution & support-without-source (FR-126, FR-127)

> Written this phase (v4-3). Restates and makes explicit the closed-source
> posture v4-0/v2-5 already put in place (private canonical repo, signed
> bundles), plus the CI gates and support procedure that keep it holding.

## 1. Closed-source distribution guarantee

- **Canonical repo**: `Hikari0Des/HikRAD` is private (Decision 60, closed
  v4-0). Customers and resellers are never granted git access of any kind.
- **Public mirror**: `Hikari0Des/hikrad-releases` is the only
  customer-facing repository — signed bundle assets and the curl-installer
  wrapper (`scripts/quick-install.sh`) only, never source. Published by
  `.github/workflows/release.yml` on every tagged release.
- **Shipped artifacts**: stripped Go binaries (`-ldflags="-s -w"`), a
  production frontend build with no source maps (`vite.config.ts`'s
  `build.sourcemap: false`, panel and portal), `backend/migrations/*.up.sql`,
  the FreeRADIUS/nginx/systemd config templates, and the installer scripts
  (`install.sh`, `hikrad`, `gen-env.sh`, `verify-bundle.sh`,
  `migrate-to-native.sh`). No `.go`/`.ts`/`.tsx` source file, no `.git`, no
  internal docs, no test fixtures, no vendor tooling (`license-tool/`,
  `release-signing/`).
- **The enforcement point**: `scripts/lint-release-manifest.sh`, wired as
  its own CI job (`release-manifest`) that assembles a real bundle via
  `build-release-bundle.sh` and audits the extracted contents on every
  push/PR — not just at release time. Its deny-list is a frozen contract
  (phase v4-3 brief §4): loosening it requires an amendment, never a local
  exception. Proven to actually fail (planted a `.go` file and a `.map`,
  confirmed rejection, removed them, confirmed a clean pass) — see
  `docs/history/gates/v4-3.md` gate item 9.

## 2. Support-without-source procedure

No support case ever creates pressure to hand over source or grant repo
access. What a support case actually needs, in order of how far it gets:

1. **`hikrad doctor`** — one command, checks every systemd unit, PostgreSQL
   reachability, FreeRADIUS config validity (`freeradius -CX`), nginx config
   validity (`nginx -t`), and whether `hikrad-api` answers
   `/api/v1/system/version`. Non-zero exit on any failure; paste the full
   output.
2. **`GET /api/v1/system/version`** (or the panel's Settings > System page) —
   app version, schema version, update channel. Confirms exactly which
   release the customer is running before anything else is diagnosed.
3. **Service logs** — `hikrad logs <unit>` streams live (`journalctl -f`);
   for a point-in-time bundle to attach to a ticket, capture without
   following:
   ```sh
   journalctl -u hikrad-api -u hikrad-acct -u hikrad-radius-worker \
     --no-pager --since "-2h" > hikrad-support-bundle.log
   ```
   (swap the unit list/window as relevant — `hikrad status` lists every
   unit name).
4. **The License page's exported activation request** (FR-123, "Export
   activation request") — when a case is licence-related, this carries the
   server's own fingerprint and current key id, so the vendor never has to
   ask "what does your server currently think its fingerprint is."

None of the above requires, implies, or should ever escalate into source
access or a repo invite. If a case genuinely cannot be resolved from these
artifacts, the next step is a scheduled remote session where the customer
drives their own terminal — never a source handoff.

## 3. Key custody

Two structurally separate Ed25519 keypairs, on separate rotation domains,
never mixed (mixing them would couple two unrelated compromise/rotation
concerns for no benefit — see `scripts/release-signing/README.md`):

| Key | Authenticates | Lives | Public half embedded in |
|---|---|---|---|
| **Release-signing** | this vendor's own build artifacts (bundle `SHA256SUMS.sig`) | CI secret store (`HIKRAD_RELEASE_SIGNING_KEY`) + an offline backup | `scripts/verify-bundle.sh` (`RELEASE_PUBLIC_KEY_PEM`) |
| **Licence-signing** | customer entitlements (the licence blob) | vendor-only, on the machine(s) running `scripts/license-tool`, **never** in CI, never on any customer-reachable host | `backend/internal/platform/license/license.go` (`ProductionPublicKeyB64`) |

Both directories (`scripts/license-tool/`, `scripts/release-signing/`) live
outside anything `build-release-bundle.sh` copies into a bundle, and
`lint-release-manifest.sh` denies their path prefixes in CI as
defense-in-depth. Each README documents its own dev-key rotation ritual —
required before the first real commercial shipment, not before.

## 4. Secret scanning

`gitleaks` (a static binary, MIT-licensed, zero runtime deps, fully
offline) runs as the `secret-scan` CI job on every push/PR against the
whole working tree, and is available locally via
`scripts/git-hooks/pre-commit` (opt in with
`git config core.hooksPath scripts/git-hooks`) scanning only staged
changes for a fast pre-commit check.

`.gitleaks.toml` extends the default ruleset with an allowlist for this
repo's own verified-by-hand non-secrets (documented dev keys, the two
embedded *public* keys, one vendored FreeRADIUS example placeholder, one
test fixture). Every allowlist entry is commented with why it's safe — a
new entry is only ever added after confirming by hand that the match is
not a real secret; never as a blanket path/directory exclusion, which
would blind the scanner to a real leak landing in that same location later.

To bypass the pre-commit hook with explicit justification (never silently):
`git commit --no-verify`. The CI job is the real gate and cannot be
bypassed this way.

## 5. Explicit non-goal: no DRM

None of the above is, or is intended to become, tamper-proofing. No code
obfuscation, no anti-debugging, no runtime tamper-detection beyond the
licence/fingerprint check itself. On-prem binaries can be reverse-engineered
by a sufficiently motivated party; the protection here is contractual and
practical — no source access, signed and verified updates, and support
cut-off — not technical prevention of reverse engineering. This is a
standing decision (FR-82.4, restated as still-standing at v4-3 kickoff),
not an oversight to "fix" in a later phase.
