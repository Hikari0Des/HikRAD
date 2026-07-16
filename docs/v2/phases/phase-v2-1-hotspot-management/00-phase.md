# Phase v2-1 — Hotspot Management (hotspot-only subscribers + multi-service NAS + NAS scoping)

> Goal: subscribers carry a **`service_type` ∈ {pppoe, hotspot, dual}** (hotspot-only accounts are full records with quota applied); a single NAS runs **many Hotspot + PPPoE service instances** via a `nas_services` child table; and subscribers/profiles can be **scoped to a NAS/service and that scope is enforced at RADIUS auth** (`nas_not_allowed`). Reworks contracts frozen by Phase 2 (subscriber service model, FR-58 authorize branch, one-type-per-NAS) — an **expansion, not a fix**: pppoe/dual auth semantics are unchanged and every Phase-2 policy test must stay green.
>
> Requirements: **FR-61/63** (subscriber data + panel UX, sub-PRD 04) · **FR-62/64** (NAS model + auth-time enforcement, sub-PRD 02). Master PRD Decision 28. Owner: this is **SOLO + sequential** (Decision 25) — the C-numbers below are *contracts*, not agents.

## Execution ordering (one session, by dependency)

1. **Schema & backend model** — migrations 0500–0502; `service_type` on subscribers (drop `allow_hotspot`); `nas_services` (drop `nas.type`); assignment columns on subscribers+profiles. Update `internal/subscribers` (store/api/bulk/authview) and `internal/radius` NAS store/CRUD/seed.
2. **AuthView + RADIUS engine** — extend AuthView (C5); rework the authorize check chain (C6); add the vendor service-instance seam (C7); FreeRADIUS bridge forwards the raw attrs.
3. **Wizard + snippet** — multi-service snippet (C8); auto-setup additive per service.
4. **Panel UI** — service-type selector + `set_service_type` bulk + filters; NAS services sub-list; subscriber/profile NAS-assignment pickers; `nas_not_allowed` localization (C9/C10).
5. **Gate** — harness auth-matrix + lossless-migration + isolation legs; `scripts/gate-v2-phase-1.sh`; write `gate-result.md`.

Commit in reviewable chunks along these boundaries (schema+model / engine / wizard / UI / gate).

## Migration range 0500–0519 (partition)

| Migration | Owns |
|---|---|
| `0500_subscriber_service_type` | `subscribers.service_type text NOT NULL DEFAULT 'pppoe' CHECK (service_type IN ('pppoe','hotspot','dual'))`; backfill `allow_hotspot=false→'pppoe'`, `true→'dual'`; **then** `DROP COLUMN allow_hotspot`. |
| `0501_nas_services` | `nas_services` table (C3); backfill one row per NAS from `nas.type`; **then** `ALTER TABLE nas DROP COLUMN type`. |
| `0502_nas_scoping` | `subscribers.nas_id/nas_service_id` + `profiles.nas_id/nas_service_id` (nullable, `ON DELETE SET NULL`) + supporting indexes. |
| `0503`–`0519` | reserved (extra indexes / follow-ups discovered during build). |

