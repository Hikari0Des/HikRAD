# Phase 4 — Agent 1 (RADIUS & NAS): ROS 6/7 quirk matrix, CoA hardening, walled-garden completion

> Owns the MikroTik-quirks risk mitigation (master risk table; sub-PRD 02 §7), FR-15 hardening, FR-14 walled-garden completion. Depends on contracts in [00-phase.md](00-phase.md) (C5 branding hosts); parallel with Agents 2–4.

## Mission & context
The master PRD names "MikroTik CoA/attribute quirks across RouterOS 6/7" a High-impact risk; by now CoA carries renewals, enforcement, and TOD sweeps — it must be proven per ROS version, not assumed. You build the ROS test matrix, encode discovered quirks into the vendor adapter, and complete the walled-garden config so expired subscribers can actually reach the portal + payment gateways from inside the expired pool (the redirect-to-renew loop that closes the business case). Detail source: sub-PRD [02-radius-nas-aaa](../../prd/02-radius-nas-aaa.md).

## File ownership
- **Exclusive:** `backend/internal/radius/**`, `deploy/freeradius/**`, `backend/test/harness/**`, `docs/ops/ros-matrix.md`.
- **Read-only:** portal/payment host requirements (D's gateway adapters document their callback/redirect hosts — frozen at phase start in C3 adapter docs). **Forbidden:** `internal/{portalapi,billing,push}`, `frontend/**`.

## Tasks
1. **ROS matrix harness**: scripted scenario suite runnable against real devices/CHR images for ROS 6.49 and 7.x — PAP/CHAP auth, all reject reasons, expired-pool accept, CoA Disconnect, CoA rate-change (in-place support differs by version), CoA pool-move, burst rate-limit strings, hotspot voucher login. Automate what CHR allows; document manual steps for the rest.
2. **Quirk table** → `docs/ops/ros-matrix.md`: per-version findings (supported CoA operations, attribute casing, timing) — and encode them: vendor adapter consults `nas.ros_version` to choose strategies (e.g. rate-change unsupported → transparent Disconnect fallback, already typed in Phase 2 FR-15.4 — now version-aware instead of NAK-reactive where knowable).
3. **Walled-garden completion**: FR-14 snippet + hotspot package gain the full expired-pool garden: portal host, Caddy-served assets, each enabled gateway's redirect/callback hosts (from adapter docs), DNS notes; verify an expired-pool client can complete a voucher redemption and a mock payment through the garden.
4. **CoA hardening**: retry/backoff tuning under packet loss (harness-simulated), concurrent CoA storm safety (enforcement worker bursts), metrics counters per operation/result for C's health page.
5. Regression: full Phase-2/3 harness suites stay green on both ROS targets.

Edge cases: ROS 7 changed some VSA behaviors vs 6.49 — where behavior is truly irreconcilable, the adapter must degrade predictably and the matrix doc must say so for support staff; garden rules must not open general internet to expired users (test negative reachability too).

## Contracts consumed/exposed
- **Consumes:** C3 adapter host lists (D), existing CoA/enforcement code (own, Phase 2/3).
- **Exposes:** version-aware adapter behavior (everyone relying on CoA), `docs/ops/ros-matrix.md` (support + pilot onboarding), garden-complete snippets (pilot install).

## Definition of done
- Gate item 7 passes; gate item 1's expired-pool → renew → CoA-restore leg verified through the garden on both ROS versions.
- Negative tests: expired-pool client cannot reach non-garden hosts; CoA storm (100 ops/min) causes no drops/misfires.
- Quirk handling has unit tests keyed by ros_version; matrix doc reviewed and committed.

## Handoff
Phase 5 receives a CoA layer trusted for pilot go-live and the ops document the pilot's ROS-version choice (master open question 2) plugs into; no further RADIUS work is scheduled before v1.
