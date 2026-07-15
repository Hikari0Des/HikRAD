# Mock gateway — redirect/callback hosts

Frozen input for B's walled-garden task (Phase 4, C1/C6): the mock adapter
never leaves the process, so it needs **no walled-garden entry**.

- Redirect host: `mock.gateway.hikrad.local` (synthetic; `CreatePayment` returns
  `https://mock.gateway.hikrad.local/pay/<ref>` but nothing ever resolves or is
  fetched — it exists only as a realistic-looking `redirect_url` for the
  portal UI to display/link during development).
- Callback: in-process only. The dev simulator
  (`POST /api/v1/dev/mock-gateway/simulate`) builds a signed callback payload
  and feeds it directly into the same `processCallback` core a real webhook
  uses (`internal/billing/paymentintents.go`) — no HTTP round-trip, no host to
  allow.

Nothing to add to `deploy/freeradius`'s walled-garden config for this adapter.
