# Phase 1 — Agent 2 (RADIUS & NAS): FreeRADIUS wiring, authorize stub, packet harness

> Owns FR-17 (foundation), NFR-8 (packet harness); depends on contracts in [00-phase.md](00-phase.md) (C4, C5, C6 seed user); parallel with Agents 1, 3–5.

## Mission & context
HikRAD authenticates MikroTik PPPoE/Hotspot subscribers through FreeRADIUS 3.2, which delegates every decision to the Go backend via `rlm_rest` (sub-100 ms budget). You wire that path end to end this phase with a **stub** policy (full policy engine is Phase 2), and build the MikroTik-simulating packet harness that CI uses forever (NFR-8). Detail source: sub-PRD [02-radius-nas-aaa](../../prd/02-radius-nas-aaa.md).

## File ownership
- **Exclusive:** `deploy/freeradius/**` (Dockerfile/image pin, `radiusd.conf`, sites, `rlm_rest` module config, dictionaries), `backend/internal/radius/**`, `backend/test/harness/**`.
- **Read-only:** `deploy/compose.yml` (Agent 1 owns; your service's mounts are agreed in C5 — if something's missing, it goes in the merge, not in your edit), `backend/internal/httpapi` (registry API per C3).
- **Forbidden:** `backend/internal/{subscribers,profiles,platform}`, `frontend/**`, migrations.

## Tasks
1. `deploy/freeradius/`: FreeRADIUS 3.2 config — clients from a bootstrap `clients.conf` allowing the harness + a `TEST_NAS_IP` env entry (DB-driven clients are Phase 2); `authorize` section calling `rlm_rest` → `http://hikrad-api:8080/internal/radius/authorize` with the C4 request mapping (PAP password, CHAP fields passed through); reply mapping: `rate_limit` intent → `Mikrotik-Rate-Limit` VSA (load the MikroTik dictionary), `address_pool` → `Framed-Pool`, `session_timeout` → `Session-Timeout`. Accounting section: log-only stub this phase (forwarding to hikrad-acct is Phase 2). [FR-17.1 groundwork]
2. `backend/internal/radius/`: `Module` (per C3 registry) serving `POST /internal/radius/authorize` — Phase-1 stub logic exactly per C4: seeded `testuser`/`testpass` (read via SQL against the C6 `subscribers` table, password compared via the dev seed's known encryption — coordinate shape only through C6, not code imports from D) → accept with `rate_limit "10M/10M"`; unknown user / bad password → reject with proper `reason`. Structure the package now for Phase 2 growth: `authorize.go`, `intents.go` (intent types shared with FreeRADIUS mapping), `stub_policy.go` (clearly marked to be replaced).
3. `backend/test/harness/`: Go RADIUS client harness (e.g. layeh/radius) simulating a MikroTik NAS — send Access-Request (PAP + CHAP variants), assert Accept/Reject + VSAs; runnable against the compose stack (`make test-harness-smoke`) and as a Go integration test in CI. Include a load mode flag (`-rate`, `-duration`) — used in Phase 5 perf verification (NFR-1). [NFR-8]
4. Document `deploy/freeradius/README.md`: how the auth path flows, how to add a test NAS IP, how to run the harness.

Edge cases: CHAP requires the cleartext password server-side — the stub must demonstrate CHAP verification working (this validates the NFR-4.2 reversible-storage decision early); rlm_rest timeout must be set (2 s) with a clear Reject on backend-down, not a hang.

## Contracts consumed/exposed
- **Consumes:** C4 authorize shape (you implement the server side + FreeRADIUS client side of it), C5 service names/ports, C6 seeded subscriber row, C3 module registry.
- **Exposes:** working FreeRADIUS→backend path and the harness CLI/Go API — Phase 2's C and Phase 5's perf gate build on the harness; the intent-mapping config that Phase 2's vendor adapter formalizes.

## Definition of done
- Gate item 3: harness against `docker compose up` gets Accept-with-Mikrotik-Rate-Limit for testuser (PAP **and** CHAP), Reject with reason for bad password/unknown user.
- Harness integration test wired into CI and green; backend-down scenario rejects within 2 s.
- Unit tests: intent→attribute mapping table; authorize handler request parsing/validation.

## Handoff
Phase 2 receives: proven FreeRADIUS↔backend transport, the intent vocabulary, a harness to regression-test the real policy engine, and the `internal/radius` package skeleton you'll replace the stub inside (same agent role continues).
