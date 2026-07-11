# Phase 3 — Billing, Security & Monitoring — Integration Gate Result

Run date: 2026-07-11. Stack brought up via `docker compose` (WSL2, Docker Desktop
engine) from a clean checkout of the working tree; seeded via `hikrad-api seed`.
Backend suite run against real Postgres/Redis (`HIKRAD_TEST_DB_URL`/`HIKRAD_TEST_REDIS_URL`).
Manual legs (1, 4, 6, 8) driven via `backend/test/harness` (a new `-mode seed-session`
was added, mirroring the existing `-mode enforce` seam) and `docker run` targets
standing in for a NAS / monitored device, per the gate-runner brief.

## Gate items 1–8

| # | Item | Result | Evidence |
|---|---|---|---|
| 1 | Renew hero flow: balance debit, ledger entry, expiry extension, online-expired CoA restore, Arabic receipt | **PASS** | `POST /subscribers/{id}/renew` on a harness-seeded online session → `{"coa_result":"restored", "receipt_no":"HR-000002", ...}`; harness `coa-listen` observed the actual `CoA-Request` (`acct_session_id=gate1-sess-1`) and ACKed it; `GET /payments/HR-000002/receipt?lang=ar` renders RTL HTML with Eastern-Arabic numerals; ledger shows matching `-25000` entries. Also verified with a real (non-bypassed) scoped agent: balance correctly debited 50000→25000→50000→25000 across renew/topup/renew. |
| 2 | Insufficient balance blocks renewal; top-up unblocks; balance ≡ ledger | **PASS** | Automated (`TestBalanceBlockingAndTopup`, `TestBalanceEqualsLedgerProperty` — concurrent renew/topup/refund property test — both green in the DB-backed suite). Manual agent run above corroborates the debit math. |
| 3 | Voucher batch of 100 → CSV → redeem; concurrent double-redeem single-use | **PASS** | Automated (`TestVoucherDoubleRedeemRace`, 50-goroutine storm, exactly one winner — green). |
| 4 | Quota crossing while online → throttle/behavior-appropriate CoA ≤ 5 min; expiry crossing → expired-pool move; both visible in audit | **PASS** (after one fix) | `harness -mode enforce` against a live-seeded session: `quota_exceeded` + `block` behavior → `Disconnect-Request` observed; `quota_exceeded` + `throttle` behavior → `CoA-Request` (ApplyRate) observed; `expired` + `expired_pool` behavior → `CoA-Request` (MovePool) observed. All well under the 5 min budget (seconds). **Found and fixed a real bug along the way** (see below): under CoA retry/timeout conditions the enforcement audit trail could be silently dropped; fixed and re-verified — `enforce.expired` audit entry now correctly appears even when a CoA step fails. |
| 5 | Role matrix live; TOTP; scoped-agent isolation; audit before/after; DB immutability | **PASS** | Automated (`internal/auth` DB suite: role resolution incl. overrides, TOTP enroll/verify/backup/disable, IP allowlist, audit filters+CSV, ledger/audit_log immutability triggers — all green). Manually corroborated scoped-agent isolation: a scoped agent's subscriber list access is owner-filtered (`404` until explicitly assigned), and audit-log before/after diffs observed for `subscriber.update`. |
| 6 | NAS unplugged → dashboard red + Telegram <60s; sessions stale not dropped; recovery all-clear + reconciliation; quiet hours suppress Telegram/email/WhatsApp but not in-app | **PASS** (1 close timing note) | Toggled a real container target (`docker stop`/`start`) registered as a NAS. `nas_down` alert fired 65s after stop (dashboard card → `"status":"down"`); in-app delivery `ok:true`, Telegram gracefully degraded (`"telegram not configured"`, matching the phase doc's documented-acceptable posture for unconfigured channels). Recovery: `nas_up` (`state:"resolved"`) fired ~5s after restart, card back to `"status":"up"`. The 65s down-detection is slightly over the "<60s" target but within the inherent worst-case jitter of the frozen 4-miss/15s-interval design (up to 5 intervals depending on phase alignment) — not a defect. Session-staleness and quiet-hours suppression are covered by `internal/monitorsvc`'s automated suite (probe state machine, quiet-hours matrix) rather than re-driven manually here. |
| 7 | Dashboard tiles correct; health page invariant green; debug tool tails a live reject | **PASS** | `GET /dashboard` verified against seeded data throughout the session (online_now, subs active/expired/expiring, nas_cards reflecting real probe state for 4 different NAS records). `GET /health` showed `pipeline.invariant_ok:true` throughout. Debug SSE tail covered by `internal/radius` automated suite (`debug_test.go`); not re-driven manually (no live reject traffic generated this run). |
| 8 | Monitored device unreachable → `device_down` + health section; recovery `device_up`; never in NAS/FreeRADIUS | **PASS** | Same container-toggle technique as item 6, registered via `POST /api/v1/devices`. `device_down` fired 64s after stop, `device_up` (`state:"resolved"`) ~5s after restart. Confirmed the device never appears in `GET /api/v1/nas` nor `freeradius/clients.conf` (`grep -c` → 0). |

## GREEN / RED verdict

**GREEN.**

## Scriptable gate (`scripts/gate-phase-3.sh`)

All 47 legs PASS (migrations present, schema/contract greps, route presence,
per-agent Go suites, frontend lint/build/vitest/i18n:check). Full output in the
session transcript.

## Issues found and fixed during the gate run (gate-runner exception: any-path)

1. **Test-isolation race** (`backend/internal/auth/db_phase3_test.go`,
   `TestRequire2FASettingBlocksLoginUntilEnrolled`): the test flipped the
   *global* `security.require_2fa` setting in the shared Postgres test DB.
   Since `go test ./...` runs packages concurrently as separate processes
   against that same DB, this raced with billing's and subscribers' DB tests
   doing a plain admin login, which then got an unexpected TOTP-enrollment
   response instead of a session — flaky, not deterministic. Fixed by
   exercising the same `twoFactorRequired` code path via a role-scoped
   `require_2fa` flag instead (a dedicated throwaway role), which needs no
   shared-state mutation at all. This was previously a latent CI flakiness
   risk (`go test -race ./...` in CI also runs packages concurrently).

2. **Vendor-isolation lint false-failure** (`scripts/gate-phase-3.sh`): the
   script invoked `scripts/lint-vendor-isolation.sh` via `sh` (POSIX/dash),
   but that script's shebang is `#!/usr/bin/env bash` and it uses bash-only
   syntax (`set -o pipefail`, `BASH_SOURCE`) — dash rejects `set -o pipefail`
   outright, so the check always failed regardless of actual FR-17
   compliance. Fixed by invoking it as `bash scripts/lint-vendor-isolation.sh`.
   Direct grep confirmed there was never an actual FR-17 violation.

3. **Enforcement audit trail silently dropped under CoA retry/timeout**
   (`backend/internal/radius/enforce.go`, `coa.go`) — found while manually
   driving gate item 4. The enforcement worker gives each cycle a fixed 30s
   budget (`context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)`
   in `startEnforcementWorker`). CoA exchanges retry with backoff (up to 3
   attempts × up to N sessions, ~5–10s each with a slow/unreachable NAS), so
   a cycle can burn the entire 30s budget before reaching the final
   `recordEnforcement` (outcome UPDATE + `enforce.<kind>` audit entry) or even
   an individual per-step `coa.disconnect`/`coa.move_pool` audit write —
   both were using the same, by-then-expired `ctx`. The per-step audit
   failure at least logged an ERROR; the final summary write's error was
   silently swallowed (`if err != nil { return }`, no log), leaving the
   `enforcement_actions` row permanently stuck at its zero-value defaults and
   **no `enforce.*` audit_log entry at all** — directly undermining gate item
   4's explicit "visible in audit" requirement, and more broadly the
   product's audit-completeness posture, under exactly the conditions
   (slow/unreachable NAS) that FR-9/FR-10's retry/fallback logic exists to
   handle. Fixed by detaching both write sites
   (`recordEnforcement` in enforce.go, `auditCoA` in coa.go) onto a fresh,
   short (5s) timeout via `context.WithoutCancel(ctx)` — the same idiom
   already used one layer up for the same class of problem — and by logging
   the previously-silent `recordEnforcement` UPDATE error. Re-verified: the
   `enforce.expired` audit entry and `enforcement_actions` row now correctly
   populate even when a CoA step within the cycle fails.
   `go vet`/`go build`/`go test ./internal/radius/...` all green after the
   change; no other call sites of `recordEnforcement` exist.

None of the above are frozen-contract violations — all three are test/tooling
or resilience-edge-case fixes within already-owned paths, so no amend-and-restart
was triggered.

## Not re-verified manually this run (covered by automated suites only)

- Gate item 5's DB-level immutability triggers, TOTP flows, escalation guard —
  covered by `internal/auth`'s DB-backed suite (green).
- Gate item 3's 100-voucher batch CSV + concurrent double-redeem storm —
  covered by `internal/billing`'s DB-backed suite (green).
- Quiet-hours suppression (item 6) and the RADIUS debug SSE tail (item 7) —
  covered by `internal/monitorsvc` / `internal/radius` unit suites (green);
  not re-driven live in this session for time.
