# Phase 4 — Agent 1 (RADIUS & NAS) status

**Done.** ROS quirk matrix (`docs/ops/ros-matrix.md`) encoded into `vendor.Adapter.SupportsInPlace` — CoA now skips a doomed round trip instead of NAK/timeout for `address_pool` (never in-place on MikroTik) and `rate_limit` on Hotspot; every existing caller already had a Disconnect fallback, so this is latency-only.
NAS API auto-setup (FR-56.2-56.4, migration `0320`): `POST /api/v1/nas/{id}/auto-setup/{preview,apply}`, RouterOS API client (`go-routeros/routeros`) wrapped entirely inside `internal/radius/vendor/` (lint-vendor-isolation passes), additive-only, whole-apply-abort-on-conflict, apply recomputes+hashes the plan server-side so router drift between preview/apply is caught with no session table. Apply refused per-ROS-version until validated (`ros_matrix.go`).
CoA hardening: `coaMaxInflight`/`enforceMaxConcurrent` storm caps, `coa:metrics` Redis counters for C's health page.
Walled-garden: existing `HIKRAD_PORTAL_HOSTS` mechanism documented as the single source for portal + gateway hosts (operator-configurable by design — gateway domains aren't ours to hardcode).
Harness gained `-mode ros-matrix` for pasteable per-version evidence.

**Verified for real**, not just compiled: spun up an isolated Postgres and ran the full `internal/radius`/`vendor`/`harness` suites plus the auto-setup negative tests (planted conflict, hash-staleness, wrong creds) against it — all green. `-race` unavailable here (no gcc); CI has it.

**Gaps, honestly:** ROS quirk findings are pilot-pending (documented, one-line-flippable) — no real MikroTik/CHR reachable from this sandbox; matrix doc §5 is the manual bring-up checklist. Full `docker compose` harness-smoke not run (native-Windows FreeRADIUS bind-mount limitation, pre-existing). `go build ./...`/`go vet ./...` currently fail in `internal/portalapi`/`billing` — Agent D's in-progress work, unrelated to this scope; my gate legs scope to my own packages so they aren't masked by it.

17/17 of my `scripts/gate-phase-4.sh` legs pass.
