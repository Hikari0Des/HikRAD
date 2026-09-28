# HikRAD FreeRADIUS config

This directory is a complete FreeRADIUS 3.2.3 `raddb` tree (vendored from
the stock `freeradius/freeradius-server:3.2.3` image and customized). v4-2's
native installer (`scripts/install.sh`) copies it wholesale to
`/etc/hikrad/freeradius` on every install/upgrade, patches three paths
(`raddbdir`, `logdir`, `run_dir`) to point inside that tree instead of the
distro package's own `/etc/freeradius`, validates it (`freeradius -CX`)
*before* enabling `hikrad-radius-worker.service`, and never runs the
distro's own `freeradius.service` (disabled at install time — the two would
otherwise fight over UDP 1812/1813/3799).

## How the auth path flows (contract C4)

```
MikroTik NAS / harness --Access-Request(PAP or CHAP)--> hikrad-radius-worker:1812
                                                            |
                                            sites-enabled/default: authorize {}
                                                            |
                                    exec module "hikrad_authorize"
                                    (mods-available/hikrad_authorize)
                                                            |
                                          scripts/authorize.pl
                                                            |
                                POST http://127.0.0.1:8080/internal/radius/authorize
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

**Why an exec script instead of `rlm_rest` directly** — two dead ends,
found by testing against the real image rather than assumed.

1. `rlm_rest`'s `connect_uri` reachability pre-check runs once at server
   *startup* and aborts the whole virtual server if it fails — a problem if
   `hikrad-api` isn't already up when `hikrad-radius-worker` starts
   (`After=hikrad-api.service` in the systemd unit narrows this but doesn't
   eliminate every race, e.g. hikrad-api restarting mid-update).
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

### Service discrimination (FR-58)

`scripts/authorize.pl` sends the real `service` field: `hotspot` when the
Access-Request carries `Service-Type = Login-User` (how MikroTik Hotspot logins
present), `pppoe` otherwise. The backend accepts a Hotspot login for a PPPoE
subscriber only when that subscriber opted in.

The accept-path attribute mapping has two intents beyond rate-limit/pool:
`static_ip` → `Framed-IP-Address` (precedence over `Framed-Pool`, FR-16.2) and
`redirect_expired` → `Mikrotik-Address-List` (walled-garden, FR-9). The backend
never emits both `static_ip` and `address_pool` for one accept.

### Accounting forward (contract C6)

`accounting {}` in `sites-enabled/default` calls `hikrad_accounting`
(`mods-enabled/hikrad_accounting` → `scripts/accounting.pl`) which POSTs each
record to `hikrad-acct` (`$HIKRAD_ACCT_URL`, default
`http://127.0.0.1:8082/acct`) in the C6 JSON shape. It **fails closed**: any
error exits non-zero, the section `reject`s, no Accounting-Response is sent, and
the NAS retransmits — this is the never-lose-a-packet guarantee (M2). Only a 2xx
(hikrad-acct confirmed a durable enqueue) lets FreeRADIUS ack.

### DB-driven clients: the decision (FR-13.2)

The open question was **rlm_sql dynamic clients vs. config-regeneration +
reload**. We chose an exec-script bridge for the same reason as the
authorize path above (NFR-4 requires NAS secrets encrypted at rest;
`rlm_sql`'s `read_clients` needs the secret in cleartext in a column
FreeRADIUS can read — that would defeat the encryption):

- `hikrad-api` owns the source of truth (the encrypted `nas` table) and, on
  every NAS create/update/delete, regenerates a plain `client { … }` file
  (decrypting each secret only in-process) to `HIKRAD_RADIUS_CLIENTS_PATH`,
  which points at `clients-generated.conf` (this tree `$INCLUDE`s it from
  `clients.conf`; it ships empty so the server starts before the first
  regeneration). **A NAS must be in FreeRADIUS's *transport-layer* client
  list to work at all** — FreeRADIUS validates the shared-secret /
  Message-Authenticator against the matching `client {}` before it runs any
  module; a packet from an IP it has no client for is silently dropped
  ("Ignoring request … from unknown client").

- **FreeRADIUS 3.x reads clients.conf only at startup — neither SIGHUP nor a
  `radmin` "reload" re-reads clients or listen sections** (confirmed live
  against 3.2.3). Before v4-2, the only fix was a supervisor that restarted
  the whole process on every NAS change (a sub-second window, tolerable
  under Docker where FreeRADIUS was disposable, but not the "zero restart"
  bar v4-2 sets, gate item 9).

