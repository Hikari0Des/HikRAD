# Phase 5 — Agent 3 (Backend Business) status

**Done.** `backend/internal/reports/**` (revenue/settlement/subscribers/usage + FR-48 digest) and
`backend/internal/importer/**` (SAS4 CSV wizard), migrations 0400-0401, mounted in modules.go.
Gate legs appended to `scripts/gate-phase-5.sh` (Agent 3 section) — all pass locally.

**Design calls:** revenue report sums `payments` (joined to ledger for manager/profile), matching
the pre-existing `revenue_daily` view's own "for reports FR-45" comment; settlement is a literal
`ledger_transactions` slice per FR-20.4, so `closing_iqd` ≡ live balance by construction regardless
of entry-type coverage. Importer self-dispatches each create through the real chi router (in-process,
`m.router.ServeHTTP`) onto `POST /api/v1/subscribers` — audit + policy invalidation come free, no
subscribers-table SQL. Upload is base64-in-JSON, not multipart: the frozen `enforceJSON` middleware
415s non-JSON POSTs and that file isn't mine to amend.

**Seam for C:** `reports.ExpiringSubscribers(ctx, db, days, scope)` is the one FR-36/FR-46.1 query
definition (test `TestExpiringReportMatchesDigestQuery` proves it matches the report endpoint);
`GET /internal/reports/digest` composes the FR-48 numeric+message-key payload. C's
`monitorsvc/conditions.go` (`digestSummary`, `digestPerSubscriber`) still has its own inline queries —
wiring those to call this seam is C's edit, out of my path ownership.

**Known limitation:** execute's self-dispatched calls forward the caller's 5-min access token; a
batch whose *wall-clock* runtime exceeds that (only plausible on a very slow DB at very large scale)
leaves remaining rows `pending` — safe, since execute is idempotent and a re-run finishes them.

**Unrelated, pre-existing:** `internal/portalapi` `TestPortalMeComposition` fails on this tree before
and after my changes (`days_left`-shaped assertion) — not touched by this phase, flagging for owner.
