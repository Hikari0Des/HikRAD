# Phase 5 — Agent 3 (Backend Business): reports APIs, SAS4 CSV import, scheduled digests

> Owns FR-45, FR-46, FR-47, FR-6, FR-48 (digest part). Depends on contracts in [00-phase.md](00-phase.md) (C1-D, C2, C3); parallel with Agents 1, 2, 4.

## Mission & context
The last business surfaces: reports that let Omar steer the business and settle Hassan's collections — every figure an aggregation of ledger entries so reports and balances can never disagree — plus the SAS4 CSV import wizard that lowers switching cost (named mitigation of the competition risk), and the daily digest generalization. Read-only over data earlier phases own. Detail sources: sub-PRDs [08-reports](../../prd/08-reports.md), [04](../../prd/04-subscribers-profiles.md) FR-6.

## File ownership
- **Exclusive:** `backend/internal/reports/**`, `backend/internal/importer/**`, `backend/migrations/0400_*.sql`–`0409_*.sql`.
- **Read-only:** ledger/payments (billing), subscribers/profiles, `usage_daily` + sessions (C), settings. **Forbidden:** writes to any other module's tables (importer creates subscribers **via the subscribers service API**, not SQL), `frontend/**`, `deploy/**`.

## Tasks
1. Migrations 0400–0409 per phase C1-D: import batches/rows (+ `report_schedules` only if the FR-48 stretch lands).
2. **Financial reports** per C2 (FR-45): revenue grouped by day/month/manager/profile/method over signed ledger sums (refunds negative — totals reconcile by construction); settlement report (opening/topups/renewals/refunds/closing; closing ≡ live balance at `to=now`); scoped-manager rules (own data only; comparison views need unscoped).
3. **Subscriber reports** per C2 (FR-46): new/expired/expiring-N/by-profile/inactive-N-days; **expiring shares one query definition with C's digest rule** (expose it as a service C calls); inactive = no session overlapping the window (the frozen definition from sub-PRD 08's open question — document it); rows carry ids for worklist links.
4. **Usage reports** (FR-47): top consumers, per-NAS totals — `usage_daily` rollups only, response under the 1.5 s page budget at 5k scale (verified in C's perf suite — provide the fixtures).
5. **Export & print** (FR-46.2): streaming CSV under `export` permission; print-view data variants (E renders); all filters URL-stable.
6. **CSV importer** per C3 (FR-6): upload (UTF-8 + CP1256 detection), column-mapping with SAS4 preset, dry-run per-row validation (duplicates, phone format, unknown profile with create-option, bad expiry), execute idempotently via the subscribers API (audit + policy invalidation come free), summary report. Batch sizes to 10k rows without timeout (async job reusing the Phase-2 bulk-job pattern).
7. **Daily digest data** (FR-48 core): the business-digest composition endpoint C's scheduler consumes (new users, renewals, revenue, expiring soon — one call, localized-key payload). Arbitrary saved-report schedules = stretch only.

Edge cases: revenue report spanning a timezone-DST-free but month-boundary-sensitive range (Asia/Baghdad month edges — reuse C's boundary math convention); settlement for a manager with zero activity (clean zeros, not 404); import row with duplicate username *within the file*; dry-run → data changed before execute (re-validate on execute, skip newly-invalid with report); report on empty install returns friendly empty shapes.

## Contracts consumed/exposed
- **Consumes:** ledger/views (own module, read), subscriber service API (importer), `usage_daily` (C), `ScopeFilter`/`export` permission (A).
- **Exposes:** C2 report APIs + print-view data (E), expiring-query service (C's digest), C3 import API (E's wizard UI), digest composition endpoint (C).

## Definition of done
- Gate items 4 and 5 pass: reconciliation property tests (revenue ≡ ledger sums under randomized fixtures; settlement closing ≡ balance), digest/report single-definition test, import dry-run/execute/idempotence with planted errors incl. CP1256 Arabic.
- Perf fixtures delivered to C's suite; heavy endpoints within budget.
- Every endpoint scoped + permission-checked (A's lint green).

## Handoff
E builds the report/import UIs on these frozen shapes this phase; v1 ships. Post-v1: FR-48 full scheduling, promo pricing (FR-26) — the ledger/report base needs no rework for either.
