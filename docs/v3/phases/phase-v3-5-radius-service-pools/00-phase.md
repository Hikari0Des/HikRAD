# Phase v3-5 — RADIUS Service Config · Router-Field Dropdowns · Address-Lists · IP-Pool Ownership

> Kicked off 2026-07-22 (PRD Decision 51). SOLO, sequential. Owned by **sub-PRD 02** (RADIUS/NAS) with the FR-109 profile/subscriber data field in **sub-PRD 04** (split FR-64-style). Migration **0593**. Amends the **frozen RADIUS reply contract** (FR-109) — done explicitly here, per the standing rule. Governed by the **router-ownership sync principle** (Decision 48).

## 1. Problem (grounded)

- **#1 address-list** — abstract reply intents are `rate_limit`/`address_pool`/`session_timeout`/`redirect_expired`/`static_ip` (`vendor.go`); an address-list is emitted **only** for the expired redirect (`policy.go` `modeExpiredPool`). No way to put a normal login into a named address-list.
- **#12 IP pools** — `ip_pools` has no ownership concept; HikRAD only names a pool (allocation is the router's), with **no sync** and no way to push a HikRAD pool's ranges onto a router (the "named here, missing on the router" login failure). Decision 48 resolves the model: owned pools editable+pushable, adopted pools read-only.
- **#10/#11 service form** — `ServiceProvisionModal`'s `ros_server_name`/`interface` are blind free-text `<input>`s though `discover-services` already returns the router's real values.

## 2. Scope

FR-108 + FR-109 + FR-110. No online dependency. **No router write without an explicit hash-gated apply** (Decision 17 intact). Out of scope: multi-router pool orchestration beyond a per-NAS push; non-MikroTik vendors (the intent stays vendor-neutral, only the MikroTik mapping ships).

## Migration budget
**0593** only (`profiles.address_list`, `subscribers.address_list_override`, `ip_pools.management_mode` default `system`). Repo max becomes 0593.

## Frozen contracts

### C1. `address_list` reply intent (FR-109) — RADIUS contract amendment
- Add `IntentAddressList = "address_list"` to `internal/radius/vendor/vendor.go` **and** the mirror `IntentAddressList Intent = vendor.IntentAddressList` to `internal/radius/intents.go` (both covered by `TestIntentConstantsInSync`).
- `internal/radius/vendor/mikrotik.go` `Apply` (CoA path): add `case IntentAddressList` → `mikrotik.MikrotikAddressList_SetString(p, a.Value)` — identical VSA to `redirect_expired`. The two are emitted in mutually-exclusive outcome modes, so they never both fire in one accept.
- `internal/radius/policy.go` `replyIntents`: emit `add(IntentAddressList, view.AddressList)` in the **shared normal/throttled tail** (after the address block, before `return`), which `modeExpiredPool` returns before reaching — so an expired session never emits it (no VSA collision). Empty value ⇒ omitted (the `add` closure skips empties).
- `deploy/freeradius/scripts/authorize.pl`: add `elsif ($intent eq 'address_list') { print qq{Mikrotik-Address-List := "$value"\n}; }` in the accept loop.
- `internal/radius/authview.go`: new `AddressList string \`json:"address_list"\`` field (empty = emit nothing), documented as a backward-compatible C4 extension like `StaticIP`.

### C2. Address-list data field (FR-109) — sub-PRD 04
- `profiles.address_list` (nullable text) threaded through `internal/profiles` `store.go` (struct/`profileColumns`/`scanProfile`/`insert`/`update`) + `api.go` `profileInput`; and `subscribers.address_list_override` (nullable text, subscriber-over-profile) through `internal/subscribers` `store.go` + `api.go` (INSERT + CASE-guarded UPDATE), exactly mirroring `throttle_rate` / `rate_override`.
- `subscribers/authview.go` `GetAuthView`: `COALESCE(NULLIF(s.address_list_override,''), p.address_list)` → `v.AddressList` (override-wins-else-profile-else-empty).
- **Cache invalidation**: the profile-edit path must `InvalidatePolicyByProfile` when `address_list` changes (extend the change predicate), and the subscriber-edit path already calls `InvalidatePolicy` on every write.
- Panel: `profiles.address_list` field on the profile form; `subscriber.address_list_override` on the subscriber override section (plain `TextInput`); trilingual keys. TS `Profile`/`ProfileWrite`/`Subscriber`/`SubscriberWrite` gain the field.

