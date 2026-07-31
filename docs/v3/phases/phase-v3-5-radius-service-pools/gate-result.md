# Phase v3-5 — RADIUS Service Config · Router-Field Dropdowns · Address-Lists · IP-Pool Ownership — Gate Result

**Verdict: GREEN.** 6/6 scripted gate items PASS; 2 legs (a real-router pool push/adopt round trip, a live address-list login) are documented-pending — no MikroTik hardware in-session, both covered by fake-`ROSConn` and Go authorize unit tests.

Date: 2026-07-23 · FR-108–110 (PRD Decision 51) · sub-PRD 02 (+ 04 for the FR-109 data field) · **migration 0593** (repo max 0592 → 0593).

## What shipped

| Contract | Summary | Key files |
|---|---|---|
| C1 | `address_list` reply intent — added to the vendor vocabulary + radius mirror, mapped to `Mikrotik-Address-List` in the adapter and `authorize.pl`, emitted only in the normal/throttled tail | `vendor/vendor.go`, `radius/intents.go`, `vendor/mikrotik.go`, `radius/policy.go`, `deploy/freeradius/scripts/authorize.pl` |
| C2 | Address-list data — `profiles.address_list` + `subscribers.address_list_override`, resolved subscriber-over-profile into `AuthView.AddressList`; panel fields on both forms | migration 0593, `profiles/store.go`+`api.go`, `subscribers/store.go`+`api.go`+`authview.go`, `radius/authview.go`, `ProfilesPage.tsx`, `SubscriberFormModal.tsx` |
| C3 | Pool ownership — `ip_pools.management_mode` (+`router_ranges`), edit/delete/push refused for adopted pools, `/ip pool` push planner, hash-gated push plan/apply, adopt | migration 0593, `pools_store.go`, `pools_api.go`, `pools_provision_api.go`, `vendor/mikrotik_pool.go`, `module.go`, `PoolsPage.tsx`, `api/pools.ts` |
| C4 | Service-form router fields — discovery-fed suggestions with free-text fallback; modal converted to the FR-94 control set | `ServiceProvisionModal.tsx` |

## Gate items

1. **C1 intent — PASS.** `TestIntentConstantsInSync` extended with the new pair (the FR-17 vendor-boundary guard). `TestMikrotikApplyAddressList` asserts it encodes `Mikrotik-Address-List`; `TestMikrotikApplyRedirectExpiredStillMapsAddressList` proves the pre-existing redirect mapping is unchanged. `authorize.pl` gained the matching `elsif`.
2. **C1 emission + collision guard — PASS.** `TestAddressListEmittedOnNormalLogin` (emitted when set), `TestAddressListOmittedWhenUnset` (absent when empty), and — the important one — `TestAddressListNotEmittedForExpiredPool`: an expired session still emits only the walled-garden redirect, never both, since `modeExpiredPool` returns before the shared tail. The expired-pool reply is byte-unchanged (`TestExpiredPoolAccept` still green).
3. **C2 data — PASS.** Column added in 0593 and threaded through both stores/APIs; `GetAuthView` resolves `COALESCE`-style subscriber-override-else-profile. `address_list` is a plan attribute (like rate), so it follows the existing `apply=now` (default) invalidation — **no** change to the invalidation predicate was needed, and `apply=next_renewal` defers it exactly as it defers a rate change. Panel/TS/i18n threaded trilingually.
4. **C3 pool ownership — PASS.** `PlanPool` unit-tested against the in-memory fake router for all three outcomes — add when absent, set-by-`.id` when ranges differ (asserting `CurrentState` shows the router's value), and a genuine **no-op plan** when already in sync — plus input validation and `ReadPools` skipping unnamed rows. Both planner and reader assert **zero writes** during planning. Push is refused for a `router`-mode pool (`pool_router_managed`, 409) and for a pool with no ranges; apply re-plans and refuses on `preview_stale`; adopt inserts `router` rows, never writes to the router, and **never converts a HikRAD-owned pool** (the `ON CONFLICT … WHERE management_mode='router'` guard).
5. **C4 service form — PASS.** `ros_server_name`/`interface` now offer the router's real values (scoped to the service kind) via discovery-fed suggestions while remaining free text for the create case; the router's own pool names are surfaced next to the pool picker (the "named here, missing on the router" failure). The modal's hand-rolled raw `<select>`/`<input>` were converted to the FR-94 `Field`/`Select`/`TextInput` set; `no-native-controls (panel): clean`.
6. **No regressions — PASS.** Backend `go build` + `go vet` clean; full panel suite **34 files / 115 tests green**; `tsc --noEmit` clean; `npm run i18n:check` **OK (1550 keys × 3 locales)**; prettier + stylelint + no-native-controls clean.

## Design decisions worth recording

- **Existing pools default to `system`, not `router`.** Defaulting to `router` (the `nas_services` default) would have silently made every pool on every existing install read-only — a behavior regression. HikRAD-defined named pools are HikRAD-owned by nature; only the new adopt path creates `router` rows.
- **`router_ranges` is a separate text column.** RouterOS expresses pool ranges as `10.0.0.2-10.0.0.254`, which is **not** valid `inet` and cannot go in `ip_pools.ranges`. Rather than lossily converting a pool HikRAD doesn't own, an adopted pool stores `ranges = '{}'` plus the router's string verbatim for display. Utilization math stays meaningful only for owned pools, which is the honest answer.
- **`address_list` deliberately shares the `Mikrotik-Address-List` VSA with `redirect_expired`.** They are emitted in mutually-exclusive outcome modes, so one accept never carries both; item 2's test is the standing guard.

## Documented-pending (no hardware in-session)

- Real-router **pool push/adopt** round trip (planner + reader are unit-tested against the fake `ROSConn`; the apply path reuses the already-shipped `ApplyAutoSetup` executor).
- A **live address-list login** against a MikroTik (the Go authorize tests cover intent emission; `authorize.pl`'s branch is a one-line mirror of the adjacent, field-proven `redirect_expired` branch).
- Pre-existing, unrelated: `cmd/hikrad-updaterd`'s two rollback tests still fail (confirmed against a clean tree during v3-3; logged in `docs/ops/known-issues.md`). Not a v3-5 regression — that package depends on nothing this phase touched.
