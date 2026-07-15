# license-tool

Vendor-side license issuer for HikRAD (FR-50.1/50.4). Standalone Go module
(its own `go.mod`) so it can never be pulled into a product image — it lives
under `scripts/`, which the Docker build context never `COPY`s (only
`backend/` and `deploy/docker/healthcheck.go` are).

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
