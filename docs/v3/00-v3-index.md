# HikRAD — v3 Backlog Index

> Established 2026-07-19 (master PRD Decision 47), from the owner's 17-item post-v2 review. Same working form as [docs/v2](../v2/00-v2-index.md): each file is a self-contained mini-PRD **plus a ready-to-paste AI kickoff prompt** — start a v3 phase by pasting its prompt into a fresh Claude Code session in this repo.
>
> Historical note: an earlier, unrelated `docs/v3/` parking list existed briefly on 2026-07-17 and was merged back into v2 (Decision 32). This directory is a fresh start under Decision 47 — the two share nothing but the name.

## Rules for v3 work

1. **v3 starts only after v2 is fully shipped** — it is (all 12 phases GREEN as of 2026-07-19); the remaining precondition is that the 2026-07-19 v2.x maintenance pass (Decision 45: dropdown width, roles catalog, audit-log naming, quick toggles, manager profile fields, rate prefixes, remembered filters, instance default payment accounts, two new dashboard widgets) is deployed and stable at the pilot.
2. **Sequential, single-agent execution** — same as v2 (Decision 25 stands): one session, one phase, start to finish; no parallel teams. Order and standing rules: [phases/00-v3-execution-plan.md](phases/00-v3-execution-plan.md).
3. Every v3 feature flows through the standard pipeline: master-PRD amendment (new FR numbers continue from **FR-97**; new Decision rows) → owning sub-PRD update → a frozen-contract phase brief in `docs/v3/phases/phase-v3-N-*/00-phase.md` → implementation → integration gate with a written `gate-result.md`.
4. **Migration numbering**: one linear sequence, always the next free number above the repo's current maximum (0592 as of 2026-07-19). No ranges are reserved; the 2026-07-17 linear-sequence rule (known-issues row) is absolute.
5. Every bug found goes in [docs/ops/known-issues.md](../ops/known-issues.md); every doc a phase invalidates is updated in the same effort.

## v3 features

Build order is the file order (v3-1's design system must exist before any screen is redesigned against it; v3-2 is independent and could technically run first, but the owner's priority is the UI).

| Phase | File | Feature | Why v3, not v2.x maintenance | Est. size |
|---|---|---|---|---|
| v3-1 | [01-ui-ux-modernization.md](01-ui-ux-modernization.md) | **Full UI/UX modernization program** (owner items 1/2/4/6/11/15): a real design system (tokens, layout shell, navigation, table/filter/form/detail patterns) built **in place** in the existing React/Tailwind/Radix codebase, then a screen-by-screen overhaul of panel + portal — including the subscriber-filters redesign, form-field ordering, simpler dashboard customization, and a profile-centric account area | Touches every screen both apps have; behavior-preserving but structurally too large for a maintenance pass — and it must not be interleaved with feature work | 3–5 sequential sub-phases, very large |
| v3-2 | [02-currency-single-book.md](02-currency-single-book.md) | **Single-book IQD currency redesign** (owner item 12): one balance per manager, denominated in IQD; exchange rates become pure entry/display conversion; entry-time conversion stamped on every ledger row; upgrade converts existing foreign balances once | Reverses v2-4's per-`(manager, currency)` wallet model — a money-core rework with an irreversible data migration | 1 phase, large |

## Candidate v3+ items (not yet briefed)

Carried over unchanged from v2's candidate list: prepaid card designer · TWA store wrapper · public API docs · non-MikroTik vendor certification · application-aware QoS · receipt/alert email channel · portal OTP login for passwordless hotspot accounts (known-issues 2026-07-16 row). New owner requests land here — a new brief file or inside an unstarted phase's brief, never interleaved into a running phase.
