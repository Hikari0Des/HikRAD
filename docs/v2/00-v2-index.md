# HikRAD — v2 Backlog Index

> Established 2026-07-11 (master PRD Decision 24). This directory holds the features **deliberately deferred to v2** because they either rework code frozen by completed phases (1–2) or depend on external accounts that don't exist yet. Each file is a self-contained mini-PRD **plus a ready-to-paste AI kickoff prompt** — start a v2 feature by pasting its prompt into a fresh Claude Code session in this repo.

## Rules for v2 work

1. **v2 starts only after the v1 pilot gate** (success metric M1: pilot ISP 30 days in production). Don't interleave v2 features with v1 phases — they intentionally touch code v1 phases treat as frozen.
2. Every v2 feature still flows through the standard pipeline: master-PRD amendment (new FR numbers continue from FR-61; new Decision rows) → owning sub-PRD update → phase/task planning with frozen contracts and fresh migration ranges → parallel agents → integration gate. The kickoff prompts below encode this.
3. v2 migration number range: **05xx** (0500+), partitioned per feature file to stay clear of v1's 0001–04xx.
4. The verification baseline for v2 is [docs/verification-phases-1-2.md](../verification-phases-1-2.md): Phases 1–2 are confirmed as-documented; v2 features that touch that code must update the docs they invalidate in the same effort.

## v2 features

| # | File | Feature | Why v2, not v1 | Est. size |
|---|---|---|---|---|
| 01 | [01-hotspot-management.md](01-hotspot-management.md) | **Hotspot management**: hotspot-only subscriber accounts (`service_type`), full subscriber details for hotspot users, multiple Hotspot **and** PPPoE servers on one router (multi-service NAS) | Reworks the Phase-2-built subscriber model (`allow_hotspot` boolean → `service_type`), the policy engine's FR-58 branch, and the one-NAS-one-type model — all frozen since Phase 2 (Decision 24) | 1 phase, agents B+D+E |
| 02 | [02-payment-gateways-asiahawala-areeba.md](02-payment-gateways-asiahawala-areeba.md) | **AsiaHawala (Asiacell) + Areeba gateway adapters** | Pure adapter work behind the Phase-4 `PaymentGateway` interface, but blocked on merchant accounts/API docs that don't exist yet; zero core changes | days per adapter, agent D only |

## Candidate v2+ items (not yet briefed — from the master PRD's P7+ backlog)

Prepaid card designer · reseller/sub-manager tree · TWA store wrapper · public API docs · non-MikroTik vendor certification · application-aware QoS (Decision 20 marked post-v1 at most). Brief these the same way when chosen: one file here, mini-PRD + kickoff prompt.
