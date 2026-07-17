# Phase v2-2 — NAS Auto-Setup Config Manager + Hotspot/PPPoE Server Management

> Goal: replace the FR-56 auto-setup wizard's fixed, additive-only plan with a **form-driven** one that can read the router's current RADIUS-relevant config first (FR-65), and let the operator resolve a conflicting item as `keep` / `update` / `abort` instead of only `abort` (FR-66) — abort stays the default, nothing is ever deleted. On top of that pipeline, add **Hotspot/PPPoE server management** (FR-67, Decision 31): every `nas_services` instance carries a management mode (`router` = discovered/adopted, `system` = HikRAD-created/owned), and operators can list live router config per instance, create a new system-managed server, edit a system-managed one, or adopt a router-managed one — from a panel form, never Winbox.
>
> Requirements: **FR-65/66/67** (all owned by sub-PRD 02 — no subscriber/profile-facing half this time). Master PRD Decision 31 (scope) / Decision 33 (FR commitment). Owner: **SOLO + sequential** (Decision 25) — the C-numbers below are *contracts*, not agents.
>
> This phase also resolves — **partially** — the open known-issue row "NAS auto-setup (multi-hotspot)" (`docs/ops/known-issues.md`): FR-66's per-conflict resolution lets a router with the **stock `default` hotspot profile** be updated instead of only refused, but a router whose zones already carry **custom-named profiles** still has no single target for FR-66's plan to update — FR-67's **adopt** flow is the actual fix for that case (the operator picks the exact zone, HikRAD reads its real profile, editing proceeds against a specific instance rather than a guess). Update the known-issues row to reflect this split when implementation lands, not before.

## Execution ordering (one session, by dependency)