### C3. IP-pool ownership + router push (FR-108)
- **Model**: `ip_pools.management_mode` (`system`|`router`, default `system`). `pools_store.go` threads it through `poolRow`/`poolView`/`poolColumns`/`scanPool`; `insertPool` writes `system`. A new `adoptPoolFromRouter` insert path writes `router`.
- **Read-only for adopted**: `updatePool`/`deletePool` handlers refuse a `router`-mode pool (409 `pool_router_managed`, message pointing at "this pool is owned by the router"). Ranges/purpose/name of an adopted pool are never written.
- **Push planner (vendor-neutral)**: add `PlanPool(conn ROSConn, in PoolPushInput) (AutoSetupPlan, error)` to the `Adapter` interface + MikroTik impl mirroring `planHotspotServer`: `conn.Read("/ip/pool/print")`, match by name; if absent emit `{Action:"add", Sentence:["/ip/pool/add","=name=…","=ranges=…"]}`, if present-and-differing emit `{Action:"set", Sentence:["/ip/pool/set","=.id=…","=ranges=…"]}`, if identical no item. Apply reuses `ApplyAutoSetup`.
- **Push endpoint**: `POST /api/v1/nas/{nasId}/pools/{poolId}/push/plan` + `/apply` (permission `pools.edit`, hash-gated like FR-66/67), pushing a **system**-owned pool's ranges to that NAS's router via `connectNAS`. Refuses a `router`-mode pool. No write on plan; write only on a hash-matched apply. Audited.
- **Adopt endpoint**: `POST /api/v1/nas/{nasId}/pools/adopt` reads the router's `/ip/pool/print` and inserts any not-already-known pool as a `router`-mode `ip_pools` row (name + ranges), read-only thereafter; confirm-gated, writes nothing to the router.
- **Panel**: Pools page shows a mode badge; Edit/Delete disabled for `router` pools; a "Push to router" action (pick a NAS) on `system` pools; an "Adopt from router" entry. All new strings trilingual.

### C4. Router-field dropdowns (FR-110)
- `ServiceProvisionModal`: on open (when the NAS has API creds), call `discoverServices(nasId)`; derive distinct `ros_server_name` / `interface_note` / `router_pool_name` lists. Replace the two free-text `<input>`s with the panel `Combobox` (type-ahead, **allows an off-list value** so a not-yet-on-router name can still be typed for the create case). Convert this modal's hand-rolled raw `<select>`s to the panel `Select` at the same time (keeps the `no-native-controls` gate green — this modal currently uses raw `<select>`, exempt only because it predates the gate's file set; converting is in scope). Free-text fallback preserved.

## Integration gate
1. **C1** — `TestIntentConstantsInSync` includes the new pair; a `vendor` test asserts `IntentAddressList` encodes `Mikrotik-Address-List`; a `policy` test asserts a normal login with a profile address-list emits `address_list`, and an **expired** session does **not** (no collision); `authorize.pl` change present.
2. **C2** — a DB-backed AuthView test resolves `address_list` with override-wins; profile/subscriber create/update round-trip the field; profile edit invalidates the policy cache when it changes.
3. **C3** — `PlanPool` unit tests (fake `ROSConn`) cover add/set/no-op; the push endpoint refuses a `router` pool and is hash-gated; adopt inserts `router`-mode rows and writes nothing to the router; update/delete refuse `router` pools.
4. **C4** — the service form renders dropdowns from discovery with a free-text fallback; no raw native `<select>`/`<input>` left in the modal (gate `lint-no-native-controls.sh` green).
5. **v2-12 CI floors** green; new strings trilingual (`i18n:check`); backend `go build`/`vet`/`test` green; harness smoke unaffected.
6. **No regressions**: existing RADIUS/pools/profiles/subscribers tests pass; the expired-redirect path is byte-unchanged.

## Documented-pending (no hardware/stack in-session)
Real-router pool push/adopt round trip and a live address-list login are hardware legs (unit-tested via the fake `ROSConn` and the Go authorize tests; same posture as prior phases).

## Owner decisions (Decision 48, confirmed)
Pool strategy = adopt/own split (owned editable+pushable, adopted read-only); router config never edited by HikRAD except an explicit push of a HikRAD-owned object. `address_list` is vendor-neutral (MikroTik mapping only in the adapter). Existing pools default to `system` (preserve edit/delete).
