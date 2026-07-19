# v3 Execution Plan

> Established 2026-07-19 (PRD Decision 47). Same execution model as v2 (`docs/v2/phases/00-v2-execution-plan.md`): **SOLO, sequential, single-agent** — one Claude Code session runs one phase start-to-finish. The standing kickoff protocol, gate discipline, and known-issues rule from the v2 plan apply verbatim; this file only fixes v3's order and v3-specific rules.

## Build order

1. **v3-1 — UI/UX modernization program** (`../01-ui-ux-modernization.md`, FR-97–99). Internally split into sequential sub-phases (count fixed at kickoff, recommend 5: foundation → subscribers+billing screens → NAS+monitoring screens → security/settings+account area → portal). Each sub-phase ends with the full frontend suite green and the app shippable.
2. **v3-2 — single-book IQD currency redesign** (`../02-currency-single-book.md`, FR-100–102). Independent of v3-1; runs second by owner priority. If an urgent business need reorders it first, that's a one-line owner decision — there is no technical dependency either way, but never run them interleaved.

## Standing rules (v3 deltas only — everything else inherits from the v2 plan)

1. **FR numbers** continue from FR-97. **Migrations**: linear next-free-number above the repo max (0592 at plan time); no ranges, no reservations.
2. **v3-1 is behavior-preserving by construction** — any endpoint/permission/i18n-semantics change it wants must be escalated to the owner as a brief amendment first. v3-2 is the opposite: it supersedes v2-4 clauses, and must list them explicitly in its brief.
3. **The v2-12 CI gates are floors**: `scripts/lint-no-native-controls.sh`, both apps' `layoutSmoke.test.tsx`, stylelint logical-properties, `npm run i18n:check` — a v3 phase that breaks any of them is red regardless of its own gate.
4. **v3-2's migration rehearsal is a gate item, not optional**: the conversion migration must be exercised against a copied realistic dataset (pilot backup restored to a throwaway DB — never the live stack, per the standing never-probe-production rule) with a written reconciliation before it ships.
5. Phase docs live at `docs/v3/phases/phase-v3-N-<slug>/00-phase.md` + `gate-result.md`, same shape as v2.
