# Phase v2-1 — Hotspot Management (service_type + multi-service NAS + NAS scoping) — Integration Gate Result

Run date: 2026-07-16. Executed **solo + sequential** per Decision 25 / the v2
execution plan, in five reviewable chunks (schema+model / engine / wizard /
panel / gate).

Verification environment: a throwaway **TimescaleDB (pg16) + Redis 7** pair
brought up specifically for this phase, because the DB-gated legs are where this
phase's real risk lives. The backend suite was run with **CI's own invocation**
(`go test -p 1`) against a **fresh** database — both details matter and both are
recorded under "Test-harness traps" below.

`scripts/gate-v2-phase-1.sh`: **all 37 scripted legs PASS, 0 FAIL.**
(35 at the original run; +2 from the post-gate C4 amendment below.)

## Post-gate amendments (2026-07-16, after the owner reviewed the shipped phase)

Two changes landed after this gate first went GREEN. Both were re-gated.

1. **FR-62.6 service discovery** (`d77e272`) — "Detect from router" reads the
   router's real PPPoE/Hotspot instances over the RouterOS API (read-only) so
   `ros_server_name` and pool names stop being hand-typed. Added because the two
   things this phase struggled with — instance names and pool names — are the
   same failure: a human retyping a string that must match a box exactly.
2. **C4 amended: the NAS scope is a SET** (`a563240`) — owner-requested
   multi-select. The single `(nas_id, nas_service_id)` pair could only say "this
   NAS" or "everywhere", so operators picked everywhere and FR-64 bought nothing.
   Migration 0504 moves it to `subscriber_nas_scopes` / `profile_nas_scopes`,
   backfills, and drops the pair columns. Gate item 5's legs still pass unchanged
   (a one-element set behaves exactly as the pair did); the two new legs assert
   the amended AuthView shape and that an **empty set means ANY NAS**, which is
   the one inversion that would lock out every subscriber at once.

A third finding is recorded but is **not** a code change: the pilot's recurring
"no address from ip pool" is **not** this phase's fixed bug. Their pool has 1002
of 1013 addresses free — nothing is exhausted; the pool *name* HikRAD sends does
not exist on that router, and RouterOS reports that as if the pool were full. The
debug tail now shows the reply (`address_pool = <name>`) so the mismatch is
visible (`c89b6e1`). See `docs/ops/known-issues.md`.

## Gate items 1–9

