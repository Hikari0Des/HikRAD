# HikRAD FreeRADIUS config (Phase 1, Agent 2 — RADIUS & NAS)

This directory is bind-mounted whole at `/etc/raddb` by the `freeradius`
service in `deploy/compose.yml` (Agent 1's frozen contract C5) — it is a
complete FreeRADIUS 3.2.3 `raddb` tree (vendored from the stock
`freeradius/freeradius-server:3.2.3` image and customized), not a set of
overlay deltas.

## How the auth path flows (contract C4)

```
MikroTik NAS / harness --Access-Request(PAP or CHAP)--> freeradius:1812
                                                            |
                                            sites-enabled/default: authorize {}
                                                            |
                                    exec module "hikrad_authorize"
                                    (mods-available/hikrad_authorize)
                                                            |
                                          scripts/authorize.pl
                                                            |
                                POST http://hikrad-api:8080/internal/radius/authorize
                                        {username,password?,chap_challenge?,
                                         chap_response?,nas_ip,
                                         calling_station_id?,service}
                                                            |
                                {action:"accept"|"reject",reason,attributes:[...]}
                                                            |
                        script prints Tmp-String-0/-1 (decision) and, on
                        accept, Mikrotik-Rate-Limit / Framed-Pool /
                        Session-Timeout — all into the **reply** list
                                                            |
                    unlang in authorize{} branches on Tmp-String-0:
                      accept -> control:Auth-Type := Accept (FreeRADIUS's
                                built-in unconditional-accept fast path —
                                pap/chap/mschap/files never get a chance to
                                independently authenticate the password)
                      reject -> explicit `reject` (Reply-Message set from
                                Tmp-String-1's reason)
```

**Why an exec script instead of `rlm_rest` directly**, as the agent-2 task
file originally sketched: two dead ends, found by testing against the real
image rather than assumed.

1. `rlm_rest`'s `connect_uri` reachability pre-check runs once at server
   *startup* and aborts the whole virtual server if it fails — a problem the
   moment `hikrad-api` isn't already healthy before `freeradius` starts
   (compose's `depends_on: condition: service_healthy` narrows this but
   doesn't eliminate races, e.g. hikrad-api restarting).
2. This build's `rlm_json` only implements `%(json_encode:...)`; it has no
   response-decode / jpath-map support, and `map json <expr> { ... }`
   parses (json is a registered map type) but fails at runtime — there is no
   built-in way to turn our C4 JSON body into RADIUS attributes without
   another module in the chain anyway.

So `hikrad_authorize` (an `exec` module, `mods-available/hikrad_authorize`)
runs `scripts/authorize.pl`, which does the HTTP POST *and* the JSON-to-
attribute mapping in one process, keeping both concerns together and
failing closed (any error — timeout, malformed JSON, non-2xx — becomes a
reject) rather than hanging. Its 2-second `timeout` is the real enforcement
of the "reject within 2s on backend-down" requirement — empirically,
`HTTP::Tiny`'s own internal timeout does not reliably fire before the exec
module's hard kill, so the exec `timeout` is what actually bounds the
worst case.

`rlm_exec`'s output-pairs parser does **not** accept `list:Attribute` prefix
syntax (confirmed empirically — it errors "Expecting operator"), so every
attribute the script prints lands in whichever single list `output_pairs`
names (`reply`, here). `Tmp-String-0`/`Tmp-String-1` are FreeRADIUS's
generic scratch attributes; they're never encoded onto the wire, so parking
them in the reply list is harmless.

Accounting is left as the stock log-only stub (`detail` + `unix` in
`sites-enabled/default`'s `accounting {}`) — forwarding to hikrad-acct's
lossless ingest pipeline is Phase 2.

## Adding a test NAS IP

`clients.conf` ships a broad `docker_bridge_dev` entry
(`172.16.0.0/12`, secret `testing123`) covering the private ranges Docker's
default and Compose-generated bridge networks allocate from — this is
already enough for `backend/test/harness/` and CI regardless of which
subnet Docker picks. It is deliberately permissive (dev/CI only, secret is
the FreeRADIUS default) and superseded entirely by Phase 2's DB-driven,
per-subscriber-NAS clients (FR-17).

For a NAS reachable from outside Docker's own bridge ranges (e.g. testing
against a real MikroTik on the LAN), `clients.conf` also
`$INCLUDE`s `clients.local.conf` (an empty no-op by default). Copy
`clients.local.conf.example` over it and set `TEST_NAS_IP`; note the env var
must also reach the container, which requires adding
`environment: ["TEST_NAS_IP=${TEST_NAS_IP}"]` to compose.yml's `freeradius`
service (Agent 1's file) — not needed for the harness/CI path above.

(A plain `$ENV{TEST_NAS_IP}` inline in `clients.conf` was tried first and
rejected: an unset/empty env var in a client's `ipaddr` fails config
parsing and refuses to start the *entire* server, not just that client.)

## Running the harness

```sh
make -C backend test-harness-smoke   # against a running `docker compose up` stack
```

See `backend/test/harness/README.md` (Agent 2) for the harness's own flags
(`-rate`, `-duration` for the NFR-1 perf-gate mode arriving in Phase 5) and
what it asserts: PAP and CHAP Access-Accept with `Mikrotik-Rate-Limit =
"10M/10M"` for the seeded `testuser`/`testpass`, Access-Reject with a
`Reply-Message` reason for a wrong password or unknown user.

## A note for anyone testing this on native Windows (not WSL2)

FreeRADIUS refuses to start if `/etc/raddb` (or anything under it) is
group/world-writable ("insecure configuration"). Docker Desktop's bind-mount
translation for a raw Windows path (e.g. `F:\...`) commonly presents files
as permissive regardless of the source ACLs, which trips this check —
`chmod` from inside a container does not fix it, because the underlying
translation, not a real Unix mode bit, is what's being read. This is exactly
why `docs/ops/dev-setup.md` says to use WSL2 on Windows: cloning the repo
into the WSL2 distro's own filesystem (not a `/mnt/<drive>` passthrough of a
Windows path) gives the bind mount a real Linux filesystem underneath, and
the problem does not occur. It also does not occur in CI (`ubuntu-latest`)
or on a Linux/macOS dev machine.
