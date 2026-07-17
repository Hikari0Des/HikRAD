# HikRAD v2 — Sequential Execution Plan

> Established 2026-07-16 by owner decision. **v2 is executed by ONE agent at a time, phase by phase — no parallel agent teams** (this supersedes the v1 multi-agent model of `docs/phases/00-team.md` for all v2+ work; the v1 path-ownership table survives only as a map of who built what). Each phase is started by pasting the kickoff prompt from its feature brief into a fresh Claude Code session; the session amends the PRDs, writes this folder's `phase-v2-N-*/00-phase.md` (frozen contracts + integration gate), waits for owner confirmation, then implements everything itself, sequentially, committing in reviewable chunks and pushing with clear messages.
>
> **Label correction (2026-07-17, PRD Decision 35):** every `v2-N` label below equals its source file's own number (`docs/v2/0N-*.md` → phase `v2-N`; there is no file `08`, so the sequence has a gap at `v2-8`). Earlier the same day the labels had drifted from the filenames (e.g. what is now `v2-3` was briefly called `v2-2`, and what is now `v2-2` was briefly called `v2-7`) — an artifact of planning discussions, not a deliberate scheme, and confusing enough that the owner caught it just by looking at the `docs/v2/` directory listing. **The table's row order is the build order and is unchanged by the correction** — only the label text changed. If you are reading an older commit, a phase-folder name, or a gate script whose number doesn't match this table, trust this table and `docs/PRD.md` Decision 35.

## Standing rules (all v2 phases)

