# HikRAD — Sub-PRD Index

> Generated from [docs/PRD.md](../PRD.md) v1.0 on 2026-07-08; updated 2026-07-09 for master v1.1 (FR-55–58, Decisions 16–20). The master PRD remains the source of truth; these files elaborate it, never contradict it. Split confirmed by the product owner on 2026-07-08.

## Files & ownership

| # | File | Scope (one line) | Owns FRs | Owns NFRs | Owns risks / open questions | Build order (master phase) |
|---|---|---|---|---|---|---|
| 01 | [01-platform-install-licensing.md](01-platform-install-licensing.md) | Docker installer, offline license, backup/updates, `/api/v1` skeleton, settings, optional Cloudflare tunnel | FR-49–53, FR-57 | NFR-3, NFR-7, NFR-8 | Scope-creep risk, license-cracking risk · OQ-3 (price) | 1st (P1, finishes P6) |
| 02 | [02-radius-nas-aaa.md](02-radius-nas-aaa.md) | FreeRADIUS↔Go auth path, NAS CRUD/wizard + auto-discovery/auto-setup, CoA, IP pools, vendor-neutral core | FR-13–18, FR-56 | NFR-1 | MikroTik ROS-quirks risk · OQ-2 (pilot ISP) | 2nd (P1–P2) |
| 03 | [03-lossless-accounting-live-monitoring.md](03-lossless-accounting-live-monitoring.md) | Lossless accounting pipeline, live sessions, usage graphs, dashboard, NAS/system health, alerts, WhatsApp subscriber messaging | FR-31–40, FR-55 | NFR-2 | Pipeline-complexity risk, SAS4-competition risk | 3rd (P2, alerts P4) |
| 04 | [04-subscribers-profiles.md](04-subscribers-profiles.md) | Subscriber CRUD/search/bulk/CSV-import, user page, profiles + expiry/quota behaviors, dual-service (PPPoE-on-Hotspot) rules | FR-1–12, FR-58 | NFR-5 | — | 4th (P3) |
| 05 | [05-billing-payments-vouchers.md](05-billing-payments-vouchers.md) | Renewals, immutable ledger, agent balances, receipts, vouchers, e-wallet gateway interface | FR-19–26 | — | E-wallet-availability risk · OQ-1 (gateway priority) | 5th (P3, gateways P5) |
| 06 | [06-managers-roles-security.md](06-managers-roles-security.md) | Manager accounts, granular permissions + scoping, 2FA, audit log, security posture | FR-27–30 | NFR-4 | — | 6th (P4; middleware needed from P1) |
| 07 | [07-subscriber-portal-pwa.md](07-subscriber-portal-pwa.md) | Subscriber portal, trilingual RTL localization, PWA packaging of portal + panel | FR-41–44, FR-54 | NFR-6 | RTL/trilingual-effort risk | 7th (P5; NFR-6 rules apply from P1) |
| 08 | [08-reports.md](08-reports.md) | Financial/subscriber/usage reports, agent settlement, scheduled digests | FR-45–48 | — | — | 8th (P6) |

## Coverage audit (mandatory check — passed)

- **FRs: 58/58 owned, each by exactly one sub-PRD.** FR-1…FR-12 → 04 · FR-13…FR-18 → 02 · FR-19…FR-26 → 05 · FR-27…FR-30 → 06 · FR-31…FR-40 → 03 · FR-41…FR-44 → 07 · FR-45…FR-48 → 08 · FR-49…FR-53 → 01 · FR-54 → 07 · FR-55 → 03 · FR-56 → 02 · FR-57 → 01 · FR-58 → 04. No gaps, no double ownership. (FR-58 splits like FR-5/9/10: 04 owns data + rules, 02 enforces at auth time.)
- **NFRs: 8/8 owned.** NFR-1 → 02 · NFR-2 → 03 · NFR-3 → 01 · NFR-4 → 06 · NFR-5 → 04 · NFR-6 → 07 · NFR-7 → 01 · NFR-8 → 01.
- **Master risks: 7/7 owned.** E-wallet → 05 · MikroTik quirks → 02 · Scope creep → 01 · Pipeline complexity → 03 · License cracking → 01 · RTL/trilingual → 07 · SAS4 competition → 03.
- **Open questions: 3/3 owned.** OQ-1 (gateway priority) → 05 · OQ-2 (pilot ISP) → 02 · OQ-3 (price point) → 01.

Cross-cutting NFRs are owned in one file and *applied* everywhere by reference: NFR-1 splits its numeric budgets (auth latency owned in 02; ingest/live-update implemented in 03; page-load inherited by all UI modules), NFR-5 (04) and NFR-6 (07) bind every panel/portal screen, NFR-4 (06) binds every endpoint.

## Dependency map

```
                    ┌──────────────────────────────────────────────┐
                    │ 01 platform-install-licensing (foundation)   │
                    └───────┬──────────────────────────────────────┘
            everything builds on 01 (Compose, /api/v1, settings)
                            │
        ┌───────────────────┼───────────────────┐
        ▼                   ▼                   ▼
  ┌───────────┐      ┌────────────┐      ┌────────────┐
  │ 02 radius │◄─────┤ 04 subs &  │      │ 06 managers│
  │ nas & aaa │ auth │  profiles  │◄─────┤ roles/sec  │ (permissions, scoping,
  └─────┬─────┘ reads└─────┬──────┘scope └─────┬──────┘  audit — used by all)
        │ acct feed, CoA   │ quota/graphs      │
        ▼                  ▼                   │
  ┌─────────────────────────────┐              │
  │ 03 lossless acct/live/mon   │◄─────────────┘
  └─────┬───────────────────────┘
        │ usage data, alerts          renewals/ledger
        ▼                                   │
  ┌────────────┐   voucher/payment APIs  ┌──▼─────────┐
  │ 08 reports │◄────────────────────────┤ 05 billing │
  └────────────┘   (also reads 03, 04)   └──────┬─────┘
                                                │ portal renew/pay
                                          ┌─────▼──────┐
                                          │ 07 portal  │ (+ NFR-6 locale rules
                                          │  & PWA     │  consumed by all UIs)
                                          └────────────┘
```

Circularity note: 02↔04 is intentional and clean — 04 owns subscriber/profile *data and rules*, 02 owns *auth-time enforcement* and reads 04's read-model; 04 calls 02's CoA/invalidation contract. Similarly 05↔06: 06 owns manager *identity/permissions*, 05 owns manager *money*.

## How to use these files

Each sub-PRD is designed to be the **only** document needed to build its domain: it restates its owned requirements from the master (original text vs. elaboration clearly separated), adds acceptance criteria, and pins the exact contracts it exposes to / consumes from its neighbors. Hand one file to one developer or one AI coding session. When a contract in section 4 of any file changes, update both sides in the same commit. If a sub-PRD ever seems to disagree with [the master PRD](../PRD.md), the master wins — fix the sub-PRD.

Recommended build order is the `#` numbering (it tracks the master's P1→P6 phasing); modules 06 (auth middleware) and 07 (localization rules) publish contracts that earlier-built modules consume, so stub those contracts in P1 even though their full modules land later.
