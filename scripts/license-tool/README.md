# license-tool

Vendor-side license issuer for HikRAD (FR-50.1/50.4, FR-123). Standalone Go
module (its own `go.mod`) so it can never be pulled into a product build —
it lives under `scripts/`, outside anything `scripts/build-release-bundle.sh`
copies into a release bundle (it names each file/directory it stages
explicitly, never globs `scripts/` wholesale), and
`scripts/lint-release-manifest.sh` (FR-126, phase v4-3) denies the
`license-tool/`/`release-signing/` path prefixes in CI as defense-in-depth.
Nothing here is compiled into, embedded in, or shipped alongside any product
binary or bundle.

## Dev/pilot key — `dev-vendor-key.b64`

This checked-in key is a **development and pilot-rehearsal key only**. Its
public half is embedded as `license.ProductionPublicKeyB64` in
`backend/internal/platform/license/license.go` right now, which means every
build from this checkout can issue and accept licenses signed with it — fine
for internal dev, seed data, and the M4 install rehearsal, **not fine for a
real customer sale**.

**Before shipping the first commercial license:**

1. `go run . keygen -out /somewhere-offline/vendor-key.b64` on an
   air-gapped machine, not this checkout.
2. Paste the printed public key into `ProductionPublicKeyB64` and rebuild
   every product image.
3. Delete `dev-vendor-key.b64` from the tree (or leave it — it no longer
   matches the embedded pubkey once step 2 lands, so it stops working) and
   never commit the real private key anywhere.

## Usage

```sh
# One-time (already done for the dev key above):
go run . keygen -out vendor-key.b64

# Issue a license for a fingerprint the buyer sent you (from their wizard's
# license step or GET /api/v1/license):
go run . issue \
  -key dev-vendor-key.b64 \
  -keyid K-2026-0001 \
  -licensee "Example ISP" \
  -tier 5k \
  -max-subscribers 5000 \
  -version 1 \
  -fingerprint "e3b0c44298fc1c14:facc16d28ecb810b" \
  -out example-isp.json
```

`example-isp.json` is what the buyer uploads via the first-run wizard's
license step or `POST /api/v1/license`. Nothing here or on the buyer's side
makes a network call as part of this exchange (NFR-7, AC-50a) — the
fingerprint and the signed license travel by whatever offline channel you
already use (email, support ticket, USB).

### File-exchange activation — `activate` (FR-123, preferred)

The flag-driven `issue -fingerprint <value>` form above requires the buyer to
correctly transcribe their fingerprint — get it stale or mistyped, and the
resulting license lands straight back in grace. `activate` removes that
class of error entirely: the buyer downloads an **activation request** file
from their server (the panel's License page "Export activation request"
button, or `POST /api/v1/license/request-blob`) and sends you that file
instead of typing anything.

```sh
go run . activate -request hikrad-activation-request-<fingerprint>.json \
  -key dev-vendor-key.b64 \
  -keyid K-2026-0001 \
  -licensee "Example ISP" \
  -tier pro \
  -max-subscribers 1000 \
  -entitlements self_service,api_access \
  -out example-isp.json
```

`activate` verifies the request file's self-checksum before signing anything
against it, prints the licensee-so-far/fingerprint/currently-installed-key
for your confirmation, and signs the resulting license against **exactly**
the fingerprint the request file carries — there is no `-fingerprint` flag
on this command at all, so a stale or mistyped fingerprint cannot reach a
signed license by construction. With no flags beyond `-request` on a
terminal it walks you through the remaining fields interactively, same as
`issue`.

### Graphical issuer — `serve`

If you'd rather not remember flags, run a local web form instead:

```sh
go run . serve -key dev-vendor-key.b64 -keyid K-2026-0001
# → open http://127.0.0.1:7070/ in a browser
```

Fill in licensee / tier / fingerprint, pick a tier (a catalog tier fixes its
own subscriber cap; choose *Custom…* to type your own), click **Sign license**
and **Download** the resulting `.json`. It uses the exact same signing core as
`issue`, so the output is byte-identical. The private key is read server-side
and never sent to the browser, and the listener is **loopback-only** unless you
pass `-allow-remote` (which exposes the signer — don't, unless you know why).
