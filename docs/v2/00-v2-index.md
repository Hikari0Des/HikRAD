# HikRAD — v2 Backlog Index

> Established 2026-07-11 (master PRD Decision 24); expanded 2026-07-16 from the owner's post-v1 review. This directory holds the features **deliberately deferred to v2** because they rework code frozen by completed v1 phases or depend on external accounts that don't exist yet. Each file is a self-contained mini-PRD **plus a ready-to-paste AI kickoff prompt** — start a v2 phase by pasting its prompt into a fresh Claude Code session in this repo.

## Rules for v2 work

1. **v2 starts only after the v1 pilot gate** (success metric M1: pilot ISP 30 days in production). Don't interleave v2 features with v1.x maintenance fixes — they intentionally touch code v1 treats as frozen.
2. **Sequential, single-agent execution** (owner decision 2026-07-16): one Claude Code session executes one phase at a time, start to finish — **no parallel agent teams**. The order, standing rules, and migration ranges live in [phases/00-v2-execution-plan.md](phases/00-v2-execution-plan.md).
3. Every v2 feature still flows through the standard pipeline: master-PRD amendment (new FR numbers continue from FR-61; new Decision rows) → owning sub-PRD update → a frozen-contract phase brief in `docs/v2/phases/phase-v2-N-*/00-phase.md` → implementation → integration gate with a written `gate-result.md`. The kickoff prompts encode this.
4. v2 migration ranges: **0500–0589**, partitioned per phase in the execution plan — but note the 2026-07-17 amendment there: migration numbers are one **linear sequence** (next free number above the current max, always); ranges are budgets, and a passed range is dead. v1.x maintenance used 0412 and then continues inside whatever tail the sequence has reached.
5. The verification baseline for v2 is [docs/verification-phases-1-2.md](../verification-phases-1-2.md) plus the v1 phase gate results. **v2 phases must update every doc they invalidate in the same effort**, and record every bug found in [docs/ops/known-issues.md](../ops/known-issues.md).

## v2 features (execution order — see the plan for rationale)

| Phase | File | Feature | Why v2, not v1.x | Est. size |
|---|---|---|---|---|
| v2-1 | [01-hotspot-management.md](01-hotspot-management.md) | **Hotspot management + NAS scoping**: hotspot-only subscriber accounts (`service_type`), multi-service NAS (multiple Hotspot + PPPoE servers per router), subscriber/profile→NAS assignment **enforced at auth**, router-pool fallback (owner items 2/13-adjacent/23/24) | Reworks the Phase-2 subscriber model, the FR-58 authorize branch, and the one-NAS-one-type model (Decision 24) | 1 phase, large |
| v2-2 | [03-nas-autosetup-config-manager.md](03-nas-autosetup-config-manager.md) | **Auto-setup config manager**: values form, read current router config, per-item keep/update/abort (owner item 10) | Extends the Phase-4 additive-only auto-setup contract (Decision 17) and the vendor plan layer | 1 phase, medium |
| v2-3 | [04-multi-currency.md](04-multi-currency.md) | **Full multi-currency billing** IQD/USD/EUR: per-transaction currency, per-currency balances, admin rate table (owner item 16, explicit owner choice over display-only) | Reworks the Phase-3 money core (implicit-IQD ledger/balances/receipts/reports) | 1 phase, large |
| v2-3b | [09-cost-margin-and-reseller-pricing.md](09-cost-margin-and-reseller-pricing.md) | **Cost, margin and reseller pricing**: per-plan buy price + fixed overheads, margin on the ledger, per-reseller wholesale prices (owner request 2026-07-16) | Same money core as v2-3 — a renewal stops being one debit at one price and becomes a retail charge + a wholesale debit + a cost stamp. Runs adjacent to v2-3 so the ledger is reworked once, not twice | 1 phase, large |
| v2-4 | [06-preferences-and-account-fields.md](06-preferences-and-account-fields.md) | **Per-manager preferences** (theme/language/landing/notifications, server-side) + **subscriber email** end-to-end (owner items 18/22) | Adds account-scoped settings storage v1 never had; email threads through import/panel/portal | 1 phase, small |
| v2-5 | [07-one-click-updater.md](07-one-click-updater.md) | **One-click update from the panel** via a host-side updater daemon (owner item 1; guided flow shipped in v1.1) | Privileged host agent + socket protocol — real attack surface, deliberately not rushed in v1.x | 1 phase, medium |
| v2-6 | [05-closed-source-distribution.md](05-closed-source-distribution.md) | **Closed-source distribution & licensing hardening**: signed image bundles, no source on customer servers, license-gated delivery (owner item 17) | Changes the delivery model install.sh/update were built on | 1 phase, medium |
| v2-7 | [02-payment-gateways-asiahawala-areeba.md](02-payment-gateways-asiahawala-areeba.md) | **AsiaHawala (Asiacell) + Areeba gateway adapters** | Pure adapter work behind the Phase-4 `PaymentGateway` interface, but blocked on merchant accounts/API docs that don't exist yet | days per adapter |

## Candidate v2+ items (not yet briefed — from the master PRD's P7+ backlog)

Prepaid card designer · TWA store wrapper · public API docs · non-MikroTik vendor certification · application-aware QoS (Decision 20 marked post-v1 at most) · receipt/alert **email channel** (once v2-4's email field exists). Brief these the same way when chosen: one file here, mini-PRD + kickoff prompt, slotted into the execution plan.

## v3 parking backlog

Items the owner reports **while v2 is in flight** that aren't v2 scope go to [docs/v3/00-v3-index.md](../v3/00-v3-index.md) (established 2026-07-17; three of its six items — NAS-uuid bug, manager removal, factory reset — were pulled forward into v1.x the same day at the owner's request, leaving the frontend modernization pass, per-manager dashboards, and instance branding). A v2 session must not interleave the rest uninvited — park, don't scope-creep. v3 reserves migrations 0600–0689.

**reseller/sub-manager tree** left this list on 2026-07-16: it is now the open blocker at the top of [09-cost-margin-and-reseller-pricing.md](09-cost-margin-and-reseller-pricing.md) §6, because whether resellers nest decides that phase's schema rather than being a separate feature.
