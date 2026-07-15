# RouterOS 6.49 / 7.x quirk matrix

> Owner: Agent B (RADIUS & NAS), Phase 4. Mitigates the master PRD's High-impact
> risk "MikroTik CoA/attribute quirks across RouterOS 6/7" (sub-PRD
> [02-radius-nas-aaa](../prd/02-radius-nas-aaa.md) §7) and gates FR-56 auto-setup
> per version (contract C6). Read this before touching CoA fallback logic,
> `internal/radius/vendor/mikrotik*.go`, or the FR-14 snippet.

## How to read this document

Each finding below is one of two kinds:

- **Code-verified** — proven by an automated test in this repo (unit test or
  the packet-level harness, `backend/test/harness/`), runnable with no real
  hardware. These are the scenarios CI actually re-checks on every change.
- **Pilot-pending** — MikroTik's documented/well-known behavior, encoded into
  the adapter as the current default, but not yet exercised against a
  physical router or CHR image from this environment (no network access to
  provision one here). Flipping a pilot-pending finding after a real-hardware
  run is a one-line change in `internal/radius/vendor/mikrotik_autosetup.go`
  (`SupportsInPlace`) or `docs/ops/ros-matrix.md` — no API or schema change.
  **Before a pilot go-live on a given ROS build, run the manual checklist in
  §5 against that exact build and update this table.**

## 1. Scenario suite

Automated (`backend/test/harness`, see its `-mode` flags and
`internal/radius/*_test.go`):

| Scenario | Harness / test | Status |
|---|---|---|
| PAP accept + reject | `-mode smoke` (`smoke.go`) | code-verified |
| CHAP accept + reject | `-mode smoke` | code-verified |
| Reject reasons (bad password, expired/blocked, MAC mismatch, session limit, unknown user/NAS, service not allowed, quota exhausted) | `internal/radius/authorize_test.go` | code-verified |
| Expired-pool accept (`redirect_expired` intent → address-list) | `internal/radius/authorize_test.go` (`TestExpiredPoolAccept`) | code-verified |
| CoA Disconnect | `-mode coa-listen` + `TestCoARoundTripReal`/`TestCoAAckNak` | code-verified |
| CoA rate-change (in-place vs. fallback) | `TestSupportsInPlace_QuirkMatrix`, `TestWorker_ThrottleNAKFallsBackToDisconnect` | code-verified (fallback path); real in-place rate-limit-on-active-PPP-session — pilot-pending |
| CoA pool-move (in-place vs. fallback) | `TestWorker_ExpiryExpiredPool_MoveNAKFallsBack`, `restoreCoA` (billing) | code-verified (fallback path — see §2) |
| Burst rate-limit strings (FR-11) | `vendor.TestComposeRate` | code-verified (string grammar only; not a router round-trip) |
| Hotspot voucher login | `-mode voucher-login`, `TestVoucherLogin_*` | code-verified |
| NAS API auto-setup preview/apply, additive-only, conflict-abort, hash-mismatch-abort | `internal/radius/db_phase4_test.go` (`TestAutoSetup_*`, run against a fake in-memory RouterOS API — see §4) | code-verified logic; real RouterOS API wire behavior — pilot-pending |

Every "code-verified" row above ran clean in this environment, including a
real-Postgres run of the auto-setup suite (`HIKRAD_TEST_DB_URL`) — see CI job
`backend` and `make -C backend test-harness-smoke`.

## 2. Findings: CoA in-place support (`vendor.Adapter.SupportsInPlace`)

