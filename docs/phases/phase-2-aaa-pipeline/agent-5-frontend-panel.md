# Phase 2 — Agent 5 (Frontend Panel): NAS screens, users, global search, Live Sessions

> Owns UI halves of FR-2, FR-3, FR-4, FR-13/14/16 (screens), FR-31 (table); NFR-5 in practice. Depends on contracts in [00-phase.md](00-phase.md) (C2 auth shapes, C5-via-live-API, C6, C7); parallel with Agents 1–4.

## Mission & context
First real operator features in the panel: NAS management with the copy-paste wizard (persona Ali), the user list/detail that front-desk Sara lives on, keyboard-first global search, and the flagship **Live Sessions** table (≤ 2 s updates, filter, disconnect). All strings via `@hikrad/shared` i18n (en+ar complete, ku keys); everything responsive to phone width. Detail sources: sub-PRDs [04](../../prd/04-subscribers-profiles.md) §5, [02](../../prd/02-radius-nas-aaa.md) §5, [03](../../prd/03-lossless-accounting-live-monitoring.md) §5.

## File ownership
- **Exclusive:** `frontend/panel/**`.
- **Read-only:** `frontend/shared/**`. **Forbidden:** `frontend/portal/**`, backend/deploy paths.

## Tasks
1. Swap auth to the real endpoints (shapes unchanged per phase C2 — verify refresh rotation + forced-logout on revocation works against A's implementation).
2. **Global search** (FR-2): top-bar component, `/` keyboard shortcut, debounced `GET /api/v1/search`, grouped results, Enter opens top hit → user page. Skeleton states; Arabic input tested.
3. **User list** (FR-1/4): paginated/filterable table (status, profile, owner, expiring-in), saved filter chips, column chooser; bulk-action bar (enable/disable, change profile, extend expiry, move owner, export CSV) driving D's async bulk endpoint with progress + per-row failure display.
4. **User detail page** (FR-3, key flow 2 step 2): status banner (online/offline live flag, expiry countdown, remaining quota), live session widget (SSE-driven), usage graphs daily/monthly (C7-C data; charts LTR inside RTL), session history table (incl. stale/reaped badges + last-disconnect reason), audit trail (A's endpoint), edit form, **Reset MAC** one-click with confirm, disable-with-CoA-disconnect flow. Renew button present but disabled with "Phase 3" tooltip flag behind a feature switch.
5. **NAS screens** (FR-13/14): card list showing enabled state + last-seen (health colors arrive Phase 3 — leave the slot), create/edit wizard with type-specific steps, ROS 6/7 tabbed snippet block with copy button + "Test" (seen-since-created endpoint), delete-with-live-sessions confirm.
6. **IP pools** (FR-16): list with utilization bars + 90% warning state, CRUD forms, assignment to profiles/NAS.
7. **Live Sessions** (FR-31): SSE table (snapshot + incremental updates, no page refresh), columns per sub-PRD 03 FR-31, filters (NAS/profile/manager/search), stale rows dimmed with tooltip, per-row actions: Disconnect (permission-aware, confirm, result toast incl. CoA NAK/timeout error surface) and Open user. Virtualized rows (2k sessions must scroll smoothly).
8. Permission-aware UI: hide/disable actions the manager lacks (`disconnect`, `export`, edit verbs) based on the login response's permission set.

Edge cases: SSE reconnect with snapshot re-sync (no ghost rows); clock-skew-safe uptime rendering; empty states (fresh install: "waiting for first accounting packet" with link to NAS wizard); RTL with LTR-islands (MAC/IP/username) everywhere; bulk on 800 rows must not freeze the tab.

## Contracts consumed/exposed
- **Consumes:** C7-D subscribers/profiles/search/bulk, C7-B nas/pools/snippet, C7-C usage/sessions, C6 SSE, A's auth/audit endpoints — all frozen in the phase brief.
- **Exposes:** the screen structure Phase 3 (same role) extends with dashboard/billing/manager UIs.

## Definition of done
- Gate item 5 passes end to end; gate item 3's "visible ≤ 2 s" measured from harness Start to row render.
- Component tests: search shortcut/debounce, bulk failure rendering, SSE reducer (upsert/remove/stale), permission gating, MAC-reset flow.
- `i18n:check` green (en+ar complete; ku keys present); phone-width manual pass documented with screenshots in the PR.

## Handoff
Phase 3 (same role) receives working operational screens to hang dashboard/billing/alerts on; the Renew button slot activates when D ships FR-19.
