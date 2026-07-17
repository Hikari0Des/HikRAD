# v2-06 — Per-manager preferences & subscriber contact fields

> Owner requests 2026-07-16 (items 18 + 22-remainder): managers get **their own** settings (not only global ones), and subscribers get an **email** field (name/phone self-edit already shipped in v1.1).

## 1. Problem

1. All settings in v1 are global (`platform.Settings` key-value). A manager's language and theme persist only in that browser's localStorage — nothing follows the account across devices, and nothing per-manager is configurable at all (landing page, table densities, notification preferences).
2. Subscribers have no email column anywhere — the portal can't collect it, receipts can't be mailed, and future notification channels have nothing to send to.

## 2. Requirements (draft — renumber as FR-6x at kickoff)

### FR-A — Per-manager preferences
- New `manager_preferences` table (manager_id PK, JSONB doc) with a typed, versioned schema: `language`, `theme (light|dark|system)`, `numerals`, `landing_page`, `table_page_size`, `notification_prefs` (which alert classes reach in-app/push for this manager).
- `GET/PUT /api/v1/me/preferences` (any authenticated manager, self only). Server value seeds the client on login; localStorage remains the offline/pre-login fallback and syncs up on change (same pattern as the portal's language persistence).
- Global settings stay the tenant defaults; a manager preference overrides only presentation-level values — never permissions, scoping, or money rules.

### FR-B — Subscriber email
- `subscribers.email` nullable text + basic format validation; end to end: panel form + list column (optional), portal Settings self-edit, CSV import mapping (SAS4 export carries email), audit-logged like phone.
- Email is contact data only in this phase — no mailing pipeline yet (that stays a candidate feature: receipt/alert email channel).

## 3. Impact map

| Touched | Built in | Change |
|---|---|---|
| `internal/auth` (or `platform`) | Phase 2 (A) | preferences store + /me/preferences routes |
| Panel shell (I18nProvider seed, theme store seed, UserMenu) | Phases 2–5 (E) | server-seeded preferences, sync-on-change |
| `subscribers` schema/CRUD/import | Phases 2/5 (D) | email column + validation + CSV mapping |
| Portal SettingsPage | Phase 4 (F) | email field |

## 4. Acceptance sketch

- Manager sets dark theme + Kurdish on desktop; signs in on a phone → same theme/language without touching localStorage.
- Preferences never leak across managers; a manager cannot PUT another's preferences (403).
- Subscriber sets their email in the portal; it shows in the panel form; a SAS4 CSV with an email column imports it; invalid formats 422 with a field error.

## 5. AI kickoff prompt (paste into a fresh Claude Code session at repo root)

```text
You are working in the HikRAD repo. v1 is complete; we are starting v2 phase 6: per-manager preferences + subscriber email. You work SOLO — no parallel agents; execute sequentially (backend prefs → panel seeding → subscriber email end-to-end), committing in reviewable chunks.

Read, in this order and nothing else yet: CLAUDE.md, docs/v2/phases/00-v2-execution-plan.md, docs/v2/06-preferences-and-account-fields.md, docs/prd/01-platform-security.md (settings section), docs/prd/04-subscribers-profiles.md, frontend/shared/src/ui/theme.ts, frontend/shared/src/i18n/I18nProvider.tsx.

Step 1 — Amend the docs (single commit): new FR rows + Decisions Log row in docs/PRD.md, update sub-PRDs 01 and 04, docs/prd/00-index.md.

Step 2 — Create docs/v2/phases/phase-v2-6-preferences/00-phase.md with frozen contracts (preferences JSONB schema + versioning, /me/preferences shapes, subscribers.email column + validation rule, CSV mapping key) and the integration gate (cross-device seed test, cross-manager 403, import mapping test; migration range 0550–0559). Scriptable gate items → scripts/gate-v2-phase-6.sh.

Step 3 — Stop and present the phase brief for my confirmation before writing feature code.

Constraints: preferences are presentation-only — never permissions/scoping/money; email is contact data only (no SMTP pipeline this phase). Panel/portal strings trilingual. Update every doc invalidated; record bugs in docs/ops/known-issues.md.
```