1. **Solo + sequential.** One session, no subagent teams. Order work inside a phase by dependency (schema → backend → RADIUS → UI), not by v1 agent roles.
2. **Docs stay true.** Every doc a change invalidates (PRD, sub-PRDs, docs/ops/*, CLAUDE.md) is updated in the same phase. The gate includes a docs-accuracy check.
3. **Bugs get recorded.** Any bug found while building — fixed or not — gets a row in `docs/ops/known-issues.md` (root cause, fix, commit) so later AI sessions never ghost-guess.
4. **Migrations** (amended 2026-07-17 after a near-miss, see known-issues): numbers form **one linear sequence** — golang-migrate applies only versions above the DB's current one, so every new migration (maintenance or phase) takes **the next free number above the repo's current maximum**. The per-phase ranges below are *budgets* for how many numbers a phase may consume, not reservations that survive being passed. History: v1 phases used 0001–0411, v1.x maintenance 0412 then 0505+, v2 phases 0500+.
5. **Frozen contracts** work exactly as in v1: the phase's `00-phase.md` freezes API shapes/schema/events before implementation; amendments are explicit, never silent.
6. **Gates**: each phase defines an integration gate; scriptable items live in `scripts/gate-v2-phase-N.sh`; the phase ends with a written `gate-result.md` in its folder, like v1 phases.

## Phase order (build sequence — read top to bottom; `v2-N` is NOT this row order, see the label-correction note above)

| Build # | Phase | Feature brief | Folder (created at kickoff) | Migrations | Why this order |
|---|---|---|---|---|---|
| 1 | v2-1 | [01-hotspot-management.md](../01-hotspot-management.md) — hotspot-only subscribers, multi-service NAS, **subscriber/profile→NAS scoping enforced at auth** (items 2/23/24) | `phase-v2-1-hotspot-management/` | 0500–0519 | Biggest daily-felt gap; other phases' UI (forms with NAS/server selectors) builds on its model |
| 2 | v2-3 | [03-nas-autosetup-config-manager.md](../03-nas-autosetup-config-manager.md) — form-driven auto-setup, read current router config, modify-or-create (item 10) **+ Hotspot/PPPoE server management**: router-managed (adopt) vs system-managed (create/edit) servers (Decision 31) | `phase-v2-3-autosetup-config-manager/` | 0520–0529 | Builds directly on v2-1's multi-service NAS model; server management builds on this phase's own config read/write pipeline. **Complete** — gate GREEN 27/27, see its `gate-result.md` |
| 3 | v2-4 | [04-multi-currency.md](../04-multi-currency.md) — full IQD/USD/EUR billing (item 16) | `phase-v2-4-multi-currency/` | 0530–0538 | Money-core rework; isolated from the NAS work above. **Complete** — gate GREEN 10/10, see its `gate-result.md` |
| 4 | v2-9 | [09-cost-margin-and-reseller-pricing.md](../09-cost-margin-and-reseller-pricing.md) — plan cost price, overheads, margin on the ledger, per-reseller wholesale pricing (owner 2026-07-16) | `phase-v2-9-cost-margin-pricing/` | 0540–0549 | **Immediately after v2-4, same money core.** Both change what a ledger row means; splitting them means migrating and re-freezing the ledger contract twice. A cost price without a currency is the same bug v2-4 exists to fix, so currency lands first. **Blocked at kickoff on one owner answer: do resellers nest?** (brief §6) |
| 5 | v2-6 | [06-preferences-and-account-fields.md](../06-preferences-and-account-fields.md) — per-manager preferences, subscriber email (items 18/22) | `phase-v2-6-preferences/` | 0550–0559 | Small, independent; wants v2-4's display-currency preference slot to exist |
| 6 | v2-7 | [07-one-click-updater.md](../07-one-click-updater.md) — panel-triggered host update (item 1) | `phase-v2-7-one-click-update/` | 0560–0569 | Ops surface; benefits from everything before it being stable |
| 7 | v2-5 | [05-closed-source-distribution.md](../05-closed-source-distribution.md) — signed image/bundle delivery, licensing hardening (item 17) | `phase-v2-5-closed-source/` | 0570–0579 | Changes the delivery pipeline the updater (v2-7) will then carry |
| 8 | v2-2 | [02-manual-payment-providers.md](../02-manual-payment-providers.md) — named providers, per-manager receiving accounts, portal transfer-proof + attachments, 1-day provisional, owner-reviewed (Decision 30) | `phase-v2-2-manual-payments/` | 0580–0589 | Books money on approval — needs v2-4's currency and v2-9's wholesale/retail ledger semantics settled first, despite its low file number |
| 9 | v2-10 | [10-custom-dashboards.md](../10-custom-dashboards.md) — permission-gated widget catalog + per-manager layouts (ex-v3.2, Decision 32) | `phase-v2-10-custom-dashboards/` | 0590–0599 | Stores layouts in v2-6's `manager_preferences`; a pending-payments widget wants v2-2 to exist |
| 10 | v2-11 | [11-instance-branding.md](../11-instance-branding.md) — logo + instance name through logins/PWA/receipts/reports (ex-v3.3, Decision 32) | `phase-v2-11-instance-branding/` | 0600–0609 | Independent; placed late only to keep the money/NAS spine uninterrupted |
| 11 | v2-12 | [12-frontend-modernization.md](../12-frontend-modernization.md) — complete modern control set (dropdowns/fields/ticks…), responsive/overflow audit, polish (ex-v3.1 broadened, Decision 32) | `phase-v2-12-frontend-modernization/` | none expected | **Must run last** — every earlier phase adds UI this pass must cover, and nothing may un-modernize it afterwards |

> AsiaHawala + Areeba gateway adapters (was the original v2-2 slot before manual-payment-providers existed) withdrawn 2026-07-17 (PRD Decision 29) and **replaced by the manual-payment-providers phase above** (Decision 30). A real gateway integration, if ever wanted, is a fresh brief.

Re-ordering is allowed (owner's call) — the hard dependencies are: v2-3 after v2-1; **v2-9 immediately after v2-4** (same ledger); v2-7 before or with v2-5's registry work; v2-2 after v2-9; v2-10 after v2-6 (and ideally after v2-2); **v2-12 last, always**.

## How to start a phase

1. Open a fresh Claude Code session at the repo root.
2. Paste the **AI kickoff prompt** from the feature brief (§5 of each file).
3. The session stops after writing `00-phase.md` — review the frozen contracts and gate, then tell it to proceed.
4. At the end, expect: green gate script, `gate-result.md`, updated docs, commits pushed.