Encoded in `internal/radius/vendor/mikrotik_autosetup.go`; consulted by
`coa.go` **before** sending a packet (FR-15.4, "version-aware instead of
NAK-reactive where knowable") — an unsupported combination short-circuits
straight to Disconnect instead of burning a 5s+retry round trip that would
only ever NAK or time out.

| Intent | PPPoE | Hotspot | ROS 6.49 | ROS 7.x | Basis |
|---|---|---|---|---|---|
| `rate_limit` (Mikrotik-Rate-Limit CoA) | supported in-place | **not** supported in-place | same | same | Pilot-pending. MikroTik has long honored an in-place `Mikrotik-Rate-Limit` CoA change against an active PPP session on both major versions. Hotspot sessions bind to a dynamic queue keyed by the Hotspot binding, which historically does not get re-evaluated by a bare CoA on either version — every caller in this codebase (enforcement worker, billing renewal/refund) already treats a failed `ApplyRate` as a Disconnect-fallback trigger, so this finding only changes *how fast* the fallback happens, never the outcome. |
| `address_pool` (Framed-Pool CoA / `MovePool`) | **not** supported in-place | **not** supported in-place | same | same | Pilot-pending, but high-confidence: `Framed-Pool` is read only during authentication; MikroTik does not remap an already-active session's address pool from a CoA-Request on either service type or version. Nothing in the codebase relies on `MovePool` succeeding in-place — `billing.restoreCoA`, `billing.refund`, and the enforcement worker's `expired_pool` steps are all written with `fallback: true` / an unconditional Disconnect-on-`!Ok()` already in place (Phase 2/3). Because `SupportsInPlace` reports `false` unconditionally, `MovePool` now skips the packet entirely and returns `CoAUnsupported` immediately — same fallback outcome, but without the wasted round trip, which matters for gate item 1's "expired → renew → CoA-restore" latency and for CoA-storm safety (§3). |
| `session_timeout` | supported in-place | supported in-place | same | same | Code-verified via `Apply`'s attribute mapping (`rfc2865.SessionTimeout`); no session-state migration is implied. |
| `redirect_expired` (address-list) | supported in-place | supported in-place | same | same | Code-verified: adding a client to a MikroTik address-list is a router-wide structure independent of session internals — this is the existing FR-9/expired-redirect mechanism, unchanged since Phase 2. |

**Support-staff note:** if a pilot install proves `rate_limit` actually does
apply in-place on Hotspot for a specific ROS build, flip that cell's `false`
to `true` (parameterized by `nasType`, and easy to further parameterize by
`rosVersion` if the two majors ever diverge here) — every caller already
tolerates either outcome, so this is a pure latency/reliability win with no
behavior-contract change.

## 3. CoA hardening (this phase)

- **Version-aware short-circuit** (§2): `coaService.send` checks
  `SupportsInPlace` before dialing, saving a guaranteed-failing round trip.
- **Storm safety**: `coaMaxInflight` (64) bounds process-wide concurrent
  CoA/Disconnect exchanges (`coa.go`); `enforceMaxConcurrent` (16) bounds how
  many subscribers' enforcement plans the worker runs at once (`enforce.go`).
  A burst beyond either cap queues — nothing is dropped — so a midnight
  expiry sweep crossing hundreds of subscribers can't self-inflict the packet
  loss the retry logic exists to tolerate. Both are process-lifetime
  in-memory bounds; a restart clears them (no persistence needed — nothing is
  in-flight across a restart by definition).
- **Metrics**: `coa:metrics` (Redis hash, `<op>:<outcome>` → count) is
  incremented on every CoA/Disconnect attempt including version-aware skips;
  `radius.CoAMetrics(ctx)` reads it back. C's health page (`monitorsvc`) can
  poll this the same way it already polls `enforce:failures`
  (`EnforcementFailuresKey`).
- **Retry/backoff**: unchanged from Phase 2 (5s timeout, 1 retry per attempt;
  the enforcement worker separately retries a still-failing session up to 3
  times with linear backoff, `enforceSessions`) — proven under simulated
  packet loss by `TestCoATimeoutThenRetrySucceeds` /
  `TestCoATimeoutExhausted`.

## 4. NAS API auto-setup (FR-56.2-56.4, contract C6)

`POST /api/v1/nas/{id}/auto-setup/preview` and `.../apply` — see
`internal/radius/autosetup_api.go` and
`internal/radius/vendor/mikrotik_autosetup.go`. Safety contract (frozen by
Decision 17): preview only ever issues RouterOS API `print` (read) sentences;
apply recomputes the plan **server-side** from a fresh read and refuses unless
its hash matches the hash the caller got from preview — this is what catches
the router changing state between the two calls, with no stored
preview-session table. A non-empty conflict list aborts the whole apply before
a single write sentence is sent.

**Per-version enablement** (`internal/radius/ros_matrix.go`,
`rosMatrixValidated`): apply is enabled for `ros_version` prefixes `"6"` and
`"7"` (i.e. 6.49+ and any 7.x) and refused — with a pointer to the FR-14
copy-paste snippet — for anything else (empty, or pre-6.49 like `"5.26"`).
Preview is always available (read-only) regardless of this gate.

Marked **code-verified** here means: the additive/conflict/idempotency
*decision logic* (which sentence to add, what counts as "already ours" vs. a
conflict, hash-based staleness detection, whole-apply-abort) is exercised by
`internal/radius/db_phase4_test.go` against a real Postgres NAS row and an
in-memory fake RouterOS API device (`fakeRouter`) that persists writes back
into its own state, so a second preview after apply proves the plan converges
to a no-op. What is **not** yet proven is the literal RouterOS API wire
protocol against a real device/CHR (sentence syntax RouterOS actually accepts,
`/radius/incoming/set` vs `/radius/incoming` singleton semantics, hotspot
profile `.id` targeting) — that's §5's manual checklist, required once before
enabling auto-setup for a given pilot ISP's actual router fleet.

## 5. Manual checklist (run once per ROS build before pilot go-live)

Requires a real router or a MikroTik CHR image — neither is reachable from
this build environment (no network access to provision one), so this section
stays manual per the task's own instruction ("automate what CHR allows;
document manual steps for the rest").

1. **Snippet bring-up**: paste the FR-14 config-snippet output
   (`GET /api/v1/nas/{id}/config-snippet?ros=6|7`) into a fresh router of that
   version; confirm `backend/test/harness -mode smoke` against it passes all 5
   cases, and `-mode voucher-login` for a Hotspot NAS.
2. **CoA disconnect**: dial in, then call the panel disconnect action (or
   `radius.Disconnect` directly); confirm the router drops the session within
   the 5s timeout and FreeRADIUS's next Access-Request re-establishes it.
3. **CoA rate-change**: while connected on PPPoE, trigger `ApplyRate`
   (e.g. via a TOD boundary or the enforcement worker); confirm the router
   applies the new `Mikrotik-Rate-Limit` in place (no drop). Repeat on
   Hotspot; per §2's current finding this is expected to NAK/timeout and fall
   back to Disconnect — confirm that fallback actually happens and the
   session comes back with the new policy after re-auth.
4. **CoA pool-move**: trigger `MovePool` (e.g. an expiry crossing while
   online); confirm it now skips straight to Disconnect (§2) with no wasted
   timeout, and the session re-authenticates into the expired pool.
5. **Auto-setup preview/apply**: run preview against the freshly-bootstrapped
   router — expect a clean, conflict-free, all-no-op plan (everything the
   snippet already added should read back as "ours"). Then, on a *second*,
   unconfigured router of the same version, run preview → apply and confirm
   the router ends up in the same state the snippet would have produced, and
   that FR-14.4's "seen since created" check goes true after a real client
   authenticates. Plant a conflicting manual `/radius` entry pointing
   elsewhere first and confirm apply refuses with the router unchanged.
6. **Walled garden reachability** (§6): from an expired-pool client, confirm
   the portal/payment hosts load and a non-garden host (e.g. a general
   internet host) does **not** — negative reachability, not just positive.
7. Record the outcome (pass/fail per step, ROS build string) in this file's
   §2/§4 tables and flip any finding that turned out different in practice.

## 6. Walled-garden completion (FR-14/FR-18)

The Hotspot config snippet (`vendor.SnippetInput.WalledGarden`) and the
downloadable Hotspot login package (`GET /api/v1/nas/{id}/hotspot-package`,
its README) both render the **same** host list, sourced from
`HIKRAD_PORTAL_HOSTS` (comma-separated, `internal/radius/snippet.go`
`defaultWalledGarden`). Auto-setup's planner (§4) additively creates one
`/ip/hotspot/walled-garden` entry per host not already present.

Operators must list, per pilot deployment:

- The HikRAD portal's own host(s) (and the panel's, if a subscriber-visible
  branding asset is served from a different Caddy host).
