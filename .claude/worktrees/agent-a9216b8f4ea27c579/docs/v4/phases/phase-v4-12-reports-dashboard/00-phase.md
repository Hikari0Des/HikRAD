# Phase v4-12 — Reports, dashboard & program gate

> **FRs:** FR-173 … FR-176 · **Migrations:** 0634–0635 · **Depends on:** every prior v4 phase
> **Owner sub-PRD:** 08 (reports) + 03 (dashboard) · **This is the last v4 phase, by design.**

## 1. Intent

Two jobs. First, close the reporting gap: SAMM's reports answer the questions operators ask daily — who is using the most, how is traffic trending, what did the cards earn — and HikRAD's do not, in that shape. Second, run the **program gate**: the cross-phase acceptance that proves v4 as a whole, on a clean machine, before anything ships.

## 2. Scope

**In:** reports parity, dashboard parity, journal reconciliation, and the program gate.
**Not in:** new capability of any kind. If something is missing at this point it goes to `docs/v4/candidates.md`, not into this phase.

## 3. Work

### 3.1 FR-173 — Reports (0634)
- Date presets (7/14/30/90d) and explicit ranges, with a loading state that covers long queries honestly rather than appearing to hang.
- **Trend lines**: active subscribers, online sessions, expired subscribers.
- **Daily traffic**: stacked bars, upload vs download, with a tooltip giving both and the combined total.
- **Top-10 lists**: download and upload, for today and for the month — four lists.
- **Traffic by entity**, paginated and searchable, with **separate Users and Cards tabs** (today in MB, month in GB), rows linking to the subscriber or card group.
- Financial, subscriber and usage reports keep their existing shapes and gain journal reconciliation (§3.3).

`0634` adds the daily rollup indexes these queries need. Measure first: if the existing hypertable rollups already serve them inside the page-load budget, skip the migration and say so — an unused index is a maintenance cost.

### 3.2 FR-174 — Dashboard (0635)
Inside the existing permission-gated widget catalogue (FR-89/90 — this extends it, it does not replace it):
- **Clickable KPI tiles** — online now, active, expired, open tickets, routers reachable, unpaid invoices — each navigating to the correspondingly filtered list.
- **Pipeline health panel**: CoA queue depth, failed CoA in 24h (from v4-8), pending audit actions, sessions throttled (from v4-4), sessions snapshot, active plans.
- **Recent activity tables**: recent CoA outbox, recent admin actions, recent subscribers, router health — each with "view all".

Every new widget carries its permission string and is absent, not empty, when denied. `0635` extends the widget catalogue registration.

### 3.3 FR-175 — Reconciliation
**Every reported money figure ties to the journal (v4-9) exactly**, asserted by a test that walks each report and compares against a journal query. Not sampled, not eyeballed. This is the same discipline FR-40 already applies to the accounting pipeline's counters, extended to the books.

### 3.4 FR-176 — Program gate
Run the master plan §7 gate end to end and record it. This is the phase's real deliverable.

## 4. Frozen contracts

- Reports read the journal for money and the usage rollups for traffic — never a mix, never a cached total.
- New dashboard widgets register through the existing catalogue with a permission string; no widget bypasses it.
- `?widgets=` parametrisation behaviour is unchanged: an unpermitted id is silently dropped, never a 403 for the whole call.

## 5. Acceptance gate

### Phase items

| # | Item | Proof |
|---|---|---|
| 1 | Trend, daily traffic, four top-10s, traffic-by-entity | Each rendered against seeded data with known expected values |
| 2 | Users and Cards tabs separate | Card traffic never appears in the Users tab and vice versa |
| 3 | Reports reconcile to the journal | Reconciliation test green across every money report |
| 4 | KPI tiles navigate correctly | Each tile lands on the correctly filtered list |
| 5 | Pipeline health accurate | Panel figures match the underlying tables under load |
| 6 | Widget permissions | A denied widget is absent from the picker and the response |
| 7 | Page-load budget | Reports load inside NFR-1's 1.5s at the 5k-subscriber tier |
| 8 | Charts use tokens | v4-1 chart palette; distinguishable in both themes |
| 9 | Migrations linear | 0634–0635 clean, or documented as unnecessary |

