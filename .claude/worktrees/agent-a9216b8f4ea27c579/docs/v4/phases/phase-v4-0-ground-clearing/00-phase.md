# Phase v4-0 — Ground clearing

> **FRs:** FR-114 · **Migrations:** none · **Depends on:** nothing · **Blocks:** every other v4 phase
> **Owner sub-PRD:** 01 (platform) · **Decisions committed at kickoff:** 60, 68

## 1. Why this runs first

Two reasons, one urgent. The urgent one: Decision 56 recorded that the canonical repository is **currently public on GitHub** — full source readable with no auth — which contradicts FR-83.1 and the entire closed-source premise the licensing model rests on. That is closed in this phase, before anything else.

The second: 126 planning documents, ~22 per-agent task files describing an execution model that was abandoned, 13 status files, 71 undifferentiated known-issues rows, a `CLAUDE.md` that is a wall of prose, and dead code from three retired features. Every later v4 phase pays a tax on that until it is cleared.

## 2. Scope

**In:** repository privacy, documentation consolidation, dead-code and dead-script deletion, `CLAUDE.md` rewrite, known-issues triage.
**Not in:** any behaviour change, any Docker removal (that is v4-2 — this phase only *inventories* it), any schema change.

## 3. Work

### 3.1 Repository privacy (Decision 60) — do this first, in its own commit
- Flip `Hikari0Des/HikRAD` to private. Confirm anonymous `git clone` and `raw.githubusercontent.com` both 404.
- Confirm `Hikari0Des/hikrad-releases` contains **only** the bootstrap installer and release assets — no source file of any extension, no `.git` history carrying source.
- Audit the public repo's existing history for any source that was ever pushed there. If found: the fix is rotation and a written note in `docs/history.md`, not a rewritten history that pretends otherwise.
- Rotate anything that was exposed while public: release-signing key, licence-signing key, any token in CI. **Assume exposure — do not reason about whether anyone looked.**

### 3.2 Documentation consolidation
Per master plan §5. Deletions, each its own commit:
- `docs/phases/*/status-agent-*.md` (13 files).
- `docs/phases/phase-*/agent-N-*.md` (~22 files).
- `docs/v2/*.md` briefs and `docs/v3/*.md` briefs, **after** their content is reduced into `docs/history.md`.
- **Keep every `gate-result.md`.** They are the evidence trail; they move to `docs/history/gates/` unchanged.

New `docs/history.md`: one row per shipped phase — phase id, date, FRs, migrations consumed, gate result link, one-sentence outcome. It replaces the narrative that currently lives in `CLAUDE.md`.

### 3.3 `CLAUDE.md` rewrite
Target under 150 lines: what the project is · document hierarchy · commands · architecture invariants (vendor neutrality, router ownership, append-only money, offline-first, lossless pipeline) · migration numbering rule · a pointer to `docs/history.md`. **No program history in this file.**

### 3.4 Known-issues triage
Split the 71 rows into **Open** and **Closed**. Every open row is assigned to the v4 phase that will fix it, or explicitly marked `wontfix` with a reason. An open row with no owning phase is a planning bug.

### 3.5 Dead code and scripts
Delete only what master plan §5 lists, one commit per unit, suite green either side:
- Retired-feature remnants: e-wallet leftovers, `allow_hotspot`, `nas.type`, `card_payments`, pre-0595 per-currency wallet code, `formatIQD`.
- `scripts/render-release-compose.sh`.
- Migrate the ~35 `Field` call sites lacking `htmlFor`/`id` onto `FormField` (associates by construction), then delete the unsafe path.

**Inventory only, delete in v4-2:** `deploy/compose.yml`, `deploy/docker/*`, `deploy/caddy/`.

### 3.6 Security finding to record now
`deploy/freeradius/certs/` contains **checked-in private keys** (`ca.key`, `server.key`, `client.key`, `*.p12`). Stock FreeRADIUS bootstrap certs, but a known private key on any EAP surface is indefensible. Open a known-issues row at security severity, assigned to **v4-3** (FR-127), which replaces them with install-time generation.

## 4. Frozen contracts

