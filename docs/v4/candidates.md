# v4 candidates — post-program backlog

> First created at v4-12 kickoff (the master plan's own scope-discipline rule: "if
> something is missing at this point, it goes in `docs/v4/candidates.md`, not into this
> phase"). v4-12 is the last v4 phase, so this file is now the release valve for
> anything found during it that is real but out of scope — not a phase brief, not
> committed to, just a recorded observation for whoever plans what comes after v4.

## From v4-12 (reports, dashboard, reconciliation)

- **License grace-state detection is broken** (`docs/ops/known-issues.md`,
  2026-08-18, re-confirmed live at v4-12's own program-gate check): a licence inside
  its expiry window reports `valid` instead of `grace`. Not a candidate feature — a
  real defect, unfixed across 5 phases now, directly touching program-gate item P6.
  Whoever plans post-v4 work should treat this as a priority fix, not backlog.
- **`ops-health-panel`'s throttled-sessions figure is an approximation** (most-recent
  CoA-outbox reason per open session), not v4-4's full multi-dimension limits
  evaluation re-derived sitewide. A real sitewide "who is currently throttled and why"
  view would need either a persisted flag maintained by the limits worker itself, or a
  batch adaptation of `subscribers/limitprogress.go`'s existing per-row query — either
  is a real, scoped piece of work, not a one-line fix.
- **Dashboard KPI tiles have two precision gaps**: `open-tickets` links to
  `status=open` only (the widget's own count includes `in_progress` too, and
  `SupportTicketsPage`'s status filter is single-select); `routers-reachable` links to
  the unfiltered `/nas` list (no reachability filter exists there to link to). Both are
  small, real frontend-filter additions to screens v4-12 doesn't otherwise own.
- **Cards-tab traffic-by-entity rows don't deep-link to a specific card group** —
  `CardsPage` has no route/query-param convention for opening one group directly (it's
  local component state only), so the new report links to the flat `/cards` list.
  Adding that convention is a `internal/cards`/`CardsPage` change, not a reports one.
- **Two known, real test bugs in `internal/billing/v4_9_db_test.go`**, found while
  fixing the journal sign bug but deliberately left open (different root causes, out of
  v4-12's domain): `TestUndo_ReversesRenewal_BothInAuditLog` scans a legitimately-NULL
  `expires_at` into a non-nullable destination; `TestInvoices_ScopeIsolation_AgentCannotTouchAnotherAgentsInvoice`
  expects 404 for cross-tenant invoice access where the endpoint returns 403 — an
  undecided design question (which status code is actually right here), not a
  mechanical bug. See `docs/ops/known-issues.md`, 2026-08-20 rows, for full detail.
- **The pre-v4-12 DB-gated test backlog outside `internal/billing`** (`internal/accounting`,
  `internal/notify`, `internal/monitorsvc`'s `TestSubscriberEvents_*`, `internal/radius`'s
  `TestCoAOutbox_HybridFallback_*`, `internal/support`) is still open — `docs/ops/known-issues.md`'s
  2026-08-20 row (narrowed by this phase to exclude the now-fixed `internal/billing`
  subset) has the full list and reproduction commands.
- **`go test ./...` run as one invocation races across packages against one shared
  external test database** — `TestDashboardDefaultEqualsToday` (and likely other
  cross-domain-read tests) can observe a different row count between two calls made
  moments apart because a sibling package's DB-gated suite inserted rows concurrently.
  Not a product bug; a test-infrastructure gap (no per-package test database
  isolation) worth closing if this repo's CI ever runs `go test ./...` as a single
  parallel invocation against one live database rather than isolated service
  containers per job, the way `.github/workflows/ci.yml` currently does.