- Every **enabled** payment gateway's redirect/callback host, from that
  gateway's own merchant integration docs (e.g. whatever ZainCash's
  onboarding packet specifies for the live merchant account) — this is
  operator/merchant-account-specific and deliberately not hardcoded here; the
  mock gateway needs no extra host since its whole flow stays on HikRAD's own
  API/portal host.
- Any CDN/static-asset host, if the portal doesn't serve its own assets.

DNS note: walled-garden entries match on `dst-host` (a DNS hostname the router
snoops from the client's own DNS queries, not an IP) — the expired-pool client
must be able to **resolve** these hosts too, so the walled garden implicitly
needs the pool's assigned DNS server allowed/working before any HTTP host
matching can take effect. The pool's own DNS settings (`ip_pools` /
`pool_assignments`, `internal/radius/pools_*.go`) are unaffected by this
phase — verify the expired pool's DNS server is one clients can actually reach
un-gated (a resolver inside the garden's own allow list, or the router's own
built-in DNS proxy) as part of §5's manual checklist.

**Negative reachability is part of the contract, not an afterthought**: the
walled-garden list is exhaustive and additive-only — nothing in this phase
adds a wildcard or catch-all entry, and the planner in §4 never proposes one
(`planWalledGarden` only ever emits per-host `dst-host=` entries). §5 step 6
is the manual proof that an expired-pool client genuinely cannot reach the
open internet.
