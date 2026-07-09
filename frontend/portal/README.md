# @hikrad/portal — subscriber portal (Phase-1 skeleton)

Mobile-first subscriber portal (persona: Noor). This phase ships the skeleton
only (FR-43 groundwork): branded login shell with the language switcher,
three stubbed routes (Home, Usage, Renew) with localized placeholder content,
and the trilingual RTL foundation from
[`@hikrad/shared`](../shared/README.md). Phase 4 fills in real features
(auth, live data, renewal — the renew→CoA hero flow).

## Run

```
npm install        # once, from frontend/ (workspace root)
npm run dev -w portal
npm run build -w portal   # dist/ is what Caddy serves under /portal
npm test -w portal
```

Served at `https://localhost/portal` by the Compose stack's Caddy (contract
C5): the `/portal` prefix is stripped by Caddy and re-added by Vite's
`base: '/portal/'` + the router basename (see `src/main.tsx`). The dev server
proxies `/api` to the stack (override with `VITE_API_PROXY_TARGET`).

## Structure

- `src/App.tsx` — routes: `/` login shell; `/home`, `/usage`, `/renew` inside
  `PortalLayout` (header + bottom tab nav).
- `src/branding.ts` — placeholder branding tokens; replaced by server
  settings in Phase 4/5. Colors flow through `src/theme/tokens.css` (same
  `--hik-*` tokens as the panel).
- All strings live in `../shared/locales/{en,ar,ku}/portal.json` — see the
  shared README for the rules (`npm run i18n:check` is CI-fatal).
- Component library mirrors the frozen Phase-1 decision: Tailwind CSS +
  Radix UI, CSS logical properties only (Radix components arrive with the
  first interactive features in Phase 4).