The 0500 and 0501 add→backfill→drop sequences are single migrations because all Go reads switch to the new column in the same phase/binary (on-prem single-version deploy; migrations run at the new binary's boot).

**No `.down.sql` files** (amended 2026-07-16, owner-confirmed — supersedes this doc's original "every migration ships a paired `.down.sql`"). Reasons, in order of authority:
1. **FR-51.4 / [docs/ops/update.md](../../../ops/update.md)**: migrations are *forward-only*; "there is no down-migration path in production". A failed update rolls back by restoring the pre-update images + the automatic pre-update backup, never by migrating down. The master PRD wins over a phase brief (doc hierarchy).
2. **Repo reality**: all 47 v1 migrations are `.up.sql` only; `platform.Migrate` only ever calls `m.Up()`.
3. **0500's down path is provably lossy**: `service_type` has three values, `allow_hotspot` has two — `hotspot→true` and `dual→true` collapse, so a down-then-up round trip would silently convert every hotspot-only account into `dual`, granting PPPoE access it never had. A rollback path that corrupts data on re-upgrade is worse than none.

Gate item 1 therefore tests the **forward** migration only.

## Frozen contracts

### C1. Non-invalidation guarantee (the hard constraint)
Every existing `internal/radius` policy test (`authorize_test.go`, `enforce_test.go`, `db_phase4_test.go`, `internal/subscribers/authorize_e2e_test.go`) must pass unchanged for **pppoe** and **dual** subscribers. Tests that construct `AuthView{AllowHotspot: ...}` are updated to `ServiceType: "pppoe"|"dual"` with **identical expected outcomes**. No pppoe/dual reject reason, rate, pool, session-count or quota result changes.

### C2. Subscriber service type (FR-61) — data
- Column per the 0500 migration above. AuthView field **`ServiceType string`** (`pppoe|hotspot|dual`) replaces `AllowHotspot bool`.
- Loader (`internal/subscribers/authview.go`) selects `s.service_type` in place of `s.allow_hotspot`.
- Create/update API: `service_type` string field replaces the `allow_hotspot` bool (see C9). CSV import mapping (`internal/importer`) maps the old/any `allow_hotspot`-style column to `service_type` (`true→dual`, `false→pppoe`) and accepts an explicit `service_type` column.

### C3. `nas_services` schema (FR-62)
```sql
CREATE TABLE nas_services (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  nas_id          uuid NOT NULL REFERENCES nas(id) ON DELETE CASCADE,
  service         text NOT NULL CHECK (service IN ('pppoe','hotspot')),
  label           text NOT NULL DEFAULT '',   -- zone / SSID / friendly name
  interface_note  text NOT NULL DEFAULT '',
  ip_pool_id      uuid REFERENCES ip_pools(id) ON DELETE SET NULL,  -- per-service pool
  ros_server_name text NOT NULL DEFAULT '',    -- RouterOS hotspot server / PPPoE service-name for instance matching
  enabled         boolean NOT NULL DEFAULT true,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX nas_services_nas_idx ON nas_services (nas_id);
```
Invariant: every NAS has ≥ 1 service row (enforced in CRUD, not schema — delete-last-service is refused by the API). `nasView` JSON drops `type`, adds `services: [{id, service, label, interface_note, ip_pool_id, ros_server_name, enabled}]`.

### C4. Subscriber/profile NAS-assignment columns (FR-64)
`subscribers` and `profiles` each get `nas_id uuid NULL REFERENCES nas(id) ON DELETE SET NULL` and `nas_service_id uuid NULL REFERENCES nas_services(id) ON DELETE SET NULL`. Nullable pair = **any NAS** (v1 default). `nas_service_id` set without `nas_id` implies its parent NAS.

### C5. AuthView deltas (contract C4 amendment)
```go
type AuthView struct {
    // ... all existing fields UNCHANGED ...
    ServiceType       string  `json:"service_type"`         // pppoe|hotspot|dual   (replaces AllowHotspot)
    AssignedNASID     string  `json:"assigned_nas_id"`      // FR-64 effective (subscriber-over-profile), "" = any
    AssignedServiceID string  `json:"assigned_service_id"`  // FR-64 effective, "" = any service
    ServicePoolName   string  `json:"service_pool_name"`    // FR-64.3 fallback pool of the resolved instance (loader fills per-request or engine resolves)
}
```
- The loader computes the **effective** assignment with subscriber-over-profile precedence: if the subscriber's `nas_id` is set, use the subscriber pair whole; else use the profile pair.
- `ServicePoolName` note: the resolved *service instance* is only known at auth time (depends on the request), so the engine resolves the instance's pool from `nas_services.ip_pool_id` **after** service-instance resolution (C7), not the loader. The AuthView field is populated by the engine for the reply; the loader leaves it empty. (Implementer may instead pass the resolved `nas_services` row directly into `replyIntents` — either is contract-conformant as long as C6's pool precedence holds.)

### C6. Authorize request/response + check chain (the core rework)
**Request (`authorizeRequest`) gains raw attrs for vendor service-instance resolution (C7):**
```go
CalledStationID string `json:"called_station_id"`  // MikroTik hotspot server / MAC
NASPortType     string `json:"nas_port_type"`
NASPortID       string `json:"nas_port_id"`
// existing: Service string (coarse pppoe|hotspot hint from the bridge) stays
```
**Response shape unchanged.** New reject reason constant **`ReasonNASNotAllowed = "nas_not_allowed"`**.

**Frozen check chain** (deltas marked ▶):
1. Known NAS (unchanged).
2. ▶ **Resolve service instance** via C7 (`vendor.For(nas.vendor).ResolveService(...)`) → `(service, *nasServiceRow)`. `service` from resolution supersedes the coarse hint. Resolution failure rejects **`nas_not_allowed`** (surfaced to FR-39) in both of these cases:
   - **No enabled instance of the requested kind on this NAS** (amended 2026-07-16, owner-confirmed): the NAS runs no enabled `nas_services` row of the coarse kind at all — e.g. a hotspot login lands on a PPPoE-only NAS. This is a *configuration* reject, not a subscriber reject: it precedes the subscriber lookup's service-type matrix (step 4) and is deliberately **not** `service_not_allowed`, which means "this subscriber may not use this service". Rationale: the operator's NAS model, not the account, is what forbids the session, and FR-39's debug view must be able to tell those two apart at a glance.
   - **Ambiguous match**: multiple candidate instances of the coarse kind exist and none matches the request's identifying attributes.
3. Resolve view; voucher fallback (hotspot only) — unchanged; a voucher bypasses steps 4–5.
4. ▶ **Service-type matrix (FR-61)** — replaces the single `!AllowHotspot` gate:

   | `service_type` | request `pppoe` | request `hotspot` |
   |---|---|---|
   | `pppoe` | accept-path | **reject `service_not_allowed`** |
   | `hotspot` | **reject `service_not_allowed`** | accept-path |
   | `dual` | accept-path | accept-path (FR-58 semantics) |
5. ▶ **NAS scope (FR-64)** — after service-type, before credentials: if `AssignedNASID != ""` and it ≠ the authenticating NAS's id → reject `nas_not_allowed`; if `AssignedServiceID != ""` and it ≠ the resolved instance's id → reject `nas_not_allowed`. Empty = any.
6. Credentials — unchanged (skipped for voucher).
7. Status disabled — unchanged.
8. Expiry (FR-9) — unchanged (applies to hotspot-only exactly like pppoe).
9. ▶ **Quota (FR-10)** — currently skipped for all hotspot requests; new rule: skip **only** when the subscriber is `dual` **and** the request is `hotspot` (FR-58.3 dual exemption). For `hotspot`-only (`service_type='hotspot'`) the quota **applies** (FR-61.3).
10. ▶ **Session limit (FR-5/58.2/61.3)** — `sessionLimitReject(view, hotspot)` becomes service-type-aware:
    - `dual` + hotspot request → keep the FR-58.2 rule (≥1 concurrent hotspot ⇒ `session_limit`).
    - `hotspot`-only + hotspot request → count hotspot sessions against `view.SessionLimit` (`>0 && count>=limit ⇒ session_limit`).
    - pppoe request (pppoe/dual) → unchanged (count pppoe against `SessionLimit`).
11. MAC lock — unchanged (`if !hotspot` guard already excludes hotspot).

**Reply pool precedence (FR-64.3) is SERVICE-AWARE** in `replyIntents` (corrected 2026-07-16 after a pilot bug — see [known-issues.md](../../../ops/known-issues.md), "no more free addresses in the pool"):
- **pppoe request:** `static_ip` → resolved pppoe-service instance's `ip_pool_id` (if set) → profile `PoolName` → **omit**.
- **hotspot request:** `static_ip` → resolved hotspot-service instance's `ip_pool_id` (if set) → **omit**. The profile's `PoolName` is a PPPoE pool and is **NEVER** applied to a hotspot session — omitting `address_pool` lets the MikroTik Hotspot assign from its own interface/DHCP pool (a hotspot-only router's normal behaviour).

Rationale: v1 unconditionally emitted the profile's `Framed-Pool` on hotspot logins, so the router tried to allocate from a (often nonexistent/empty) named pool and failed. Hotspot addresses now come from the hotspot **service instance** (operator-configured) or the router's local pool — never the PPPoE profile pool. Already true that an empty pool omits the intent (`add()` skips ""); this phase makes the fallback service-aware and **locks it with tests** (gate item 6). Document this on the pools screen: "profile pools are PPPoE; hotspot pools are per-service or router-local."

### C7. Vendor service-instance resolution seam (FR-17 — the isolation boundary)
New adapter method on `vendor.Adapter`:
```go
// ResolveService maps a NAS's request-identifying RADIUS attributes to one of
// its nas_services rows. Vendor-specific attribute parsing (Called-Station-Id
// hotspot-server encoding, NAS-Port-Type, etc.) lives ONLY here (FR-17).
ResolveService(q ServiceQuery, candidates []ServiceInstance) (ServiceInstance, bool)
```
- `ServiceQuery{Service, CalledStationID, NASPortType, NASPortID string}` (coarse `Service` = the bridge hint); `ServiceInstance` mirrors the `nas_services` row fields the adapter needs (`ID, Service, ROSServerName`).
- The MikroTik adapter matches a hotspot request's `CalledStationID`/server-name against `ros_server_name`; falls back to the single enabled instance of the coarse kind. **No RADIUS-attribute-name parsing for instance identity appears anywhere under `internal/radius` outside `vendor/`** — `scripts/lint-vendor-isolation` must stay green (extend its grep list if new attribute tokens are introduced).
- The FreeRADIUS bridge (`deploy/freeradius/scripts/authorize.pl`) forwards `Called-Station-Id`, `NAS-Port-Type`, `NAS-Port-Id` (it keeps its existing coarse pppoe/hotspot guess as the `service` hint). The perl bridge is vendor-integration glue outside the Go isolation grep; keep its logic minimal (forward, don't interpret).

### C8. Wizard snippet shape (FR-62.4)
`GET /api/v1/nas/{id}/config-snippet?ros=6|7` renders **all enabled `nas_services`** in one snippet: one shared `/radius` + PPPoE AAA block when any pppoe service is enabled + one `/ip hotspot` block per enabled hotspot service (walled-garden from branding, FR-18). `vendor.SnippetInput` gains `Services []ServiceSnippet{Service, Label, ROSServerName, PoolName, Interface}`; the adapter loops. ROS 6/7 tabs preserved. FR-56 auto-setup (`PlanAutoSetup`) plans each service's entries additively.

### C9. REST/API deltas (frozen shapes)
- **Subscriber create/update:** `allow_hotspot` (bool) → `service_type` (`"pppoe"|"hotspot"|"dual"`, default `"pppoe"`); add `nas_id`, `nas_service_id` (nullable strings). Detail/list responses expose `service_type` + assignment.
- **Bulk:** action `set_allow_hotspot` → **`set_service_type`** (param `service_type`); audit action string `subscriber.bulk_set_service_type`.
- **Profile create/update:** add `nas_id`, `nas_service_id` (nullable).
- **NAS create/update:** drop top-level `type`; add `services: [ {service, label, interface_note, ip_pool_id, ros_server_name, enabled} ]` (≥1 required; delete-last refused). `nasView` returns `services[]`. Optional convenience sub-routes may be added but the embedded array is the frozen contract.
- **List filters:** `?service_type=pppoe|hotspot|dual` on `GET /subscribers` (and, coordinated, live sessions/reports).

### C10. Panel UX (FR-63)
Subscriber form: PPPoE/Hotspot/Both radio (persists `service_type`) + NAS/service assignment pickers (nullable = "Any NAS"). Profile form: NAS/service assignment pickers. NAS page: services sub-list (per-service status + session count) with per-service wizard steps. **Subscriber list stays UNIFIED** (owner-confirmed 2026-07-16): hotspot-only accounts are ordinary subscribers in the one list, discovered via a `service_type` filter chip — there is deliberately no separate "Hotspot users" section (that would re-create the SAS4 split the FR-61 brief set out to remove, and Sara's ≤3-click flow wants one place to search). Live sessions filter by `service_type` the same way. New reject reason `nas_not_allowed` localized in the FR-39 debug reason map. **All new strings in `frontend/shared/locales/{en,ar,ku}/*.json`; `npm run i18n:check` CI-fatal.**

### C11. Cache invalidation fan-out (FR-64.4)
`radius.InvalidatePolicy(subscriberID)` fires on subscriber `service_type`/assignment change (existing per-mutation call covers it). A **profile** assignment change must fan out to that profile's subscribers — add a `radius.InvalidatePolicyByProfile(profileID)` (or loop subscriber ids) invoked from the profile-update path. Frozen: a profile assignment change invalidates every affected cached AuthView.

## Integration gate

Green when all pass (scriptable legs in `scripts/gate-v2-phase-1.sh`; human/harness legs noted):

1. **Lossless migration** — apply 0500/0501 against a DB seeded with mixed `allow_hotspot` + typed NAS rows: every `false→pppoe`, `true→dual`, one `nas_services` row per NAS from `type`, zero row loss. Forward-only, per the migration-range note above (no down leg). (DB-gated Go test.)
2. **Phase-2 regression** — the full `internal/radius` + `internal/subscribers` suites pass with pppoe/dual outcomes byte-for-byte unchanged (C1).
3. **Service matrix (harness-driven)** — the RADIUS packet harness drives, per the C6 table: pppoe-only rejects hotspot `service_not_allowed`; hotspot-only rejects pppoe `service_not_allowed` and accepts hotspot with hotspot rate + quota enforced + `session_limit`-capped concurrency; dual accepts both with FR-58 semantics; voucher still bypasses.
4. **Multi-service NAS (harness-driven)** — one NAS with 2 hotspot + 1 pppoe `nas_services` rows: each request resolves to its own instance and gets that instance's pool; the config-snippet covers all three; live sessions carry the service instance.
5. **NAS scoping** — a subscriber assigned to NAS A rejects `nas_not_allowed` on NAS B (and on a non-assigned service instance) and accepts on the assigned one; profile-level assignment applies unless the subscriber overrides (subscriber-over-profile). Also: a hotspot request to a NAS with **no enabled hotspot instance** rejects `nas_not_allowed` (not `service_not_allowed`), and the converse for pppoe (C6 step 2).
6. **Service-aware pools (the pilot-bug lock)** — (a) a **dual/hotspot** subscriber whose profile HAS a PPPoE pool but whose resolved hotspot service has none ⇒ the hotspot accept reply contains **no** `address_pool` intent (router-local pool used; the v1 "no free addresses" bug cannot recur); (b) the same subscriber's **pppoe** login still gets the profile pool; (c) setting a hotspot-service pool makes hotspot emit that pool; (d) no-pool-anywhere on pppoe omits it too. (Locks item 24 + the pilot bug; documented on the pools screen.)
7. **Vendor isolation** — `scripts/lint-vendor-isolation` green: service-instance resolution + any new RADIUS-attribute parsing live only under `internal/radius/vendor/`.
8. **Panel** — `frontend/panel` build + lint + vitest green; `frontend` `i18n:check` green (0 missing keys, 0 hardcoded strings) incl. the service-type selector, filters, assignment pickers, `nas_not_allowed`.
9. **Docs accuracy** (Decision 27) — PRD/sub-PRD/index reflect FR-61–64; `known-issues.md` carries any bug found while building.

Human/hardware legs (documented-pending like v1 gates): live multi-service auth on a real MikroTik running ≥2 hotspot servers + PPPoE; auto-setup apply of a multi-service snippet on real hardware.

## Bugs
Any bug found while building goes in [docs/ops/known-issues.md](../../../ops/known-issues.md) with root cause + fix + commit (Decision 27 / v2 rule 3), before or alongside the fix.
