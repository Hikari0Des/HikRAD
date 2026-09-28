# HikRAD — Sub-PRD 08: Reports

> Derived from [docs/PRD.md](../PRD.md) v1.0 on 2026-07-08; updated 2026-07-18 for master Decision 42 — v2 phase 11 (instance branding): FR-46.2's print-header elaboration now names its identity source explicitly; no new FR (this module consumes [01](01-platform-install-licensing.md)'s FR-91 identity endpoint, same "consumer, not owner" pattern it already follows for every other domain's data). Owns: FR-45, FR-46, FR-47, FR-48
> Depends on: [05-billing-payments-vouchers](05-billing-payments-vouchers.md) (ledger — the source of all financial figures), [04-subscribers-profiles](04-subscribers-profiles.md) (subscriber states/expiry), [03-lossless-accounting-live-monitoring](03-lossless-accounting-live-monitoring.md) (usage rollups, alert engine for scheduled digests), [06-managers-roles-security](06-managers-roles-security.md) (permissions, ownership scoping, `export` right) · Depended on by: none (leaf module).

## 1. Scope & context

Read-only analytical views over data other modules own: financial reports for **Omar** (revenue steering) and agent settlement (**Hassan**'s end-of-week report — the other half of [05](05-billing-payments-vouchers.md) FR-20.4), subscriber lifecycle reports, usage reports, and scheduled report emails. Reports never compute money independently — every financial figure is an aggregation of ledger entries, so reports and balances can never disagree. Built in P6 (v1 gate), after all source data exists.

## 2. Owned requirements — elaborated

### FR-45 (M) — Financial reports
**Master:** Revenue by day/month, by manager/agent, by profile, by payment method; agent collection/settlement report.

*Elaboration:*
- **FR-45.1** — Revenue views: time series (day/month, date-range picker, ISP timezone from settings) with group-by manager/agent, profile, payment method/source (manual, voucher, each gateway). Refunds/adjustments shown as negative amounts, so totals always reconcile with the ledger.
- **FR-45.2** — Agent collection/settlement report: per agent, per period — opening balance, top-ups, renewals performed (count + amount), refunds, closing balance; printable (the settlement document Hassan and Omar sign off on, per Hassan's user story).
- **FR-45.3** — Scoped managers see only their own financial reports ([06](06-managers-roles-security.md) FR-27.2); the by-manager comparison view requires an unscoped account.

### FR-46 (M) — Subscriber reports
**Master:** New/expired/expiring, actives by profile, inactive-N-days; all reports filterable, exportable (CSV; printable view).

*Elaboration:*
- **FR-46.1** — Views: new subscribers per period; expired per period; expiring-in-N-days (N selectable — same query the FR-36 digest alert uses, one definition); actives by profile (distribution); inactive-N-days (no session in N days per [03](03-lossless-accounting-live-monitoring.md) session data — churn-risk list).
- **FR-46.2** — Every report (this FR and FR-45/47): filter bar, CSV export (gated by the `export` permission), and a print-clean view (no chrome, ISP header, RTL-correct per NFR-6). *(v2 phase 11, Decision 42):* the ISP header's name/logo come from [01](01-platform-install-licensing.md) FR-91's public `GET /api/v1/branding` endpoint — the same source every other print/login/manifest surface reads, fixed this phase after being found silently non-functional (see `docs/ops/known-issues.md`). This report header was already wired to that endpoint pre-phase (`PrintHeader.tsx`); the fix is entirely on the endpoint side, no report-side code change.
- **FR-46.3** — Rows link to the user page ([04](04-subscribers-profiles.md) FR-3) — reports are worklists, not just documents.

### FR-47 (S) — Usage reports
**Master:** Top consumers, per-NAS totals per period.

*Elaboration:* top-N subscribers by download/upload per period; per-NAS traffic totals per period — both straight queries over `usage_daily` rollups ([03](03-lossless-accounting-live-monitoring.md) FR-33 API), never raw hypertable scans at panel-load budgets (NFR-1 page < 1.5 s).

### FR-48 (C) — Scheduled report emails
**Master:** Daily digest to Omar.

*Elaboration (Could):* schedule = report + recipients + cadence, delivered via the alert engine's channel/quiet-hour infrastructure ([03](03-lossless-accounting-live-monitoring.md) FR-36.3 already carries the daily business digest — new users, renewals, revenue, expiring soon; this FR generalizes it to arbitrary saved reports). Email via SMTP settings; failure to send never affects anything else (NFR-7).

## 3. Acceptance criteria

- **AC-45a** — Given any date range, then the revenue report total equals the signed sum of ledger entries in that range exactly (verified against [05](05-billing-payments-vouchers.md) FR-24 data in tests).
- **AC-45b** — Given Hassan's settlement report for last week, then opening balance + top-ups − renewals − refunds = closing balance, and closing balance matches his live balance if run to now.
- **AC-46a** — Given "expiring in 7 days", then the report's row set is identical to the FR-36 digest's list for the same N and moment.
- **AC-46b** — Given a scoped manager runs any report, then only their owned users' rows/figures appear (API-level check with foreign data present).
- **AC-46c** — Given any report, then CSV export matches the filtered rows and the print view renders RTL-correct in Arabic with the ISP header.
- **AC-47a** — Given 12 months of usage data at 5k-subscriber scale, then top-consumers renders in < 1.5 s (NFR-1) using rollups.

## 4. Data & interfaces

**Owned entities:** none primary — this module owns only report definitions/saved schedules (`report_schedules` if FR-48 ships). All figures come from other modules' tables/APIs.

**Exposes:** `GET /api/v1/reports/{financial|subscribers|usage}/…` with filter params; `GET …/export.csv`; print-view routes. All apply `Require`/`ScopeFilter` from [06](06-managers-roles-security.md).

**Consumes:** `ledger_transactions` aggregates ([05](05-billing-payments-vouchers.md)); subscriber state/expiry queries ([04](04-subscribers-profiles.md)); `usage_daily` + session recency ([03](03-lossless-accounting-live-monitoring.md)); SMTP + timezone/currency settings ([01](01-platform-install-licensing.md) FR-53); permission/scoping middleware ([06](06-managers-roles-security.md)).

## 5. UX notes

Reports are answer-shaped, not data-dump-shaped: each opens with its headline number(s) then the table; charts follow dataviz conventions already used by the dashboard ([03](03-lossless-accounting-live-monitoring.md) FR-32) for consistency. Filter state encoded in the URL (shareable). Print view: A4, ISP branding header, generated-at timestamp, page numbers. Localized incl. Eastern-Arabic numerals option; tables mirror correctly in RTL while numeric columns stay readable (NFR-6 rules from [07](07-subscriber-portal-pwa.md)).

## 6. Out of scope

- The dashboard (live operational view) → [03](03-lossless-accounting-live-monitoring.md) FR-32 — reports are historical/analytical.
- Ledger correctness, balances → [05](05-billing-payments-vouchers.md); alert digests' delivery machinery → [03](03-lossless-accounting-live-monitoring.md).
- **Deferred by master:** nothing report-specific is deferred; postpaid invoicing (non-goal) would be a reporting concern but is out of v1 entirely.

## 7. Risks & open questions (owned)

- *(No master risks or open questions are owned here.)*
- **NEW:** "inactive-N-days" needs a precise definition (no *session* vs. no *traffic*) — propose "no accounting-visible session overlapping the window" and confirm with the pilot ISP.
- **NEW:** settlement report period boundaries (week start, cut-off time) must match how Iraqi ISPs actually settle with agents — confirm during pilot onboarding; make week-start a locale setting if needed ([01](01-platform-install-licensing.md) FR-53).
