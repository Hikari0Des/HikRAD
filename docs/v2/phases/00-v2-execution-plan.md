# HikRAD v2 — Sequential Execution Plan

> Established 2026-07-16 by owner decision. **v2 is executed by ONE agent at a time, phase by phase — no parallel agent teams** (this supersedes the v1 multi-agent model of `docs/phases/00-team.md` for all v2+ work; the v1 path-ownership table survives only as a map of who built what). Each phase is started by pasting the kickoff prompt from its feature brief into a fresh Claude Code session; the session amends the PRDs, writes this folder's `phase-v2-N-*/00-phase.md` (frozen contracts + integration gate), waits for owner confirmation, then implements everything itself, sequentially, committing in reviewable chunks and pushing with clear messages.

## Standing rules (all v2 phases)

1. **Solo + sequential.** One session, no subagent teams. Order work inside a phase by dependency (schema → backend → RADIUS → UI), not by v1 agent roles.
2. **Docs stay true.** Every doc a change invalidates (PRD, sub-PRDs, docs/ops/*, CLAUDE.md) is updated in the same phase. The gate includes a docs-accuracy check.
3. **Bugs get recorded.** Any bug found while building — fixed or not — gets a row in `docs/ops/known-issues.md` (root cause, fix, commit) so later AI sessions never ghost-guess.
4. **Migrations** (amended 2026-07-17 after a near-miss, see known-issues): numbers form **one linear sequence** — golang-migrate applies only versions above the DB's current one, so every new migration (maintenance or phase) takes **the next free number above the repo's current maximum**. The per-phase ranges below are *budgets* for how many numbers a phase may consume; a range the sequence has already passed is dead (that's why the 2026-07-17 maintenance migration is 0505, inside v2-1's unused tail, not 0413).
5. **Frozen contracts** work exactly as in v1: the phase's `00-phase.md` freezes API shapes/schema/events before implementation; amendments are explicit, never silent.
6. **Gates**: each phase defines an integration gate; scriptable items live in `scripts/gate-v2-phase-N.sh`; the phase ends with a written `gate-result.md` in its folder, like v1 phases.

## Phase order

| Phase | Feature brief | Folder (created at kickoff) | Migrations | Why this order |
|---|---|---|---|---|
| v2-1 | [01-hotspot-management.md](../01-hotspot-management.md) — hotspot-only subscribers, multi-service NAS, **subscriber/profile→NAS scoping enforced at auth** (items 2/23/24) | `phase-v2-1-hotspot-management/` | 0500–0519 | Biggest daily-felt gap; other phases' UI (forms with NAS/server selectors) builds on its model |
| v2-2 | [03-nas-autosetup-config-manager.md](../03-nas-autosetup-config-manager.md) — form-driven auto-setup, read current router config, modify-or-create (item 10) | `phase-v2-2-autosetup-config-manager/` | 0520–0529 | Builds directly on v2-1's multi-service NAS model |
| v2-3 | [04-multi-currency.md](../04-multi-currency.md) — full IQD/USD/EUR billing (item 16) | `phase-v2-3-multi-currency/` | 0530–0539 | Money-core rework; isolated from the NAS work above |
| v2-3b | [09-cost-margin-and-reseller-pricing.md](../09-cost-margin-and-reseller-pricing.md) — plan cost price, overheads, margin on the ledger, per-reseller wholesale pricing (owner 2026-07-16) | `phase-v2-3b-cost-margin-pricing/` | 0540–0549 | **Immediately after v2-3, same money core.** Both change what a ledger row means; splitting them means migrating and re-freezing the ledger contract twice. A cost price without a currency is the same bug v2-3 exists to fix, so currency lands first. **Blocked at kickoff on one owner answer: do resellers nest?** (brief §6) |
| v2-4 | [06-preferences-and-account-fields.md](../06-preferences-and-account-fields.md) — per-manager preferences, subscriber email (items 18/22) | `phase-v2-4-preferences/` | 0550–0559 | Small, independent; wants v2-3's display-currency preference slot to exist |
| v2-5 | [07-one-click-updater.md](../07-one-click-updater.md) — panel-triggered host update (item 1) | `phase-v2-5-one-click-update/` | 0560–0569 | Ops surface; benefits from everything before it being stable |
| v2-6 | [05-closed-source-distribution.md](../05-closed-source-distribution.md) — signed image/bundle delivery, licensing hardening (item 17) | `phase-v2-6-closed-source/` | 0570–0579 | Changes the delivery pipeline the updater (v2-5) will then carry |
| v2-7 | [02-payment-gateways-asiahawala-areeba.md](../02-payment-gateways-asiahawala-areeba.md) — AsiaHawala + Areeba adapters | `phase-v2-7-gateways/` | 0580–0589 | Blocked on merchant accounts — slot it whenever they exist (can run any time after v2-3 for currency) |

Re-ordering is allowed (owner's call) — the only hard dependencies are: v2-2 after v2-1; **v2-3b immediately after v2-3** (same ledger); v2-7 after v2-3 if settlements are non-IQD; v2-5 before or with v2-6's registry work.

## How to start a phase

1. Open a fresh Claude Code session at the repo root.
2. Paste the **AI kickoff prompt** from the feature brief (§5 of each file).
3. The session stops after writing `00-phase.md` — review the frozen contracts and gate, then tell it to proceed.
4. At the end, expect: green gate script, `gate-result.md`, updated docs, commits pushed.
