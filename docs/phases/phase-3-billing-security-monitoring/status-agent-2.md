# Phase 3 — Agent 2 (RADIUS & NAS) status

**Done. Build + `go vet` + `go test ./internal/radius/... ./test/harness/...` green; my gate legs pass.**

Delivered (exclusive paths only; migration 0220):
1. **Enforcement worker** (`enforce.go`, FR-9/10, gate item 4) — consumes `enforce.quota_exceeded` + `enforce.expired`, resolves behavior via AuthView, CoA per session (quota: block→Disconnect / throttle→ApplyRate / expired_pool→MovePool; expiry: block→Disconnect / expired_pool→MovePool+minimal rate). Idempotent via `enforcement_actions` dedup (5-min bucket, DB-level so it survives restart), per-action audit, NAK→Disconnect fallback (FR-15.4) with retry/backoff, `enforce:failures` Redis counter for C.
2. **Debug stream** (`debug_api.go`, FR-39, gate item 7 half) — `GET /api/v1/live/debug` SSE tail of `radius:decisions`, username/nas_id filters, `nas.view`-gated.
3. **TOD sweeps** (`tod.go`, FR-11) — boundary CoA (Asia/Baghdad), publishes `tod.window {profile_id, active}` for C; **vendor burst** composition in the MikroTik adapter (`ComposeRate`), wired into auth reply + AuthView burst fields.
4. **Hotspot template** (`hotspot.go`, FR-18) — `GET /api/v1/nas/{id}/hotspot-package` zip (login.html+css+md5.js+README), branding-themed, username/password **and voucher** login; voucher-format detection + `VoucherAuthenticator` seam in the authorize path.
5. **Harness**: `-mode enforce` (seed live session + publish event → observe CoA) and `-mode voucher-login`.

Seams D/A must wire (degrade safely until then): `SetTODProvider`, `SetVoucherAuthenticator`, `SetVoucherPrefix`. AuthView gained backward-compatible burst fields (+ `ExpiredPoolName`/`ThrottleRate` already present) — flagged C4 extension like `StaticIP`.

Manual (DoD): hotspot template + voucher login on a real MikroTik/CHR — pending pilot router; harness `voucher-login` mode automates the packet half.
