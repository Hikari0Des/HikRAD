# Phase 4 — Portal, Payments & PWA — Integration Gate Result

Run date: 2026-07-12. Backend suite run against real Postgres/Redis
(`HIKRAD_TEST_DB_URL`/`HIKRAD_TEST_REDIS_URL`, standalone containers). Frontend
suites run via `npm`. Full `docker compose` stack (all 7 services) also brought
up for real — via WSL2 native filesystem (Windows bind-mounts break
FreeRADIUS/Caddy file-permission checks; a plain `git clone` misses uncommitted
work, so the working tree was `rsync`'d in directly) — and driven with curl,
the RADIUS packet harness, and a small custom PAP client against real
FreeRADIUS + hikrad-api + hikrad-acct + Caddy-served panel/portal dists.

## Gate items 1–10

| # | Item | Result | Evidence |
|---|---|---|---|
| 1 | Noor's flow (login → status/usage/speed, no quota ceiling → voucher redeem → expiry extends → expired-pool session restored via CoA, Arabic RTL) + FR-44 password change | **PASS** (API/backend half); real-phone half **documented-pending** | API half fully proven twice over: (a) `go test ./internal/portalapi/...` against real Postgres/Redis — `TestPortalMeComposition` (Decision-21 body scan), `TestPortalVoucherRedeemSelfTargeted`, `TestPortalSelfUpdateRotatesSessions` (FR-44, old refresh token revoked, old password rejected) all green; (b) live-stack curl walkthrough on the real deployed stack: created a subscriber, portal-logged-in, redeemed a real voucher batch (`POST /api/v1/portal/vouchers/redeem` → `{"coa_result":"not_online", "new_expires_at":...}`), confirmed the renewal path correctly evaluated (and, for a genuinely offline test subscriber, correctly skipped) the CoA-restore step. Raw PAP auth against real FreeRADIUS (`Framed-Pool` reply attribute) confirms the expired→pool routing mechanism is live end-to-end. No real phone / Arabic-RTL visual walkthrough was possible in this environment (no device) — this matches every Phase-4 agent's own status note and is the kind of leg the phase brief itself expects a human + running stack for. |
| 2 | Mock-gateway e2e: create → redirect → 3× replayed callback = one renewal; stuck-pending reconciled; disabled gateway degrades gracefully, voucher path unaffected | **PASS** | `TestMockGatewayLifecycleReplayAndDisabled` (3 replays → exactly 1 `ledger_transactions` row, disabled-gateway 503 + voucher still works), `TestProcessCallbackReplayIdempotent`, `TestReconcileExpiresStaleIntent` — all green, all now individually pinned as named legs in `scripts/gate-phase-4.sh`, confirmed repeat-safe (ran the full gate script 3× back-to-back against the same persistent DB with no drift). |
| 3 | Live-adapter checklist for whichever gateway has credentials | **Documented pending** (sanctioned by FR-23.5) | No ZainCash merchant account in this environment. `zaincash` adapter exists, disabled by default; `mock` ships as the always-on demo/CI path (item 2). |
| 4 | Both PWAs install on Android Chrome, branded, offline screen in airplane mode; iOS install education; SW update toast across a redeploy | **PASS** (asset/serving half); device half **documented-pending** | Beyond static-file-existence checks: on the real deployed stack, `GET /manifest.webmanifest` (panel) and `GET /portal/manifest.webmanifest` both return real branded JSON (200), `GET /portal/sw.js` and `GET /portal/` return 200 through Caddy exactly per the frozen `/`, `/portal/*`, `/api/*` routing contract. No Android/iOS device available to drive an actual install/offline/update-toast cycle. |
| 5 | Panel push: NAS-down alert arrives as a push notification on an installed panel PWA (Android) | **PASS** (backend half); device half **documented-pending** | `internal/push` suite green against real Redis (VAPID idempotence, RFC 8291 encryption round-trip, 410 pruning); alert engine's 4th channel wiring confirmed (`pushSender{}` in `monitorsvc/alerts.go`). **Closed a real gap found during this gate run**: no HTTP route existed for the VAPID public key — only a Go-level `push.EnsureKeys` call — even though the panel's own PWA client code (`frontend/panel/src/pwa/pushApi.ts`) already called `GET /api/v1/push/vapid-public-key` and documented the gap inline. Added the route (`backend/internal/push/module.go`); confirmed live on the real stack: `curl https://localhost/api/v1/push/vapid-public-key` → `{"key":"BJd--..."}`. No Android device to confirm an actual push notification arriving. |
| 6 | IDOR: cross-subscriber read fails; portal login rate-limit verified | **PASS** | `TestPortalIDOR`, `TestPortalTokenAudienceSeparation` — green (pre-existing). **Added the missing rate-limit leg**: no test exercised NFR-4.6's portal login limiter at all. Wrote `TestPortalLoginRateLimit` (5 failed attempts → account lock → correct password also 429s while locked) against the real limiter (`backend/internal/portalapi/ratelimit.go`); uses a synthetic per-test `X-Forwarded-For` IP rather than the shared `httptest` loopback address, since the limiter's IP bucket is real-Redis-backed and outlives a single test run — confirmed the naive version left the suite's shared IP counter locked for 15 minutes, breaking unrelated tests. |
| 7 | ROS matrix: CoA suite green on 6.49/7.x; quirk table published | **PASS** (code-verified portion); hardware portion **pilot-pending** (sanctioned by the doc itself) | `docs/ops/ros-matrix.md` published; `internal/radius/vendor` suite (quirk-gating logic, `SupportsInPlace`) and CoA storm-safety/metrics all green. No real MikroTik/CHR reachable from this environment — the doc's own §5 manual checklist already marks the hardware-dependent findings "pilot-pending," which is correct and unchanged by this gate run. |
| 8 | Auto-setup: preview→apply against a CHR creates only additive entries; planted conflict aborts cleanly; verified on both ROS targets | **PASS** (decision-logic portion); hardware portion **pilot-pending** (sanctioned by the doc itself) | `internal/radius/db_phase4_test.go` (`TestAutoSetup_*`: additive-only, conflict-abort, hash-staleness) green against real Postgres + an in-memory fake RouterOS API. Real RouterOS API wire-protocol validation needs physical/CHR hardware, unavailable here — `ros-matrix.md` §4 already documents this precisely as the boundary of what's provable without hardware. |
| 9 | WhatsApp subscriber messaging: renewal → receipt in subscriber's language; expiry reminder fires; fake path if Meta onboarding pending | **PASS** (fake path, fully sanctioned by the phase brief) | `TestDeliverSubscriberWhatsApp_RequestCaptureFake` and `TestSubscriberEvents_RenewedDeliversWhatsAppReceipt` green, now pinned as named gate-script legs. No Meta Business account in this environment — exactly the condition the phase brief names as acceptable ("documented as pending" — see amendment note, gate item 9). |
| 10 | Scratch-card flow: submit → 1-day trial ≤5s (CoA-restored if online) → pending in API; approve/reject math; double-submit rejected; codes never in list payloads | **PASS** | `TestPortalCardPaymentSubmitAndQueue`, `TestCardTrialGuardsAndApproveAnchoring` (anchoring, one-pending guard), `TestCardRejectNetsZeroAndCooldown` (net-zero reversal + cooldown) — all green, all pinned as named gate-script legs, confirmed repeat-safe. |

## GREEN / RED verdict

**GREEN.**

Every item is either fully scripted-and-green, or falls into a hardware/
merchant-account/messaging-platform "documented-pending" bucket the phase
brief itself explicitly sanctions (items 3, 9's live half, and the hardware
halves of 1/4/5/7/8) — no item failed outright, and no frozen contract
(API shapes, schema, events) was violated or needed amending. Per the
amend-and-restart rule, none of the issues found below rose to that bar, so
no STOP was triggered.

## Scriptable gate (`scripts/gate-phase-4.sh`)

130 legs, all PASS. Extended this run with a "Gate runner: named scenario
legs" section pinning items 2, 6, 9-fake, and 10 to their specific test names
(rather than relying on whole-package `go test` runs alone), so a regression
in one gate-relevant scenario surfaces on its own line. Ran the full script
three times back-to-back against the same persistent Postgres/Redis with no
drift (see isolation fixes below) — confirms it's safe to re-run, which the
original whole-package-only version was not.

## Issues found and fixed during the gate run

1. **Missing VAPID public-key HTTP route** (`backend/internal/push/module.go`)
   — Agent 4's panel PWA client already called
   `GET /api/v1/push/vapid-public-key` and flagged in its own status note that
   no such route existed (only a Go-level call). Added
   `handleVapidPublicKey` (unauthenticated by design — the VAPID public key is
   not a secret, and both panel and portal need it before finishing their own
   auth handshake). Confirmed live against the real deployed stack.

2. **Missing portal-login rate-limit test coverage** (gate item 6) — no test
   exercised `internal/portalapi/ratelimit.go`'s NFR-4.6 limiter at all.
   Added `TestPortalLoginRateLimit`.

3. **Test-isolation bug in the new rate-limit test** (found immediately after
   adding #2) — the naive version sent requests through the shared
   `httptest` client, whose source IP is always the loopback address; since
   the limiter's IP-bucket state lives in real Redis with a 15-minute TTL
   (not reset between test runs), the account-lockout test poisoned a
   suite-wide shared key and broke unrelated tests (`TestPortalMeComposition`
   started getting `429`s). Fixed by sending the test's requests with a
   synthetic per-test `X-Forwarded-For` address instead of relying on the
   shared loopback identity.

4. **Two pre-existing test-isolation bugs, surfaced by re-running the gate
   script** (`backend/internal/portalapi/db_test.go`,
   `backend/internal/billing/payments_internal_test.go`) — both tests
   asserted *global* counts against tables that aren't per-test (`gateway_configs`,
   `card_payments`): `TestMockGatewayLifecycleReplayAndDisabled` assumed "no
   gateway is enabled yet" and `TestPortalCardPaymentSubmitAndQueue` /
   `TestCardTrialGuardsAndApproveAnchoring` assumed "exactly one pending row
   exists." Each passes in isolation against a fresh database (which is all a
   single `go test ./...` run, or CI's fresh service container, ever
   exercises) but fails the *second* time the same package runs against a
   persistent DB — exactly the scenario a gate script that re-invokes named
   tests on top of a whole-package run creates. Fixed by (a) having the
   gateway test explicitly reset its own precondition instead of assuming a
   pristine DB, and (b) filtering to the row each test actually created
   (by username / by card ID) instead of asserting the table is a singleton.
   Verified: ran the full backend suite twice in a row against the same
   database with `-count=1` (defeats Go's test cache) — clean both times.

5. **Real deployment bug, found while bringing up the stack for the physical
   legs — flagged, not fixed (out of Phase 4's path ownership; `deploy/`
   outside `deploy/freeradius/` belongs to Agent A / Phase 1)**:
   `hikrad-acct` crash-loops on **any** fresh install
   (`accounting: open spill wal: open /spill/acct-spill.wal: permission
   denied`). Root cause: `deploy/compose.yml`'s `hikrad-acct` service bind-mounts
   `${HIKRAD_DATA_DIR:-./data}/acct-spill` to `/spill`; on a machine where
   that host directory doesn't exist yet, Docker auto-creates it owned by
   `root`, which shadows the image's own `chown hikrad /spill` (baked in at
   build time, in `deploy/docker/acct.Dockerfile`) the moment the bind mount
   is applied at container start — the container then runs as uid 10002 and
   can't write its own spill WAL. Reproduced twice from scratch (two
   independent fresh `docker compose up` runs). Confirmed `scripts/install.sh`
   doesn't chown this path either (`mkdir -p "$HIKRAD_ROOT/data" ...` only,
   line 86) — so this would hit a real production install identically, not
   just this sandbox. This directly threatens the product's core M2 claim
   ("never lose an Accounting-Request") on day one of any pilot. Worked
   around locally with `docker run --rm -v .../acct-spill:/spill alpine
   chown -R 10002:10002 /spill` to continue the gate walkthrough; the real
   fix (an entrypoint chown step in `acct.Dockerfile`, or
   `scripts/install.sh` pre-creating+chowning the path, or switching to a
   named Docker volume) belongs to whoever next touches `deploy/`.

None of the above are frozen-contract violations — items 1–4 are
implementation/test seams within already-owned Phase-4 paths (fixed
directly, per the gate-runner brief); item 5 is a real bug but outside this
phase's path ownership, so it's reported rather than patched.

## Not re-verified manually this run (covered by automated suites only)

- Gate item 7's CoA rate-change/pool-move in-place-vs-fallback semantics —
  covered by `TestSupportsInPlace_QuirkMatrix`,
  `TestWorker_ThrottleNAKFallsBackToDisconnect`,
  `TestWorker_ExpiryExpiredPool_MoveNAKFallsBack` (green); the real-hardware
  half is `ros-matrix.md`'s own documented pilot-pending scope.
- Gate item 8's real RouterOS API wire-protocol behavior (sentence syntax,
  singleton semantics) — the decision-logic half is proven against a fake
  in-memory device; the wire half needs the same hardware as item 7.
- Gate item 9's live Meta WhatsApp Business path — no Meta account in this
  environment; the fake-path proof is the phase brief's own sanctioned
  fallback.