- `docs/history.md` row shape: `| phase | date | FRs | migrations | gate | outcome |`.
- `docs/ops/known-issues.md` gains a required `Status` (open/closed/wontfix) and `Fixed in` column. Every future row uses it.
- Gate results live at `docs/history/gates/<phase-id>.md` from now on.

## 5. Acceptance gate

| # | Item | Proof |
|---|---|---|
| 1 | Canonical repo is private | Anonymous `git clone` and a raw URL both fail; screenshot or curl output in the gate result |
| 2 | Releases repo is source-free | `git log --all --name-only` shows no source extension in any commit |
| 3 | Exposed keys rotated | New key fingerprints recorded; old ones documented as retired |
| 4 | Doc count reduced | `find docs -name '*.md' | wc -l` ≤ 30, down from 126 |
| 5 | Every gate result preserved | Count of `gate-result.md` before == count under `docs/history/gates/` after |
| 6 | `CLAUDE.md` under 150 lines | `wc -l CLAUDE.md` |
| 7 | Known-issues fully triaged | Zero rows without a Status; zero open rows without an owning phase |
| 8 | Suite green after every deletion | `make test` and `npm run build --workspaces` green on the final commit |
| 9 | No behaviour changed | `git diff --stat` on `backend/internal` shows only deletions and the `Field`→`FormField` migration |

## 6. Commit plan

1. `chore(repo): make canonical repository private (FR-114, Decision 60)`
2. `security(repo): rotate signing keys and CI tokens exposed while public (FR-114)`
3. `docs: add history.md consolidating v1–v3 phase outcomes (FR-114)`
4. `docs: remove per-agent task and status files (FR-114, Decision 68)`
5. `docs: collapse v2/v3 briefs, preserve gate results under docs/history/gates (FR-114)`
6. `docs: rewrite CLAUDE.md to the invariants and pointers only (FR-114)`
7. `docs: triage known-issues into open/closed with owning phases (FR-114)`
8. `refactor: migrate remaining Field call sites to FormField (FR-114)`
9. `refactor: delete retired-feature remnants (FR-114)` — one commit per remnant
10. `docs: v4-0 gate result (FR-114)`

## 7. Kickoff prompt

```
Execute phase v4-0 (Ground clearing) of the HikRAD v4 program, solo and sequentially,
start to finish, in this repo.

Read first, in order:
  - docs/v4/00-v4-master-plan.md          (program plan; §5 is the deletion inventory)
  - docs/v4/phases/phase-v4-0-ground-clearing/00-phase.md   (this phase's brief)
  - docs/ops/known-issues.md              (71 rows to triage)

Deliver FR-114 exactly as the brief specifies. Order matters:

1. Repository privacy FIRST, in its own commit. Decision 56 recorded that the canonical
   repo is currently PUBLIC on GitHub — close that, verify anonymously, then rotate every
   key and token that was exposed while it was public. Assume exposure; do not reason
   about whether anyone actually looked.
2. Then documentation consolidation: 126 .md files down to under 30. Build docs/history.md
   first, THEN delete the briefs it summarises. Keep every gate-result.md — move them to
   docs/history/gates/ unchanged. Delete the 13 status-agent files and the ~22 per-agent
   task files (the multi-agent model was abandoned by Decision 25).
3. Rewrite CLAUDE.md to under 150 lines: invariants and pointers, no program history.
4. Triage all 71 known-issues rows into open/closed/wontfix, and assign every open row to
   the v4 phase that will fix it. Add the new security row for the checked-in FreeRADIUS
   private keys under deploy/freeradius/certs/, assigned to v4-3.
5. Delete dead code and scripts, one commit per unit, with the suite green either side.
   Only what master-plan §5 lists — nothing deleted "while you're in there".

Hard constraints:
  - ZERO behaviour change. This phase deletes and reorganises; it does not fix or add.
  - Do NOT touch deploy/compose.yml, deploy/docker/, or deploy/caddy/ — those are v4-2's.
    Inventory them only.
  - No migrations in this phase.
  - Conventional Commits with (FR-114) in the body; follow the brief's commit plan.

Finish by writing docs/history/gates/v4-0.md with each of the 9 gate items and the exact
command output that proves it. If an item cannot pass, say so plainly in the gate result —
do not mark it pending and move on.
```
