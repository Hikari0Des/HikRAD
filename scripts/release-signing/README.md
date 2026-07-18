# Release signing key

Signs `hikrad-vX.Y.Z.tar` release bundles (FR-81.3, phase v2-5 C1) — a
**separate** keypair from `scripts/license-tool`'s license-issuance key.
That key authenticates *customer entitlements*; this one authenticates
*this vendor's own build artifacts*. Same rotation domain neither, same key
never — mixing them would couple two unrelated compromise/rotation
concerns for no benefit.

Ed25519 via `openssl` (no new tool/dependency — `install.sh` already
requires `openssl` for self-signed TLS cert generation):

```sh
openssl genpkey -algorithm ED25519 -out release-signing-key.pem
openssl pkey -in release-signing-key.pem -pubout -out release-public-key.pem
openssl pkeyutl -sign   -inkey release-signing-key.pem -rawin -in SHA256SUMS -out SHA256SUMS.sig
openssl pkeyutl -verify -pubin -inkey release-public-key.pem -rawin -in SHA256SUMS -sigfile SHA256SUMS.sig
```

`-rawin` is required for Ed25519: it signs the message directly (no
separate digest step, unlike RSA/ECDSA `dgst -sign`). Verified working on
this repo's OpenSSL and expected on Ubuntu 22.04/24.04 (both ship OpenSSL
3.0+, which supports Ed25519).

## Dev key — `dev-release-key.pem` / `dev-release-public-key.pem`

These checked-in keys are a **development-only** pair, generated for this
phase's build/testing. The public half is embedded (as a PEM literal) in
`scripts/verify-bundle.sh` right now, which means every bundle built from
this checkout can be signed and accepted with this key — fine for CI, the
gate script, and local rehearsal, **not fine for a real customer shipment**.

**Before shipping the first commercial bundle** (mirrors
`scripts/license-tool/README.md`'s identical ritual for the license key):

1. `openssl genpkey -algorithm ED25519 -out /somewhere-offline/release-signing-key.pem`
   on an air-gapped machine, not this checkout.
2. `openssl pkey -in /somewhere-offline/release-signing-key.pem -pubout` and
   paste the resulting PEM into the `RELEASE_PUBLIC_KEY_PEM` heredoc in
   `scripts/verify-bundle.sh`, replacing the dev key.
3. Store the new private key as the `HIKRAD_RELEASE_SIGNING_KEY` GitHub
   Actions secret (base64-encoded PEM) so the CI release job (C3) can sign
   with it — never commit the private key anywhere.
4. Delete `dev-release-key.pem`/`dev-release-public-key.pem` from the tree
   (or leave them — they stop working the moment step 2 lands, since
   `verify-bundle.sh` no longer trusts them).

## Usage

```sh
# build a bundle's SHA256SUMS + SHA256SUMS.sig (done by the release job, C3):
( cd bundle-staging && find . -type f ! -name SHA256SUMS ! -name SHA256SUMS.sig -exec sha256sum {} + ) > SHA256SUMS
openssl pkeyutl -sign -inkey scripts/release-signing/dev-release-key.pem -rawin \
  -in SHA256SUMS -out SHA256SUMS.sig

# verify (done by install.sh/hikrad update, and directly by the gate script):
sh scripts/verify-bundle.sh <bundle.tar-or-extracted-dir>
```

Nothing here or in `verify-bundle.sh` makes a network call — signing and
verification are both fully offline (NFR-7), same posture as the license
system.
