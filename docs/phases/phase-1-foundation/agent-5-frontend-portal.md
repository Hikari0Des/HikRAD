# Phase 1 — Agent 5 (Frontend Portal & Localization): shared i18n/RTL framework, portal skeleton

> Owns NFR-6 (foundation), FR-43 groundwork; depends on contracts in [00-phase.md](00-phase.md) (C2, C8, C9); parallel with Agents 1–4.

## Mission & context
HikRAD ships trilingual (Arabic RTL, Kurdish Sorani RTL, English) across two React apps — panel and subscriber portal. Localization debt is a named project risk, so the mitigation is structural and starts now: you build the shared i18n/RTL framework **both** apps consume, the CI check that makes hardcoded strings impossible, and the portal app skeleton (full portal features are Phase 4). Detail source: sub-PRD [07-subscriber-portal-pwa](../../prd/07-subscriber-portal-pwa.md) NFR-6.

## File ownership
- **Exclusive:** `frontend/shared/**`, `frontend/portal/**`, the `frontend/` workspace root (`package.json` workspaces config — coordinate the panel entry with Agent 4 at merge; you create the root, they add their package).
- **Read-only:** `frontend/panel/**`. **Forbidden:** all backend/deploy paths.

## Tasks
1. `frontend/` npm workspace root; `frontend/shared/` as package `@hikrad/shared` (built with the same Vite/TS strict toolchain). [C9]
2. i18n framework per C8: `I18nProvider` (locale detection: stored preference → browser → en), `useT()` with namespaced keys and ICU-style plurals/interpolation, `useLocale()` returning `{locale, dir}` and setting `dir`/`lang` on `<html>`, locale files `frontend/shared/locales/{en,ar,ku}/common.json` (+ per-module files as they appear). Eastern-Arabic numeral option and `formatIQD()`/`formatDate()` honoring locale + (later) server settings. [NFR-6.1, NFR-6.3]
3. Bidi/RTL utilities: `<Ltr>` bidi-isolation component for usernames/MACs/IPs/phones/code, chart-container convention (charts stay LTR inside RTL pages), logical-properties lint rule (stylelint or eslint plugin) forbidding `left/right` physical properties. [NFR-6.2]
4. `npm run i18n:check`: script that scans panel+portal+shared source for hardcoded user-visible strings (JSX text/label props outside `useT`) and for keys missing from any locale file — CI-fatal. Kurdish files may lag in *content* during development but never in *keys* (untranslated keys tracked in the check's report; must be 0 untranslated for v1 cut). [NFR-6.1]
5. Shared UI primitives both apps need (kept minimal): status badge, quota progress bar, localized empty/error/loading states, IQD amount display.
6. `frontend/portal/` skeleton: Vite app, mobile-first single-column layout, branded login page shell (branding fetched later; placeholder tokens), language switcher **on the login page**, three routes stubbed (Home, Usage, Renew) with localized placeholder content. Mirror Agent 4's component-library decision (default Tailwind + Radix + logical properties). [FR-43 groundwork]
7. Docs: `frontend/shared/README.md` — how to add a string, a locale, a bidi-safe field; the rules every UI agent follows in every phase.

Edge cases: locale switch must not lose app state; `ku` (Sorani) is RTL like `ar` but a distinct locale — never fall back ku→ar silently (fallback chain ku→en with report); numbers inside RTL sentences must not reorder (test with mixed "user X used 4.2 GB" strings in ar).

## Contracts consumed/exposed
- **Consumes:** C2 (portal API client mirrors panel's — build a thin shared fetch helper in `@hikrad/shared`), C8 (you implement it), C9.
- **Exposes:** `@hikrad/shared` API surface (C8) — frozen for Agents 4/E and future phases; the i18n:check CI gate; the portal app Phase 4 (same role) fills with features.

## Definition of done
- Gate item 4 (portal half): `/portal` serves the skeleton; switching en/ar/ku flips direction correctly, all three locales render with zero missing keys.
- Gate item 5 (i18n part): `npm run i18n:check` runs in CI and fails on a deliberately-planted violation (prove it in a test), passes on the real tree.
- Unit tests: useT interpolation/plurals, locale fallback chain, formatIQD (incl. Eastern-Arabic numerals), `<Ltr>` isolation rendering.

## Handoff
Phase 4 (same role) receives a portal skeleton ready for features. Every UI agent in every phase receives: the i18n framework, the string rules, and a CI gate that keeps NFR-6 debt at zero instead of a pre-v1 crunch.
