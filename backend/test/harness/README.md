# RADIUS packet harness (Phase 1, Agent 2 — RADIUS & NAS; NFR-8)

Simulates a MikroTik NAS sending Access-Requests to a real FreeRADIUS,
proving the whole path frozen by contract C4:

```
harness --Access-Request(PAP or CHAP)--> freeradius:1812 --> hikrad_authorize
  --> POST /internal/radius/authorize --> hikrad-api --> Access-Accept/Reject
```

See `deploy/freeradius/README.md` for how FreeRADIUS wires that middle hop.

## Run it

Against a stack already brought up (`make up` from the repo root, then
`make seed`):

```sh
make -C backend test-harness-smoke
```

Or directly:

```sh
cd backend
go run ./test/harness -addr 127.0.0.1:1812 -secret testing123
```

Expected output: five `[PASS]` lines (PAP accept, PAP reject on wrong
password, PAP reject on unknown user, CHAP accept, CHAP reject on wrong
password) and `all cases passed`, exit code 0. Any `[FAIL]` line means the
authorize path is broken somewhere between FreeRADIUS and hikrad-api; check
`docker compose logs freeradius hikrad-api`.

## Flags

| Flag | Default | Meaning |
|---|---|---|
| `-addr` | `127.0.0.1:1812` | FreeRADIUS auth address |
| `-secret` | `testing123` | shared secret — must match a `clients.conf` entry (the stock `docker_bridge_dev`/`localhost` entries cover this by default) |
| `-nas-ip` | `10.0.0.99` | `NAS-IP-Address` reported in requests |
| `-timeout` | `5s` | per-request timeout |
| `-rate` | `0` | load mode: sustained requests/sec against `testuser`/`testpass` (PAP). `0` runs the five-case smoke suite once and exits |
| `-duration` | — | required with `-rate`: how long to sustain it |

Load mode (`-rate`/`-duration`) is the NFR-1 perf-verification hook Phase 5
drives for the sub-100ms p99 budget; it isn't part of the Phase-1 gate.

```sh
go run ./test/harness -addr 127.0.0.1:1812 -rate 50 -duration 30s
```

## As a Go test (CI)

`smoke_test.go` runs the same five cases, gated on `HIKRAD_TEST_RADIUS_ADDR`
(mirrors `cmd/hikrad-api/integration_test.go`'s `HIKRAD_TEST_DB_URL`
pattern) so `go test ./...` skips it wherever no live stack is reachable:

```sh
HIKRAD_TEST_RADIUS_ADDR=127.0.0.1:1812 go test ./test/harness
```

`.github/workflows/ci.yml`'s `harness-smoke` job runs `make -C backend
test-harness-smoke` (the CLI, not this env-gated test) against a stack it
brings up itself.
