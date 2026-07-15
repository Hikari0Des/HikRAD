# ZainCash gateway — redirect/callback hosts

Frozen input for B's walled-garden task (Phase 4, C1/C6): a subscriber sitting
in the expired-pool walled garden must still be able to reach ZainCash's
hosted payment page to renew. Allow, at minimum:

- `api.zaincash.iq` — transaction init (`POST /transaction/init`), payment
  page (`GET /transaction/pay`), and status query (`POST /transaction/get`);
  see `internal/billing/gateways/zaincash/zaincash.go`.

The redirect the portal sends the subscriber's browser to is
`https://api.zaincash.iq/transaction/pay?id=<txn>` (host as above).

Callback delivery: ZainCash returns the subscriber's browser to HikRAD's own
`redirect_url` (configured per-merchant, portal-hosted — no external host to
allow on our side) carrying a signed `token` query parameter that
`VerifyCallback` decodes; the callback route itself
(`POST /api/v1/payments/zaincash/callback`) is served by `hikrad-api`, not
ZainCash-hosted, so it needs no walled-garden entry either.

**Status (ship-what's-available, FR-23.5):** no live merchant account exists
yet. The host above and the JWT init/callback/status shapes follow ZainCash's
public merchant integration docs as best understood without one — re-verify
against current docs and a real sandbox before enabling
(`gateway_configs.enabled = true` for `zaincash`) in production. See the
package-level doc comment in `zaincash.go` for the full caveat.
