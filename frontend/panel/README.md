# HikRAD Panel (`@hikrad/panel`)

The admin/manager panel — React 18 + TypeScript (strict) + Vite, Tailwind CSS +
Radix UI with **CSS logical properties only** (the frozen RTL-capable
component-library choice, master PRD §8; proven by the bidirectional smoke page
at `/dev/rtl-smoke`). Trilingual en/ar/ku via `@hikrad/shared` (contract C8):
no hardcoded user-visible strings, layout mirrors fully under RTL, and
usernames/IPs/MACs stay LTR through the shared `<Ltr>` bidi-isolate component.

## Run / dev / build

```sh
# Pre-merge (no frontend/ workspace root yet): install inside this package
cd frontend/panel && npm install

npm run dev        # Vite dev server; /api/* proxies to the Compose stack's
                   # Caddy at https://localhost (override: VITE_API_PROXY_TARGET)
npm run build      # type-check (tsc -b) + production bundle → dist/
npm run test       # Vitest + Testing Library
npm run lint       # ESLint + Prettier check
npm run format     # Prettier write
```

In production the built `dist/` is served by Caddy at the site root (contract
C5); all API calls go to the same origin under `/api/v1` (contract C2).

## How routing is structured

`src/main.tsx` composes providers: `I18nProvider` (from `@hikrad/shared`; owns
`<html lang dir>`) → `ErrorBoundary` → `BrowserRouter` → `AuthProvider`.
Routes live in `src/App.tsx`:

- `/login` — public login page against the C7 dev auth stub.
- Everything else sits inside `<RequireAuth><AppShell/></RequireAuth>`:
  - `/` — dashboard shell (empty this phase),
  - `/dev/rtl-smoke` — bidirectional smoke page (sidebar link in dev builds),
  - `*` — localized 404.

`AppShell` (`src/shell/`) renders the sidebar (fixed at `inline-start` from
`md` up, a Radix Dialog drawer below that — usable down to 360 px), and the
top bar with the **global-search placeholder slot** (`/` focuses it; real
search wires in Phase 2, FR-2) and the user menu (language switcher + sign
out). Phase-2/3 screens already have disabled navigation slots in
`src/shell/Sidebar.tsx` — flip `placeholder: false` and add a route.

## How the API client is structured

`src/api/client.ts` implements contract C2:

- `request<T>(path, options)` — JSON fetch against `/api/v1`, bearer-token
  injection from `src/auth/tokenStore.ts`, C2 error envelope parsed into a
  typed `ApiError { status, code, message, fieldErrors }`; unreachable server
  → `NetworkError`.
- 401 handling: every non-`/auth/*` 401 runs through `src/auth/refresh.ts`
  (Phase-1 stub — Phase 2 swaps in the real refresh call without touching
  callers); if unrecoverable, tokens are cleared and `UNAUTHORIZED_EVENT`
  fires, `AuthProvider` drops the session and `RequireAuth` redirects to
  `/login`.
- `listPage<T>` / `paginate<T>` — cursor pagination helpers
  (`?cursor&limit` → `{ items, next_cursor }`).

Domain calls live next to their types (e.g. `src/api/auth.ts` for C7 login).

## i18n & theming

- Strings: `t('module.key')` via `useT()`; the panel's keys live in
  `src/locales/{en,ar,ku}/panel.json` (en+ar translated; ku keys present,
  content intentionally lagging — tracked by `npm run i18n:check`, Agent 5's
  CI gate).
- Brandable theme tokens: `src/theme/tokens.css` CSS custom properties,
  consumed through Tailwind (`tailwind.config.ts`). Later phases inject an
  ISP's brand color from server settings — never hardcode colors in
  components.
- Money/dates: `formatIQD()` / `formatDate()` from `@hikrad/shared`
  (Asia/Baghdad).

## Merge notes (Phase 1, Agent 4 → coordinate with Agent 5)

Written while `frontend/shared` and the `frontend/` workspace root (both
Agent 5's exclusive paths) did not exist yet. To integrate:

1. **Workspace root**: add `"panel"` to `frontend/package.json` workspaces
   (Agent 5 creates the root; this package's local `package-lock.json` is
   gitignored — the root lockfile is canonical). Panel scripts (`lint`,
   `build`, `test`) already match the CI workspace invocations.
2. **Shared package**: add `"@hikrad/shared": "*"` to `dependencies` in
   `frontend/panel/package.json`, then delete `src/dev/shared-stub/` and the
   `@hikrad/shared` alias in `vite.config.ts` + `paths` entry in
   `tsconfig.app.json`. All panel code imports only `@hikrad/shared`.
   Exports the panel needs from the real package (C8 plus two the task file
   presumes): `I18nProvider`, `useT()`, `useLocale()` (must return
   `setLocale` for the language switcher), `formatIQD()`, `formatDate()`,
   `<Ltr>` bidi-isolate, and the `Locale` type.
3. **Locale files**: move `src/locales/{en,ar,ku}/panel.json` to
   `frontend/shared/locales/{en,ar,ku}/panel.json` (contract C8) and point the
   real `I18nProvider` at them; keys are already namespaced (`login.*`,
   `nav.*`, …).
4. **Component-library decision (binds Agent 5, to be recorded in the phase
   brief's merge notes)**: Tailwind CSS + Radix UI primitives + CSS logical
   properties only — no blocker emerged; proven by `/dev/rtl-smoke` and the
   RTL test suite.