1. **Schema** — migration(s) in the 0520–0529 budget (verify against the repo's actual current max before writing — see the migration-numbering rule in `docs/v2/phases/00-v2-execution-plan.md` rule 4; if the max has advanced past 0529 by the time this is implemented, take the next free number instead and note it here).
2. **Vendor layer (FR-65 + FR-66 core)** — `vendor.Adapter.ReadConfig` (C2); extend `SnippetInput` with override plumbing already mostly present (C3); rework `PlanAutoSetup`/`PlanConflict` to carry resolution keys + update items (C4/C5); extend `ApplyAutoSetup`'s caller-side hash coverage (C6).
3. **Vendor layer (FR-67)** — `PlanService`/service-scoped provisioning sentences (C8), reusing C4/C5's PlanItem/PlanConflict/AutoSetupPlan vocabulary; the FR-62.7 `address-pool=none` guard folded into every new hotspot server plan from creation, not bolted on after.
4. **API** — `GET /nas/{id}/config` (C2); extend the two existing auto-setup endpoints (C6); the four new service-management endpoints (C9).
5. **Panel** — "Current config" tab; values-form step; per-conflict radio choice UI; server create/edit/adopt screens; NAS services sub-list shows live router config per instance (C10).
6. **Gate** — fake-router unit matrix + `scripts/gate-v2-phase-2.sh`; live-router checklist item (create a real hotspot zone end-to-end); `gate-result.md`.

Commit in reviewable chunks along these boundaries (schema / vendor-FR65-66 / vendor-FR67 / API / panel / gate).

## Migration budget 0520–0529

| Migration | Owns |
|---|---|
| `0520_nas_services_management_mode` | `nas_services.management_mode text NOT NULL DEFAULT 'router' CHECK (management_mode IN ('router','system'))`. No backfill logic needed beyond the default: every row that exists before this phase — including v2 phase 1's migration-0501 backfill from `nas.type` and anything added via discovery/manual entry since — was **not** created through HikRAD provisioning, so `'router'` is correct for all of them without a special case. |
| `0521`–`0529` | reserved (follow-ups discovered during build; e.g. an index if the services-list-by-mode filter needs one). |

Forward-only, no `.down.sql` (Decision 25's amendment / FR-51.4 — the repo-wide rule, not re-litigated here).

## Frozen contracts

### C1. Non-invalidation guarantee (the hard constraint)
Every existing `internal/radius/vendor` auto-setup test (`mikrotik_autosetup_test.go`) and every `internal/radius` NAS/services test must keep passing for the **no-form-overrides, no-resolutions** case: calling preview/apply with an empty `values` object and an empty `resolutions` map must produce byte-identical `Items`/`Conflicts`/behavior to today's FR-56.2 plan. Nothing in this phase changes what auto-setup does when the operator touches nothing new.

### C2. Config inspection (FR-65)

New `vendor.Adapter` method:
```go
// ReadConfig reads the router's current RADIUS-relevant state (FR-65). Pure
// print sentences, same ROSConn seam as PlanAutoSetup/DiscoverServices/
// CheckHealth — never writes. Vendor-specific paths/fields live only here
// (FR-17).
ReadConfig(conn ROSConn) (ConfigSnapshot, error)
```
```go
type ConfigSnapshot struct {
    RadiusEntries   []RadiusEntryConfig
    RadiusIncoming  RadiusIncomingConfig
    PPPAAA          PPPAAAConfig
    HotspotProfiles []HotspotProfileConfig
    WalledGarden    []string
}
type RadiusEntryConfig struct {
    Address, Service, Comment, SrcAddress string
    SecretPresent bool // never the secret value itself
}
type RadiusIncomingConfig struct { Accept bool; Port int }
type PPPAAAConfig struct { UseRadius, Accounting bool; InterimUpdateSecs int }
type HotspotProfileConfig struct { Name string; UseRadius bool; InterimUpdateSecs int }
```
`GET /api/v1/nas/{id}/config` (`nas.view`-gated):
```json
{
  "nas_id": "...",
  "ros_version": "7.12",
  "board_name": "...",
  "identity": "...",
  "radius": [{"address":"...", "service":"ppp,hotspot,login", "comment":"hikrad-auto", "src_address":"", "secret_present": true}],
  "radius_incoming": {"accept": true, "port": 3799},
  "ppp_aaa": {"use_radius": true, "accounting": true, "interim_update_secs": 300},
  "hotspot_profiles": [{"name": "default", "use_radius": true, "interim_update_secs": 300}],
  "walled_garden": ["portal.example.com"]
}
```
- 422 `no_api_credentials` / 502 `router_unreachable` — identical contract to `probeNASHandler`/`discoverServicesHandler`.
- Audit action `nas.config_inspect` (FR-65.4), no before/after (a read has neither).
- Implementation note (non-binding): `ReadConfig`'s read sentences are the same ones `PlanAutoSetup`'s planXXX helpers already issue — factor the reads into shared helpers so C2 and C4/C5 cannot silently drift into reading two different things.

### C3. Values-form overrides (FR-66.1)

`vendor.SnippetInput` already carries every field the form needs (`RadiusServer`, `SrcAddress`, `CoAPort`, `InterimSecs`, `WalledGarden`, `Services`) — this FR does not add fields to it, it adds a **request-body path that reaches it** instead of `snippetInputFor` being the only source. New shared request fragment, used by both endpoints in C6 and by C9's service-scoped endpoints:
```go
// autoSetupValuesInput carries the FR-66.1 form overrides. Every field is a
// pointer/nil-able slice so "omitted" (keep HikRAD's settings/NAS-derived
// default) is distinguishable from "explicitly cleared".
type autoSetupValuesInput struct {
    RadiusServer *string   `json:"radius_server,omitempty"`
    SrcAddress   *string   `json:"src_address,omitempty"`
    CoAPort      *int      `json:"coa_port,omitempty"`
    InterimSecs  *int      `json:"interim_secs,omitempty"`
    WalledGarden *[]string `json:"walled_garden,omitempty"`
}
```
`snippetInputFor` (already the single source `GET /config-snippet` and auto-setup share, per its own doc comment) gains an `overrides autoSetupValuesInput` parameter: a non-nil field wins over the settings/NAS-derived value; nil falls back to exactly today's behavior. **The copy-paste snippet endpoint (`GET /config-snippet`) accepts the same `autoSetupValuesInput` as optional query/body overrides** so both paths keep describing one desired state (FR-66.1's explicit requirement) — this is the one user-facing change to an existing v1 endpoint in this phase, additive only (omitted body = unchanged behavior).

### C4. Per-conflict resolution (FR-66.2)

`vendor.PlanConflict` gains two fields:
```go
type PlanConflict struct {
    Path     string `json:"path"`
    Existing string `json:"existing"`
    Reason   string `json:"reason"`
    // Key identifies this conflict across the preview/apply round trip so the
    // operator's choice can be echoed back (C5). Stable per plan shape: today
    // one conflict can occur per Path, so Key == Path; if a future planner
    // ever needs >1 conflict per path, Key must be disambiguated then, not now.
    Key string `json:"key"`
    // Resolvable is true when an "update" resolution has a computable target
    // sentence (UpdateCommand). False for a conflict with no single safe
    // target — the ONLY case in this phase is the hotspot-profile conflict on
    // a router whose zone uses a custom (non-"default") profile name: there is
    // no profile to update without guessing, so it stays abort-only here.
    // FR-67's adopt flow is the real answer for that router (see this doc's
    // header note).
    Resolvable bool `json:"resolvable"`
    // UpdateCommand is the human-readable command an "update" resolution would
    // run — shown in the panel next to the keep/update/abort choice so the
    // operator approves an exact sentence, same transparency PlanItem.Command
    // already gives additive items. Empty when Resolvable is false.
    UpdateCommand string `json:"update_command,omitempty"`
}
```
Internal only (never serialized — mirrors `PlanItem.Sentence`'s existing `json:"-"` pattern): each conflict-producing planXXX helper (`planRadiusEntry`, `planRadiusIncoming`, `planPPPAAA`; **not** `planHotspotProfile`, whose only conflict is the unresolvable custom-profile case) also returns the `Sentence []string` an "update" would send, carried privately alongside the `PlanConflict` until `PlanAutoSetup` resolves it into a `PlanItem` or discards it.

### C5. `PlanAutoSetup` resolution wiring (FR-66.2/66.3)

```go
// PlanAutoSetup gains a resolutions parameter (FR-66): resolutions[key] is
// "update" or "keep"; any other value (including absent) means "abort" —
// identical to today's behavior when the map is empty or nil.
PlanAutoSetup(conn ROSConn, in SnippetInput, resolutions map[string]string) (AutoSetupPlan, error)
```
For each item that would have produced a `PlanConflict` under the old (pre-FR-66) logic:
- `resolutions[key] == "update"` **and** the conflict is `Resolvable`: append the update `PlanItem` (Action `"set"`) to `plan.Items`; do **not** add it to `plan.Conflicts`.
- `resolutions[key] == "update"` and **not** `Resolvable`: still a conflict (an operator cannot force an update onto nothing to update) — behaves exactly like `"keep"` was never chosen; `plan.Conflicts` gets the entry with `Resolvable: false`.
- `resolutions[key] == "keep"`: neither `plan.Items` nor `plan.Conflicts` gets an entry for this key — the operator explicitly accepted the router's current state for this one item, and auto-setup proceeds with everything else.
- anything else (unset, `"abort"`, unrecognized string): `plan.Conflicts` gets the entry, `Resolvable` and `UpdateCommand` filled in as computed — **identical to today's abort-only behavior**, so `len(plan.Conflicts) > 0` remains the single gate `autoSetupApplyHandler` checks before writing anything (C1: this is the non-invalidation guarantee made concrete).

`AutoSetupPlan.Items`/`.Conflicts` shape is otherwise unchanged from today.

### C6. HTTP contract deltas for the existing two endpoints (FR-66.1/66.3)

```go
type autoSetupPreviewRequest struct {
    Values      autoSetupValuesInput `json:"values"`
    Resolutions map[string]string    `json:"resolutions"` // key -> "update"|"keep"; omitted = today's abort-only
}
type autoSetupApplyRequest struct {
    PreviewHash string                `json:"preview_hash" validate:"required"`
    Values      autoSetupValuesInput  `json:"values"`      // MUST match what produced PreviewHash — recomputed, never trusted
    Resolutions map[string]string     `json:"resolutions"` // ditto
}
```
- `POST /nas/{id}/auto-setup/preview` now accepts a body (previously body-less); an empty/omitted body behaves exactly as before (C1).
- `planHash` (the tamper-safety digest) is extended to fold in `Values` and `Resolutions` alongside the existing items/conflicts digest, so: (a) a router-state change between preview and apply is still caught (unchanged guarantee), AND (b) apply is cryptographically tied to the *exact* values+resolutions the operator saw in preview — resending a stale resolutions map (e.g. after the operator changed a choice) recomputes a different hash and `preview_stale` fires, exactly like today's router-drift case.
- `autoSetupApplyHandler`'s existing `len(plan.Conflicts) > 0` abort check is unchanged in code shape — it now naturally also covers "the operator left something unresolved or chose abort," because C5 already folded that into `Conflicts`.
- Response shapes (`autoSetupPreviewResponse`, `autoSetupApplyResponse`) are unchanged except `Conflicts` items now carry the C4 fields.

### C7. Never delete (FR-66.4)

No code path introduced by C2–C6 ever constructs a RouterOS `/remove` sentence, nor a `/set` targeting a field that erases router config beyond what FR-56.2 already reasoned about (the `update` resolution only ever rewrites a HikRAD-relevant field to HikRAD's own desired value — it never touches an unrelated field on the same object). This is gate-checked (see Integration gate item 7).

### C8. Service-scoped provisioning (FR-67.3/67.4) — the vendor layer

New `vendor.Adapter` method, deliberately separate from `PlanAutoSetup` (different sentence vocabulary — binding interfaces, local addresses, PPPoE profiles/service-names — not the shared RADIUS/AAA wiring FR-66 covers):
```go
// PlanService computes the FR-67.3/67.4 preview for creating or editing ONE
// system-managed server instance. Read-only connect, additive/update-only
// writes, same PlanItem/PlanConflict/AutoSetupPlan vocabulary as
// PlanAutoSetup so the HTTP layer, hashing, and panel rendering are shared
// code, not a parallel implementation.
PlanService(conn ROSConn, in ServiceProvisionInput) (AutoSetupPlan, error)
// ApplyService executes a PlanService result exactly like ApplyAutoSetup
// (same whole-apply-abort-on-first-failure contract).
ApplyService(conn ROSConn, plan AutoSetupPlan) []ApplyResult
```
```go
type ServiceProvisionInput struct {
    Kind          string // "pppoe" | "hotspot"
    ROSServerName string // required; must be unique among enabled instances of Kind (validated same as C9/serviceInput today)
    Label         string
    Interface     string // required: the router interface/bridge this server binds
    LocalAddress  string // hotspot only: the server's own gateway IP
    AddressRange  string // hotspot only, optional: DHCP range for a HikRAD-created pool
    PoolName      string // resolved HikRAD ip_pool name, or "" = router-local (mirrors C6/FR-64.3's existing pool precedence)
    Values        SnippetInput // reuses RadiusServer/Secret/CoAPort/InterimSecs/WalledGarden — a service plan wires this ONE instance to HikRAD's RADIUS exactly like PlanAutoSetup does for the whole NAS
}
```
- A new **hotspot** create plan **always** includes the FR-62.7 `address-pool=none` guard item on the server's user profile as part of the initial plan (FR-67.3's explicit requirement) — not a follow-up health-check fix, because a server HikRAD itself creates must never ship with the pilot's known trap.
- `PlanService` on a NAS with an existing enabled instance sharing the same `(Kind, ROSServerName)` returns a `PlanConflict` (not an error) — mirrors `validateServices`'s existing ambiguity guard, applied at the router-truth layer instead of only HikRAD's DB.
- No `Resolutions`/`Key` machinery here (unlike C4/C5) — a service-provisioning conflict has no "update the pre-existing thing" meaning (you cannot "update" your way into two servers sharing an identity); it is create-abort-only, matching the C1 spirit of "abort stays the default, resolution is additive and only where it's actually safe."

### C9. HTTP contract for FR-67 (server management)

```
GET  /api/v1/nas/{id}/services/{serviceId}/router-config   (nas.view)   — C2's ReadConfig, narrowed to one instance's router-side fields (interface, pool, profile, service-name, enabled). Works for BOTH management modes — a router-mode row is always inspectable, only writes are gated on mode.
POST /api/v1/nas/{id}/services/plan                        (nas.view)   — ServiceProvisionInput body (+ optional `service_id` when editing an existing SYSTEM-managed row); response: {items, conflicts, preview_hash, ros_version} (same shape as C6). 409 `not_adopted` if service_id names a ROUTER-managed row (FR-67.4: edit requires adopt first).
POST /api/v1/nas/{id}/services/apply                        (nas.edit)   — {service_id?, preview_hash, ...same body as plan}; on all_ok, upserts the nas_services row (insert with management_mode='system' if service_id was empty; update in place otherwise) using the same replaceServices-family persistence C3 (v2-1) already owns. Response: {results, all_ok, service: <serviceView>}.
POST /api/v1/nas/{id}/services/{serviceId}/adopt             (nas.edit)   — body {confirm: true} (required; a bare POST without it is refused — FR-67.5's "even an unchanged confirm is an explicit action"). Writes NOTHING to the router: sets nas_services.management_mode='system' only. Audited nas.service_adopt with before={management_mode:"router"} / after={management_mode:"system"}. 409 `already_system` if not currently router-mode. 409 `not_found` if the service_id doesn't belong to this NAS.
```
- `serviceView` (existing, C3 of phase v2-1) gains `management_mode` (`json:"management_mode"`) — additive field, no break to existing consumers.
- FR-67.4's edit-refusal: `POST /services/plan` and `/services/apply` both check the target row's `management_mode` before doing any router I/O; `router`-mode refuses with 409 `not_adopted` and a message pointing at the adopt endpoint.

### C10. Panel UX

- **Auto-setup wizard** (existing `NasAutoSetupModal`) becomes: Current-config tab (C2, rendered read-only, informational) → Values-form step (C3 fields, pre-filled from settings and, where C2 read them, from the router) → Preview step, where each conflicting item now renders as a three-way choice (`keep` / `update — show exact command` / `abort`, default `abort`) instead of only a red "blocked" row → Apply.
- **NAS services sub-list**: each row shows a management-mode badge (`Router` / `System`); expanding a row shows its live router config (C9's `router-config` read) alongside HikRAD's stored `nas_services` fields, so drift between the two is visible, not just HikRAD's own idea of the world.
- **Create server**: a form driving `ServiceProvisionInput` (C8), same preview/apply/hash-gated flow pattern as the auto-setup modal, reusing its conflict-choice component where applicable (though C8 conflicts are abort-only, per C8's note).
- **Edit** (system-mode only) / **Adopt** (router-mode only): mutually exclusive actions on a service row per its mode; adopt shows the full read-only config (C9's router-config) with one explicit "Adopt and enable editing" confirm button — no fields to change at adopt time, per C9.
- **All new strings** in `frontend/shared/locales/{en,ar,ku}/*.json`; `npm run i18n:check` CI-fatal, same as every prior phase.

## Integration gate

Green when all pass (scriptable legs in `scripts/gate-v2-phase-2.sh`; human/hardware legs noted):

1. **Migration** — `0520_nas_services_management_mode` present; adds the column with `DEFAULT 'router'` and a `CHECK` constraint; no `.down.sql`; no migration number outside the verified-free budget at implementation time.
2. **C1 non-invalidation** — the full existing `internal/radius/vendor` auto-setup suite (`mikrotik_autosetup_test.go`) passes unchanged when driven with empty `Values`/`Resolutions` (fake-`ROSConn` unit tests, no DB needed).
3. **Config inspection (FR-65)** — fake-router unit test: `ReadConfig` against a planted `/radius`+`/ppp/aaa`+`/ip/hotspot/profile`+walled-garden state returns the exact `ConfigSnapshot` expected; `GET /nas/{id}/config` returns 422 with no saved API credentials and 502 when the fake connect fails; an audit row is written on success.
4. **Form-driven values (FR-66.1)** — unit test: passing `Values.RadiusServer`/`CoAPort`/`InterimSecs`/`WalledGarden` overrides through `PlanAutoSetup` changes the resulting `PlanItem.Command`/`Sentence` accordingly; the same overrides passed to the snippet endpoint (`GET /config-snippet`) render into the copy-paste text identically — a byte-level "both paths describe one desired state" assertion (mirrors FR-14/FR-56's existing consistency test).
5. **Modify-or-create (FR-66.2/66.3)** — fake-router unit test: a planted foreign `/radius` entry with `resolutions["<radius-key>"]="update"` produces a `/set`-action `PlanItem` (never `/add`) targeting the existing entry's `.id`, and an empty `Conflicts` for that key; `"keep"` produces neither an item nor a conflict for that key and every other planned item still applies; an unresolved/`"abort"`-resolved conflict still blocks the whole apply exactly as pre-FR-66 (`len(Conflicts) > 0` refuses); the hotspot-profile custom-name case reports `Resolvable: false` regardless of resolution value. `planHash` differs across different `Resolutions`/`Values` inputs for an otherwise-identical router state, and `POST /auto-setup/apply` returns `preview_stale` when the resent `Values`/`Resolutions` don't reproduce the given hash against a freshly-read (possibly drifted) router.
6. **Never delete (FR-66.4/67.6)** — a repo grep (extends `scripts/lint-vendor-isolation.sh` or a sibling script) asserts no `/remove` literal appears in `internal/radius/vendor/mikrotik_autosetup.go` or any FR-67 service-provisioning file.
7. **Vendor isolation (FR-17)** — `scripts/lint-vendor-isolation.sh` green; all new RouterOS paths/fields (C2's read paths, C8's service-provisioning sentences) live only under `internal/radius/vendor/`.
8. **Server create (FR-67.3)** — fake-router unit test: `PlanService` for a new hotspot instance on a NAS that already runs a PPPoE service includes the FR-62.7 `address-pool=none` guard item from the first plan (not a follow-up health-check fix); on a mocked all-OK `ApplyService`, the resulting `nas_services` row (via the HTTP handler, DB-gated test) has `management_mode='system'`.
9. **Adopt (FR-67.5)** — DB-gated test: adopting a `router`-mode service flips `management_mode` to `system` with **zero** `conn.Write` calls (a fake `ROSConn` that panics on `Write` proves it), and the audit row shows `management_mode: router → system`; a bare adopt POST without `confirm: true` is refused; adopting an already-`system` row returns 409 `already_system`.
10. **Edit-requires-adopt (FR-67.4)** — DB-gated test: `POST /services/plan` (and `/apply`) against a `router`-mode `service_id` returns 409 `not_adopted` without any router I/O attempted.
11. **Panel** — `frontend/panel` build + lint + vitest green; `frontend` `i18n:check` green (0 missing keys, 0 hardcoded strings) covering the current-config tab, values-form step, per-conflict resolution choice, server create/edit/adopt screens, management-mode badge.
12. **Docs accuracy** — PRD/sub-PRD 02/index reflect FR-65–67 (this phase's Step-1 commit); `docs/ops/known-issues.md`'s "NAS auto-setup (multi-hotspot)" row updated to reflect the FR-66/FR-67 split described in this doc's header note, once implementation actually lands (not merely documented as a plan); any new bug found while building gets its own row per the standing rule.

**Human/hardware legs** (documented-pending like every prior phase's live-router items): (a) create a real hotspot zone end-to-end on a physical MikroTik via FR-67.3 and authenticate a real subscriber through it; (b) adopt a real pre-existing PPPoE server via FR-67.5 and edit its default profile from HikRAD, confirming the router-side change; (c) exercise an `update` resolution (FR-66.2) against a real router's foreign `/radius` entry and confirm the rewrite via Winbox.

## Bugs

Any bug found while building goes in [docs/ops/known-issues.md](../../../ops/known-issues.md) with root cause + fix + commit (Decision 27 / v2 rule 3), before or alongside the fix.