- **v4-2's actual fix (FR-118): FreeRADIUS's own dynamic-clients mechanism.**
  `clients.conf`'s `hikrad_dynamic` entry (`ipaddr = 0.0.0.0/0`, a
  placeholder secret that authenticates nothing on its own) is FreeRADIUS's
  documented "discover a client I don't have yet" range: the **first**
  packet from any source IP not already covered by a more specific client
  triggers `sites-available/dynamic-clients`, which runs
  `hikrad_dynamic_client` (`mods-available/hikrad_dynamic_client` →
  `scripts/dynamic-client.pl`) — the same exec-script pattern as authorize,
  looking the real source IP (`Packet-Src-IP-Address`, not the
  attacker-controlled `NAS-IP-Address`) up against the `nas` table via
  `POST /internal/radius/dynamic-client`. A match fills in
  `control:FreeRADIUS-Client-*` attributes, which FreeRADIUS's built-in
  `dynamic_clients` policy (`mods-enabled/dynamic_clients`) turns into a
  real, cached client (`lifetime = 3600`, so a disabled/deleted NAS's
  trust expires within the hour rather than lingering until restart). A
  miss — the IP is not an enabled NAS — is rejected exactly as an unknown
  client always was; nothing is ever created for it. **No reload, no
  restart, ever**, for a NAS the operator adds through the panel.

- `clients-generated.conf` and its regeneration/reload call
  (`internal/radius/clients.go`'s `reloadFreeRADIUS`, a best-effort
  control-socket "reload") are kept alongside the new mechanism, unchanged,
  as a harmless belt-and-suspenders boot-time list — not load-bearing for
  the zero-restart guarantee anymore.

CoA/Disconnect is sent by `hikrad-api` *directly* to each NAS's `coa_port`
(layeh radius over UDP), not through FreeRADIUS — so no `originate-coa` / CoA
listener config is needed here. The RouterOS side accepts it via the
`/radius incoming set accept=yes` line the FR-14 config snippet emits.

## Adding a test NAS IP

`clients.conf`'s `client localhost` entry (127.0.0.1, secret `testing123`)
already covers `backend/test/harness/` and CI's harness-smoke job: v4-2 runs
`hikrad-radius-worker` as a native process on the same host as everything
else, so a harness run from that host is a genuine loopback connection —
no bridge-network client range is needed the way it was under Docker.

For a NAS reachable from somewhere other than localhost (e.g. testing
against a real MikroTik on the LAN), `clients.conf` also `$INCLUDE`s
`clients.local.conf` (an empty no-op by default). Copy
`clients.local.conf.example` over it and set `TEST_NAS_IP` before starting
`hikrad-radius-worker`.

(A plain `$ENV{TEST_NAS_IP}` inline in `clients.conf` was tried first and
rejected: an unset/empty env var in a client's `ipaddr` fails config
parsing and refuses to start the *entire* server, not just that client.)

## Running the harness

```sh
sudo systemctl status hikrad-radius-worker   # confirm it's up
cd backend && go run ./test/harness -addr 127.0.0.1:1812 -secret testing123
```

See `backend/test/harness/README.md` for the harness's own flags
(`-rate`, `-duration` for the NFR-1 perf-gate mode) and what it asserts: PAP
and CHAP Access-Accept with `Mikrotik-Rate-Limit = "10M/10M"` for the seeded
`testuser`/`testpass`, Access-Reject with a `Reply-Message` reason for a
wrong password or unknown user.

## A note for anyone testing this from a Windows-mounted checkout

FreeRADIUS refuses to start if `/etc/hikrad/freeradius` (or anything under
it) is group/world-writable ("insecure configuration"). This can trip if
you point `freeradius -d` directly at a `/mnt/<drive>/...` WSL2 passthrough
of a Windows-backed path — Windows bind mounts commonly present files as
permissive regardless of their real ACLs, which the check reads as
insecure; `chmod` from inside WSL does not fix it, because the underlying
translation, not a real Unix mode bit, is what's being read. `scripts/
install.sh` never hits this in production (it always copies this tree into
`/etc/hikrad/freeradius` on the target Linux filesystem first); it only
matters if you're running `freeradius` directly against an unconverted
checkout path for a quick manual test — clone into the WSL2 distro's own
filesystem (e.g. `~/hikrad`) instead, or run `scripts/install.sh` and let
it stage the copy for you.