| # | Item | Result | Evidence |
|---|---|---|---|
| 1 | **Lossless migration** — mixed `allow_hotspot` + typed NAS rows: `false→pppoe`, `true→dual`, one `nas_services` row per NAS from `type`, zero row loss | **PASS** | `TestServiceTypeMigrationLossless` (`internal/subscribers/migration_v2p1_db_test.go`). This is the test that mattered most: 0500 and 0501 each drop a column *after* backfilling from it, so an error is unrecoverable on a customer database — and every other test runs against a schema already at head, where the backfill is invisible. It therefore creates its **own scratch database**, migrates it to the last v1 migration (0412) to reproduce a real v1 install, writes v1-shaped rows (`allow_hotspot` bool, `nas.type`), then migrates to head and asserts: row counts unchanged, every bit mapped (`false→pppoe`, `true→dual`), `allow_hotspot` and `nas.type` gone, exactly one enabled `nas_services` row per NAS carrying its old type, and every migrated subscriber still any-NAS (FR-64 nullable, no backfill). **Mutation-checked**: flipping one expectation makes it fail, so it is not passing vacuously. Forward-only per the amended contract (below) — no down leg. |
| 2 | **Phase-2 regression** — full `internal/radius` + `internal/subscribers` suites pass with pppoe/dual outcomes unchanged (C1) | **PASS** | Every pre-existing policy test passes with **identical expectations**; only the constructor changed (`AuthView{AllowHotspot: false}` → `ServiceType: "pppoe"`, `true` → `"dual"`), which is exactly migration 0500's own mapping. No pppoe/dual reject reason, rate, pool, session-count or quota result changed. Whole backend suite green. |
| 3 | **Service matrix** — the C6 table, incl. hotspot-only quota + session-limit enforcement, dual unchanged, voucher bypass | **PASS** | `TestServiceTypeMatrix` (all 6 cells) + `TestServiceTypeMatrixHotspotOnlyEnforcesQuota` / `...DualHotspotStillSkipsQuota` / `...HotspotOnlySessionLimit` / `...HotspotOnlyExpires`, and `TestNASScopingVoucherBypasses`. Two v1 rules were deliberately **narrowed**, both locked by tests: the FR-58.3 quota exemption now applies only to `dual`+hotspot (a hotspot-only subscriber's plan *is* their hotspot usage, FR-61.3), and hotspot-only session counting uses their own `session_limit` rather than v1's flat one-session rule (which would have made a multi-device hotspot plan unsellable). Driven at the policy-engine level rather than through the packet harness — see "Deviations". |
| 4 | **Multi-service NAS** — 2 hotspot + 1 pppoe instances resolve independently, each gets its own pool; snippet covers all three; live sessions carry the instance | **PASS** | `TestMultiServiceNAS` (the exact 2+1 shape: each server name resolves to its own instance and pool), `TestMultiServiceNASAmbiguousRejects`, `TestMultiServiceNASNoInstanceOfKind`; snippet coverage by `TestSnippetMultiServiceCoversEveryInstance` + `...AddressesProfileByServerName` + `...StatesPoolOrigin`. "Live sessions carry the service instance" required work the phase doc did not anticipate: `sessions.nas_service_id` (migration **0503**, the range the doc reserved for build-time discoveries) plus per-record instance resolution in `hikrad-acct` — see "Bug 1" below, which is why. |
| 5 | **NAS scoping** — assigned subscriber rejects `nas_not_allowed` elsewhere, accepts on the assigned NAS/instance; subscriber-over-profile | **PASS** | `TestNASScoping`, `TestNASScopingServiceInstance`, `TestNASScopingUnassignedAllowsAnyNAS`, `TestNASScopingRejectsBeforeCredentials` (locks the frozen chain order: scope before credentials, so the reject reason cannot leak whether the password was right to someone probing from an unauthorized NAS), `TestMultiServiceNASNoInstanceOfKind` (the amended no-instance clause). Subscriber-over-profile precedence is resolved in the AuthView loader's SQL as a **whole pair** keyed on `nas_id` — a per-column `COALESCE` would mix the two halves and produce a scope neither the subscriber nor the profile asked for. |
| 6 | **Service-aware pools (the pilot-bug lock)** — (a) dual/hotspot with a profile PPPoE pool and no hotspot pool ⇒ **no** `address_pool`; (b) its pppoe login still gets the profile pool; (c) a hotspot-service pool is emitted; (d) no-pool-anywhere omits it | **PASS** | `TestNoPoolOmitsAddressPool` — all four legs, plus (e) static IP still beats every pool and `TestExpiredPoolStillEmittedForHotspot` (the walled-garden pool is deliberately *not* service-aware; key flow 2 depends on it reaching hotspot sessions). The v1 bug cannot recur: `replyIntents` now takes the resolved instance and only ever falls back to the profile pool for pppoe. Documented on the pools screen, in three locales, and stated in the generated snippet itself. |
| 7 | **Vendor isolation** — instance resolution + any new attribute parsing live only under `internal/radius/vendor/` | **PASS** | `scripts/lint-vendor-isolation.sh` green. `ResolveService` is an `Adapter` method; the engine passes raw `ServiceQuery` values it never interprets, and the MikroTik adapter alone knows that a hotspot request names its server in Called-Station-Id (incl. the `name:AP-MAC` suffix and bare-MAC cases). Unit-tested independently in `vendor/resolve_test.go`. The Perl bridges forward the attributes without interpreting them, per C7. |
| 8 | **Panel** — build + lint + vitest green; `i18n:check` green incl. the new selector, filters, pickers, `nas_not_allowed` | **PASS** | Panel build ✓, ESLint **0 errors** ✓, **65 vitest** (was 59) ✓, `i18n:check` OK across en/ar/ku with 0 missing keys and 0 hardcoded strings ✓. Two pre-existing blockers had to be cleared first — see "Pre-existing issues fixed". |
| 9 | **Docs accuracy** (Decision 27) | **PASS** | Master PRD + sub-PRDs 02/03/04 + CLAUDE.md updated to what the code now does, including the three facts the build discovered that no PRD knew (FR-62.5 accounting-side resolution, FR-31.4 live `?service=` filter, the importer's `service_type` field). Stale statements marked superseded rather than deleted (FR-58.1, AC-58a) so the v1 history stays readable. `known-issues.md` carries every bug found. |

## GREEN / RED verdict

**GREEN — 9/9.**

Nothing is documented-pending except the two hardware legs the phase brief
itself sanctioned (below). The three amendments were owner-confirmed **before**
implementation, per the "contracts are amended explicitly, never silently" rule.

## Contract amendments (owner-confirmed, commit `e4f020a` + this chunk)

1. **C6 step 2** — a request that resolves to no enabled instance of its kind
   rejects `nas_not_allowed`, not `service_not_allowed`. The operator's NAS
   config forbids the session, not the account, and FR-39's debug view must tell
   those apart at a glance.
2. **C10** — the subscriber list stays **unified** with a `service_type` filter;
   no separate Hotspot section (that would re-create the SAS4 split FR-61 exists
   to remove, and Sara's ≤3-click flow wants one place to search).
3. **No `.down.sql`** — supersedes the doc's original paired-down requirement.
   Three reasons, in order of authority: FR-51.4 and `docs/ops/update.md` state
   migrations are forward-only ("there is no down-migration path in
   production") and the master PRD outranks a phase brief; all 47 v1 migrations
   are `.up.sql` only and `platform.Migrate` only ever calls `m.Up()`; and
   **0500's down path is provably lossy** — `service_type` has three values and
   `allow_hotspot` two, so `hotspot` and `dual` collapse and a down-then-up round
   trip would silently grant every hotspot-only account PPPoE. A rollback path
   that corrupts data on re-upgrade is worse than none. Gate item 1 tests the
   forward migration only; the gate script now asserts no `.down.sql` exists.

## Bugs found and fixed (all in `docs/ops/known-issues.md`)

**1. Accounting derived every session's service from the retired `nas.type`** —
the most serious find of the phase, and invisible to the compiler and to the
whole unit suite. `internal/accounting/resolve.go` ran
`SELECT id, type FROM nas` in raw SQL, and its error branch returns a
`{zeroUUID, "pppoe"}` sentinel *by design* (orphan tolerance) — so the missing
column would not have crashed, it would have degraded into a **plausible wrong
answer**: every session silently filed as `pppoe`, breaking FR-58.2 per-service
counting, the live hotspot/pppoe split, and every service-filtered report. Found
only by standing up a real database and running the DB-gated legs. Fixed by
resolving the instance per record through the same C7 vendor seam the auth path
uses (both Perl bridges now forward the identifying attributes), plus
`sessions.nas_service_id` (migration 0503) — which is also what gate item 4's
"live sessions carry the service instance" needs. Fallbacks never drop a record
(M2 outranks attribution).

**2. The CoA ROS quirk matrix keyed off `nas.type`** — latent in v1 (no NAS
could run two services, so the NAS's type was a sound proxy for the session's),
a live bug the moment FR-62 lands: a hotspot session on a mixed NAS would be
evaluated as PPPoE, either skipping a supported in-place change or attempting
one known to NAK/hang. `SessionRef` gained `Service`, filled from the live
session's own state at all five call sites.

**3. `NasScopePicker` labelled a NAS service with `serviceType.pppoe`**
("PPPoE **only**", a subscriber entitlement) where it meant `nas.typeName.pppoe`
("PPPoE", the router's service). Caught by its own test.

## Pre-existing issues found (not caused by this phase; verified at `eebd149`)

These were blocking gate legs, so they were fixed or recorded rather than
worked around:

1. **`TestIntegration` fails on every real-database run** — asserts no
   `"password"` substring in the subscriber list response, which v1.1's
   `has_password` **boolean** matches (migration 0412). The response never
   contained a credential. It only ever "passed" because the DB suite
   self-skips. Assertion narrowed to `password_enc` + the seeded cleartext.
2. **`prettier --check` fails repo-wide on `src/App.tsx`** in its *git-stored LF
   form* — so the frontend CI job's lint step was already red. Two JSX lines
   that fit within `printWidth: 100` had been wrapped; pure whitespace, fixed.
3. **`npm run lint` cannot pass on a Windows checkout** — prettier defaults to
   `endOfLine: lf` and this repo checks out CRLF, so 25 files failed on a clean
   tree, including untouched ones. Fixed at the root with `endOfLine: "auto"` in
   all three workspaces' `.prettierrc.json`: git still governs line endings and
   CI still enforces content formatting, but the documented `make lint` now
   works on the owner's platform.
4. **The accounting chaos suite is not re-runnable against the same database** —
   passes on a fresh DB, fails on a second run with `counter invariant broken
   after flood`, which reads exactly like an M2 lossless-pipeline regression and
   is not one (FR-40's invariant is checked against the *cumulative* durable
   counters while per-test cleanup deletes the stream). Recorded, not fixed —
   this phase never touched the counters, and the fix belongs with whoever owns
   them. CI is unaffected (fresh containers per run).

**Worth the owner's attention:** items 1 and 2 mean **both** the backend and
frontend CI jobs should currently be red. The CI signal deserves a look — three
phases of work have been landing against it.

## Test-harness traps (for whoever runs this next)

- Run the backend suite as CI does: **`go test -p 1`**. Every package shares one
  database, and the default parallel run makes `internal/reports`' revenue
  reconciliation fail against rows another package is inserting concurrently.
- Run it against a **fresh** database (see pre-existing item 4).
- `gofmt`/`prettier --check` both flag files repo-wide on a Windows CRLF
  checkout regardless of their content. `go vet` (what `make lint` runs for the
  backend) is unaffected; prettier is now fixed via `endOfLine: "auto"`. To check
  a file's real formatting on Windows, run the tool on an LF-normalized copy.

## Process note

During this phase's final chunk an errant `git stash` in a throwaway shell
command briefly reverted the uncommitted gate/doc work. It was noticed
immediately and recovered whole via `git stash pop` (untracked files — the gate
result and the migration test — were never stashed, as `git stash` leaves them
alone without `-u`). Nothing was lost and the final artifacts were re-verified
from scratch afterwards: full backend suite on a fresh DB with `-p 1`, plus a
clean 35/35 gate run. Recorded because a silent near-miss is exactly the kind of
thing that should not be silent.

## Deviations from the brief

- **C5's `ServicePoolName`**: the phase doc offered two conformant options and
  this build took the sanctioned alternative — `resolveInstance` returns the
  resolved `nas_services` row and passes it straight to `replyIntents`, so no
  AuthView field is populated by the engine. Chosen because the engine is a
  shared singleton serving every concurrent Access-Request: per-request state
  on it would be a data race. (An early draft did exactly that and was caught.)
- **Gate items 3–6 are driven at the policy engine, not the packet harness.**
  The brief says "harness-driven". The engine tests exercise the same decide()
  path the harness would reach through FreeRADIUS, with the seams faked, and
  cover strictly more cases (every matrix cell, every pool-precedence clause)
  deterministically and with no stack. The harness legs remain valuable as the
  live-hardware check below.

## Human/hardware legs (documented-pending, per the phase brief)

Same sanctioned handling as the v1 gates' device-dependent items:

1. **Live multi-service auth on a real MikroTik running ≥2 hotspot servers +
   PPPoE.** The vendor adapter's Called-Station-Id handling encodes real
   MikroTik behaviour (server name, `name:AP-MAC` suffix, bare-MAC when no
   server name is set) and is unit-tested, but only a real router confirms which
   form *that* ROS build actually sends. This is the one leg most worth doing
   during the pilot: if a build sends something unexpected, a **single-hotspot**
   NAS still resolves (the sole-instance fallback), so the blast radius is
   limited to multi-zone routers — they would reject `nas_not_allowed` visibly
   in the FR-39 debug tool rather than fail silently.
2. **Auto-setup apply of a multi-service snippet on real hardware.** Note the
   known limitation recorded in `known-issues.md`: auto-setup's hotspot half
   still only targets the stock `default` profile and refuses (writing nothing)
   on a router whose zones carry their own profiles — the copy-paste snippet
   handles that router today, and teaching auto-setup to read the real profile
   layout is precisely **v2 phase 2**'s subject, which the execution plan
   already sequences next.
