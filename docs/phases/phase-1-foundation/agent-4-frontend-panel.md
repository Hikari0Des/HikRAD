# Phase 1 — Agent 4 (Frontend Panel): panel scaffold, RTL shell, login

> Owns panel groundwork for NFR-5/NFR-6 (UI side); depends on contracts in [00-phase.md](00-phase.md) (C2, C7, C8, C9); parallel with Agents 1–3, 5.

## Mission & context
The HikRAD admin/manager panel is a React 18 + TS app used daily by low-technical ISP staff (persona Sara) and phone-first field agents (Hassan) — Arabic/Kurdish RTL first-class, every daily task ≤ 3 clicks (NFR-5), later installable as a PWA. This phase you scaffold the app: shell, routing, theming, API client, and a working login screen against the dev auth stub. Feature screens start Phase 2. Detail sources: sub-PRDs [04](../../prd/04-subscribers-profiles.md) §5, [07](../../prd/07-subscriber-portal-pwa.md) NFR-6.

## File ownership
- **Exclusive:** `frontend/panel/**`.
- **Read-only:** `frontend/shared/**` (Agent 5's — consume `@hikrad/shared` as a workspace dep; if an export you need is missing, it's a merge-time note, not your edit).
- **Forbidden:** `frontend/portal/**`, `frontend/shared/**` writes, all backend/deploy paths.

## Tasks
1. Vite + React 18 + TS strict scaffold under `frontend/panel/` in the npm workspace; ESLint/Prettier aligned with repo conventions.
2. Component library selection **implementing the frozen constraint** (master §8): RTL-capable — MUI or Ant with RTL cache, or Tailwind + Radix using logical properties only. Prove the choice with a bidirectional smoke page. This choice binds Agent 5 too — it's recorded in the phase brief's merge notes; default to Tailwind + Radix + logical properties if no blocker emerges.
3. App shell: sidebar navigation (mirrors under RTL), top bar with a **global-search placeholder slot** (wired in Phase 2 — FR-2 keyboard-first: `/` focuses it), user menu, responsive breakpoints down to phone width (Hassan). Empty dashboard route, 404, error boundary.
4. API client: typed fetch wrapper honoring C2 (envelope parsing → typed errors, cursor pagination helper, bearer token injection, 401 → redirect to login); token storage with refresh flow stub.
5. Login screen: username/password → C7 stub endpoint; loading/error states; localized via `@hikrad/shared` `useT()` from day one (en+ar strings written, ku keys present).
6. Theming: light theme, ISP-brandable color tokens (consumed from settings later), IQD/date formatting via shared helpers.
7. Panel README: run/dev/build, how routing + API client are structured.

Edge cases: RTL flip must mirror navigation but keep usernames/IPs/MACs LTR (use shared bidi-isolate component); the shell must render acceptably at 360 px width; login must handle backend-down with a friendly localized error.

## Contracts consumed/exposed
- **Consumes:** C2 API conventions, C7 login stub, C8 i18n exports from `@hikrad/shared`, C9 toolchain.
- **Exposes:** the shell + API client + route structure every later panel feature (Phases 2, 3, 5 — same role) plugs into; the component-library decision Agent 5 mirrors for the portal.

## Definition of done
- Gate item 4 (panel half): `https://localhost/` serves the built panel via Caddy; seeded admin logs in and lands on the dashboard shell; language switcher flips en↔ar with fully mirrored layout and zero hardcoded strings (`npm run i18n:check` green).
- Component tests (Vitest + Testing Library): login submit success/failure, API client envelope/401 handling, RTL smoke render.
- Lint + build green in CI.

## Handoff
Phase 2 receives: a shell with navigation slots for NAS/Users/Live Sessions screens, a typed API client, the auth flow (internals swap to real tokens transparently), and the library/RTL foundation all future panel screens use.
