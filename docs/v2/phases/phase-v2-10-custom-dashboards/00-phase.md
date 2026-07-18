# Phase v2-10 — Customizable Per-Manager Dashboards

Source brief: [docs/v2/10-custom-dashboards.md](../../10-custom-dashboards.md). Requirements
FR-89 (widget catalog + permission gating) and FR-90 (per-manager layout),
committed by PRD Decision 41. FR-89/90 owned entirely by sub-PRD
[03-lossless-accounting-live-monitoring.md](../../../prd/03-lossless-accounting-live-monitoring.md)
(FR-32's existing owner — no domain split, same precedent as FR-65–67/68–70/81–84/86–88).
**Hard dependency (now satisfied):** v2-6's `manager_preferences` table — see
[docs/v2/phases/phase-v2-6-preferences/gate-result.md](../phase-v2-6-preferences/gate-result.md),
gate GREEN 27/27, shipped 2026-07-18 immediately before this phase's kickoff
in the same session.

## 1. Problem (restated from the source brief)

FR-32's dashboard is one fixed layout for everyone. Omar (owner) wants
revenue and NAS health first; Sara (front desk) cares about expiring
subscribers; Hassan (agent) mostly needs his balance and his own
subscribers. Nobody can reorder, hide, or resize anything, and a manager
without a tile's underlying permission still gets a layout designed around
tiles they can't load — worse, **today the entire `GET /api/v1/dashboard`
call sits behind one single permission (`monitoring.view`)**, so a manager
holding that one permission currently sees revenue-today regardless of
whether they hold `reports.view`, and a manager lacking `monitoring.view`
gets none of the dashboard at all, not even their own subscriber counts.
Per-widget permission gating is a real tightening of the current model, not
just a cosmetic feature.

## 2. Scope for this implementation pass

1. **Backend** — widget catalog (Go source of truth, `internal/monitorsvc`),
   `GET /api/v1/dashboard?widgets=` parametrization (C3), `manager_preferences.doc`
   gains the optional `dashboard_layout` key (C2) — no migration.
2. **Panel** — `DashboardPage` becomes a widget-registry renderer; an edit
   mode (add/remove from the permitted catalog, reorder, 1×/2× size toggle,
   reset-to-default) reachable from the dashboard itself.
3. **Gate** — DB-gated tests for permission-gating, forbidden-widget-absence,
   cross-device layout, default-equals-today, reset-to-default, and
   backward-compatible no-`?widgets=` behavior; `scripts/gate-v2-phase-10.sh`;
   `gate-result.md`.

Commit in reviewable chunks, in dependency order: **widget registry (backend
catalog + endpoint split) → layout store (manager_preferences wiring, no
migration) → edit mode (panel) → data-endpoint split verification/gate**,
matching this phase's own kickoff instruction.

## Migration budget

**None used.** The layout is one more optional key inside v2-6's existing
`manager_preferences.doc` JSONB column — adding an optional field to a JSON
document already in production is not a schema change, so this phase issues
no migration file. (The source brief's execution-plan row originally budgeted
0590–0599 for this phase; that budget is stale now that v2-6 consumed
0589–0590 — moot here since nothing in this phase needs a number at all. If a
follow-up inside this phase's own build ever does need one, the next free
number above the repo's actual max at that time applies, per the standing
linear-numbering rule.)

## Frozen contracts

### C1. Widget catalog (FR-89.1/89.2) — permission map + size class

A Go-side catalog (`internal/monitorsvc`, e.g. `dashboard_widgets.go`) is the
single source of truth both the endpoint and (via a generated/mirrored
TypeScript catalog, same pattern as `SERVICE_TYPES`/permission constants
already mirrored panel-side) the panel's picker read from — **the panel never
invents a widget id or permission string of its own**, it only ever renders
what the catalog above knows about.

| id | permission (`""` = none required) | default size | response key(s) | data source |
|---|---|---|---|---|
| `online-now` | `live.view` | `1x` | `online_now`, `online_24h_sparkline` | existing `onlineNow`/`onlineSparkline` |
| `revenue-today` | `reports.view` | `1x` | `revenue_today_iqd` | existing `revenueToday` |
| `radius-rps` | `monitoring.view` | `1x` | `radius_rps` | existing `freeRADIUSHealth().ReqRate` |
| `subs-active` | `subscribers.view` | `1x` | `subs.active` | existing `subscriberTiles` (shared query, see C3 note) |
| `subs-expired` | `subscribers.view` | `1x` | `subs.expired` | existing `subscriberTiles` (shared query) |
| `subs-expiring` | `subscribers.view` | `1x` | `subs.expiring_7d` | existing `subscriberTiles` (shared query) |
| `pipeline-health` | `monitoring.view` | `2x` | `pipeline` | existing `fetchAcctCounters` |
| `nas-health` | `nas.view` | `2x` | `nas_cards` | existing `nasCards` |
| `my-balance` | `""` (self-view, every authenticated manager — same posture as [01](../../../prd/01-platform-install-licensing.md) FR-84.2's self-only preferences endpoint) | `1x` | `my_balance` | **new**: `GET /api/v1/managers/{me}/balances` equivalent, read directly (raw SQL against `manager_balances`, matching the existing "cross-domain reads are direct SQL, never a cross-package Go import" pattern `revenueToday`/`nasCards` already establish in this same file) |
| `pending-payment-tickets` | `payment_tickets.verify` | `1x` | `pending_payment_tickets` | **new**: `SELECT count(*) FROM payment_tickets pt JOIN subscribers s ON s.id = pt.subscriber_id WHERE pt.state = 'pending' AND (caller unscoped OR s.owner_manager_id = caller)` — same ScopeFilter posture as every other scoped list, direct SQL per the pattern above |
| `alerts-feed` | `monitoring.view` | `2x` | `alerts_feed` | **new**: most recent N (10) rows from `alert_events`, in-package (this module already owns that table — a Go function call, not cross-domain SQL) |

A widget id absent from this table is not a valid catalog entry — the
picker/endpoint reject it as unknown, not silently render/compute it
(fail-closed on typos, same posture C1's `notification_prefs` closed-set
validation already established in v2-6).

### C2. Dashboard layout schema (FR-90.1) — one more optional key in `manager_preferences.doc`

```go
// internal/auth (co-located with v2-6's Preferences struct — the same
// module owns the whole manager_preferences doc, one struct, one schema).
type Preferences struct {
    // ... v2-6's existing fields unchanged ...
    DashboardLayout *DashboardLayout `json:"dashboard_layout,omitempty"`
}

type DashboardLayout struct {
    Widgets []DashboardWidgetRef `json:"widgets"`
}

type DashboardWidgetRef struct {
    ID   string `json:"id"`   // a C1 catalog id
    Size string `json:"size"` // "1x" | "2x"
}
```

- **`nil` (key absent) = default layout** — every C1 widget the manager holds
  the permission for, in the C1 table's own row order, each at its default
  size. This is the exact "today's fixed dashboard, permission-filtered"
  behavior FR-90.1 requires, and it is what every manager gets until they
  save an explicit layout — no backfill, no migration touches existing rows,
  same "absence is the valid unset state" posture v2-6's C1 established.
- **A non-nil `DashboardLayout` (even `{"widgets":[]}`) is an explicit
  choice** and is honored exactly, re-filtered by the *caller's current*
  permissions at render time (never a stored snapshot) — a widget id present
  in a saved layout whose permission was later revoked is silently dropped
  at render, never a broken tile, per FR-90.3.
- **Validation (mirrors v2-6's C3 posture):** on `PUT /me/preferences`, an
  unknown widget id or an invalid `size` value (`"1x"`/`"2x"` are the only
  legal values) is a `422` naming `dashboard_layout.widgets.N.id` /
  `dashboard_layout.widgets.N.size`; nothing is written. A widget id whose
  *permission* the caller lacks is **not** a validation error at write time
  (the caller may be an admin curating another... no — self-only, so this
  case is actually "the caller added a widget they can't see," which cannot
  happen through the picker UI but is not rejected server-side either,
  matching FR-90.3's "filtered at render, not at write" design — the stored
  layout is allowed to outlive a permission change).
- **Reset-to-default is FR-84.2's existing full-document `PUT /me/preferences`
  with `dashboard_layout` omitted** — no new endpoint, no new verb. The panel
  reset button submits the current document with that one key stripped.

### C3. `GET /api/v1/dashboard?widgets=` (FR-89.3)

```
GET /api/v1/dashboard                                    → unchanged: today's full aggregate, every field always present (backward compatible)
GET /api/v1/dashboard?widgets=online-now,revenue-today    → 200, body contains ONLY the requested+permitted keys
```

- **Filtering, not the whole call, fails.** The permission check (C1's map)
  runs per requested id; an id the caller lacks the permission for is
  dropped from the response, never a `403` for the whole request — matching
  the phase's own constraint ("a forbidden widget is absent, not erroring").
  An id not in the C1 catalog at all is dropped the same way (unknown ids
  are not an error either — a client on an older catalog version degrades
  gracefully).
- **Cost, not just payload, shrinks.** The handler computes only the data
  each surviving id needs — a manager who only asked for `my-balance` never
  runs the sparkline/NAS-probe/pipeline-counter queries. **Exception, frozen
  explicitly so the implementer doesn't try to over-optimize a shared
  query:** `subs-active`/`subs-expired`/`subs-expiring` all read the same
  single `subscriberTiles` query; requesting *any one* of the three computes
  and returns the **full** `subs` object (all three counts) — only omitting
  **all three** skips the query. The client picks which sub-field a given
  widget renders; the shared query is not worth splitting three ways for a
  saving that only matters when all three are already hidden.
- **Response shape stays field-name-identical to today's** — `?widgets=`
  never renames or restructures any key, it only ever includes/omits whole
  top-level (or, for `subs`, whole nested-object) entries. This keeps a
  hypothetical unfiltered consumer (there is none today, but nothing stops
  one existing later) forward-compatible with either call shape.

### C4. Server-side enforcement is authoritative (FR-90.2)

The panel's widget picker only *offers* ids `useAuth().can(permission)`
allows — this is a convenience, never the boundary. The real boundary is
C3's per-request permission filter, run independently of whatever the client
sent, same "server re-checks every route" posture every other panel screen
already follows (CLAUDE.md's API conventions). A hand-crafted `?widgets=`
request or a manually-edited `dashboard_layout` naming a forbidden id proves
nothing — the data for it is simply never computed or returned.

### C5. Panel: edit mode + phone-first (FR-90.2/90.3)

- `DashboardPage` renders from the widget registry: resolve the effective
  layout (server layout if the manager has one, else C1's default order,
  filtered through `useAuth().can(...)` exactly like C3 filters
  server-side) → one component per widget id, sized by its `size` value.
- An edit-mode toggle exposes: **add** (a picker listing only catalog
  entries the manager can see and doesn't already have), **remove** (×
  per widget), **reorder** (drag, or an equivalent accessible up/down
  control — the *contract* is the interaction, not the implementation),
  **size toggle** (1×/2× per widget), **save** (`PUT /me/preferences` with
  the edited `dashboard_layout`), **reset to default** (C2's omit-the-key
  mechanism).
- **Phone-first is unconditional**: at the mobile breakpoint the grid is
  always a single column regardless of stored `size` — `2x` only ever
  widens a tile on larger screens, it never causes horizontal scroll or a
  multi-column phone layout. The stored **order** still applies at every
  breakpoint.

## Integration gate

Green when all pass (scriptable legs in `scripts/gate-v2-phase-10.sh`;
DB-gated legs require `HIKRAD_TEST_DB_URL`/`HIKRAD_TEST_REDIS_URL`,
self-skip otherwise — same convention as every prior phase):

1. **No schema change** — no new migration file added by this phase;
   `go build`/`go vet` clean.
2. **Permission-gating test (FR-89.1, AC sketch)** — a manager holding only
   `subscribers.view` + `reports.view` + `renew` (the builtin **agent**
   role's actual permission set — note this is *not* the source brief's
   original illustrative example, which assumed agents lack `reports.view`;
   they don't, per `internal/auth/permissions.go`'s `rolePermissions[RoleAgent]`,
   so the gate test uses the real set) requesting every C1 widget id via
   `?widgets=` gets back `subs.*`, `revenue_today_iqd`, and `my_balance`
   (self-view, no permission needed) — but never `online_now`,
   `radius_rps`, `pipeline`, `nas_cards`, `alerts_feed`, or
   `pending_payment_tickets` (needs `live.view`/`monitoring.view`/`nas.view`/`payment_tickets.verify`,
   none of which the agent role holds).
3. **Forbidden widget absent, not erroring (FR-89.3)** — a request naming a
   forbidden id alongside permitted ones returns `200` with the forbidden
   key missing and the permitted keys present; an unknown/typo'd id is
   dropped the same way, never a `400`/`422`/`403`.
4. **Cross-device layout (mirrors v2-6 AC-84a)** — manager A `PUT`s a
   `dashboard_layout`; an independent, later `GET /me/preferences` (second
   login session, no shared client state) returns the same layout.
5. **Default-equals-today snapshot (FR-90.1)** — a manager with no stored
   layout: the widget set/order the C2 default-resolution rule produces is
   asserted to exactly match, field-for-field, what `GET /api/v1/dashboard`
   (no `?widgets=`, the pre-existing full aggregate) returns for the same
   manager — proving "unset behaves exactly like v1 today" is not just
   claimed but true.
6. **Reset-to-default** — a manager who saved a custom layout, then `PUT`s
   `/me/preferences` with `dashboard_layout` omitted, reads back the C2
   default on the next `GET` — same mechanism as gate item 5, proven
   round-trip.
7. **Validation** — an unknown widget id or an invalid `size` in
   `dashboard_layout` `422`s naming the offending path; nothing is written
   on a rejected `PUT` (mirrors v2-6 gate item 6's pattern exactly).
8. **Backward compatibility** — `GET /api/v1/dashboard` with no `?widgets=`
   is byte-for-byte unchanged in shape from the pre-phase response (a
   snapshot/golden-value regression test), so nothing that ever calls the
   unparametrized endpoint breaks.
9. **Phone-first single column survives** — a panel component test asserts
   the dashboard grid renders one column at the mobile breakpoint regardless
   of a stored layout containing `2x` widgets (mirrors the existing
   `rtl-smoke.test.tsx` assertion style).
10. **Full regression** — the pre-existing `internal/monitorsvc` DB-gated
    dashboard/health/alert-rule suite passes unchanged (this phase adds no
    column and no behavior change to the unparametrized call).
11. **Panel/portal** — build + lint + vitest green; the dashboard edit mode
    exists and round-trips through `GET/PUT /me/preferences`; every widget
    label and the edit-mode UI strings are trilingual; `i18n:check` green
    (0 hardcoded strings, 0 missing keys across en/ar/ku).
12. **Docs accuracy** — PRD/sub-PRD 03 reflect FR-89/FR-90 (done in this
    phase's own Step-1 docs commit, before this file); `docs/ops/known-issues.md`
    carries any bug found while building, dated the day it's found.

Human/hardware legs: **none** — this phase has no router/device/hardware
dependency, same posture as v2-4/v2-9/v2-6.

## Open implementation questions for whoever builds this (not blocking, but worth a decision-log entry when resolved)

- **`internal/monitorsvc` has no pre-existing DB-gated HTTP-endpoint test
  harness.** Every existing test in that package (quiet hours, cooldown,
  dispatcher, SNMP encoding, downtime detection, WhatsApp templates) is
  unit-level — there is no `db_test.go` in the style `internal/auth`,
  `internal/subscribers`, and `internal/importer` already have (router +
  Postgres + Redis over `httptest.NewServer`). Building this phase's gate
  items 2/3/5/8 requires creating that harness first, following the exact
  same `env`/`call`/`setup` pattern those three packages already use — not a
  blocker, just budget the time for it; it did not exist to reuse.
- **The source brief's acceptance sketch used an inaccurate assumption about
  the agent role** ("Hassan sees my-balance and subscribers widgets, does
  not see revenue-today (no `reports.view`)") — the real builtin agent role
  already holds `reports.view` (`internal/auth/permissions.go`). Gate item 2
  above uses the *actual* permission set rather than silently preserving the
  brief's incorrect example. Worth a one-line PRD/decision-log note if this
  surprises the owner — it means an agent's dashboard **will** show
  revenue-today by default once this ships, which is arguably correct given
  Hassan's "balance-driven" persona description, but is a behavior change
  worth calling out explicitly rather than discovering silently.
- **Drag-and-drop library choice** is left to the implementer — the contract
  (C5) specifies the interaction, not the mechanism. A dependency-free
  up/down-button reorder is the simplest path and satisfies every gate item;
  a real drag library is a nice-to-have, not required by any gate item.
- **`my-balance`'s exact response shape** (single currency vs. the full
  per-currency array v2-4's multi-currency balances already support) is left
  for the implementer to resolve against the existing `GET
  /managers/{id}/balances` shape — reuse it verbatim rather than inventing a
  new one, since C1 already frames this widget as reading that same data.