### Program gate (master plan §7)

| # | Item |
|---|---|
| P1 | Clean-OS native install → real MikroTik authenticates PPPoE, **timed under 30 minutes** |
| P2 | RouterOS container install on a real 7.23+ device authenticates a hotspot subscriber |
| P3 | Upgrade from the last v3 release: data preserved, 0596→max clean on a restored realistic dataset, money book reconciles to pre-upgrade figures |
| P4 | Deliberately broken update rolls back autonomously with panel and updater killed |
| P5 | Release-manifest audit clean, signature verifies from a clean machine, canonical repo private |
| P6 | Licence lifecycle: issue → file-exchange activation → cap wall → white-label entitlement → grace never stops RADIUS |
| P7 | Contrast and token gates green; **human browser matrix** 360/768/1280 × light/dark × LTR/RTL |
| P8 | Limits matrix through the packet harness, including CoA fallback on a NACKing router |
| P9 | Portal auto-login: grants on trusted+unambiguous, **refuses on ambiguous and untrusted**, step-up works |
| P10 | Every report figure ties to the journal |

## 6. Commit plan

1. `feat(reports): trend lines, daily traffic bars, four top-10 lists (FR-173)`
2. `feat(reports): traffic by entity with separate Users and Cards tabs (FR-173, 0634)`
3. `test(reports): every money figure reconciles to the journal (FR-175)`
4. `feat(dashboard): clickable KPI tiles (FR-174)`
5. `feat(dashboard): pipeline health panel (FR-174, 0635)`
6. `feat(dashboard): recent-activity tables (FR-174)`
7. `docs: v4-12 gate result and the v4 program gate (FR-176)`

## 7. Kickoff prompt

```
Execute phase v4-12 (Reports, dashboard & program gate) of the HikRAD v4 program, solo and
sequentially, start to finish, in this repo. This is the LAST v4 phase, by design.

Read first:
  - docs/v4/00-v4-master-plan.md               (§7 is the program gate you must run)
  - docs/v4/phases/phase-v4-12-reports-dashboard/00-phase.md   (this brief)
  - docs/prd/08-reports.md
  - docs/v2/phases/phase-v2-10-custom-dashboards/  (the widget catalogue you EXTEND)
  - docs/v4/phases/phase-v4-9-accounting/00-phase.md  (the journal reports must tie to)

Deliver FR-173 through FR-176. Migrations 0634-0635 — and measure before you write them:
if the existing hypertable rollups already serve these queries inside the page-load budget,
skip the migration and say so. An unused index is a maintenance cost, not a free win.

Two jobs, and the second is the real deliverable.

Job one — reports and dashboard parity. Extend the existing permission-gated widget
catalogue (FR-89/90); do not replace it. Every new widget carries a permission string and
is ABSENT, not empty, when denied. The ?widgets= behaviour is unchanged: an unpermitted id
is silently dropped, never a 403 for the whole call.

FR-175 is the item that matters most here: EVERY reported money figure ties to the journal
exactly, asserted by a test that walks each report and compares against a journal query.
Not sampled. Not eyeballed. This is FR-40's discipline extended from the accounting
pipeline to the books, and it is what makes the reports trustworthy enough to bill from.

Job two — run the PROGRAM GATE in master plan §7, all ten items, on a clean machine, and
write the result. P1, P2, P3 and P7 need hardware, a real RouterOS device, a restored pilot
backup, and a human with a browser. They are declared here at kickoff.

Scope discipline: if something is missing at this point, it goes in docs/v4/candidates.md.
It does not get added to this phase. The program has a scope contract and this is the phase
most likely to break it.

If any program-gate item cannot pass, the correct outcome is a gate result that says so
plainly and names what is outstanding. A v4 marked GREEN with pending legs would repeat
the exact anti-pattern (A1) this entire program was written to end.

Finish with docs/history/gates/v4-12.md and the program gate result.
```
