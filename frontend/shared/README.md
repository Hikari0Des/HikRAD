# @hikrad/shared — i18n/RTL framework & shared UI

The workspace package every HikRAD frontend consumes (contract **C8**, frozen
in [Phase 1](../../docs/phases/phase-1-foundation/00-phase.md)). It owns:

- the **i18n framework**: `I18nProvider`, `useT()`, `useLocale()`
- **formatting**: `formatIQD()`, `formatDate()`, `formatNumber()`, `useFormatters()`
- **bidi/RTL utilities**: `<Ltr>`, `<ChartContainer>`, the logical-properties lint rules
- **shared UI primitives**: `StatusBadge`, `QuotaBar`, `LoadingState`/`EmptyState`/`ErrorState`, `IQDAmount`
- a **thin API client** for contract C2: `createApiClient()`, `ApiError`, `Page<T>`
- the CI gate **`npm run i18n:check`** (from `frontend/`; CI-fatal)

It is a _source_ package: apps compile `src/` directly (`"exports"` points at
`src/index.ts`), so there is no build artifact to keep in sync. Styles for the
primitives ship as plain CSS — import once per app: `import '@hikrad/shared/ui.css'`.

## The rules (every UI agent, every phase)

### 1. No hardcoded user-visible strings — ever

Anything a user can read goes through `useT()`:

```tsx
const t = useT()
<button>{t('portal.login.submit')}</button>
```

`npm run i18n:check` fails CI on raw JSX text, string literals in user-visible
attributes (`label`, `title`, `placeholder`, `alt`, `aria-label`, …) and
string-literal JSX children. Machine values inside `<Ltr>`/`<ChartContainer>`/
`<code>`/`<pre>`/`<bdi>` are exempt (they are data, not copy), as are proper
nouns kept in constants (e.g. `src/branding.ts` in the portal). For a rare
deliberate exception, put `i18n-exempt` in a comment on the same or previous
line — and expect it to be questioned in review.

### 2. How to add a string

1. Pick a key: `module.section.name` — the **top-level key is the namespace**
   and must be unique across all files of a locale (files under
   `locales/<locale>/` are only for organization; they are merged at the root).
2. Add it to **all three** locale files: `locales/en/*.json`,
   `locales/ar/*.json`, `locales/ku/*.json`. Key parity is CI-fatal; Kurdish
   _content_ may temporarily stay English (it shows up in the i18n:check
   report and must be 0 untranslated before the v1 cut), but the _key_ must
   exist. Never let ku fall back to ar — the chain is fixed: locale → en → key.
3. Use it: `t('module.section.name')`, with variables
   `t('quota.of', { used, total })` and ICU plurals
   `"{count, plural, one {# day} other {# days}}"` (Arabic supports
   `zero/one/two/few/many/other`; `#` renders the count with locale digits).
   For mixed RTL sentences containing usernames/numbers, pass
   `{ isolate: true }` as the third argument to bidi-isolate the values.

### 3. How to add a locale file (new module)

Create `locales/{en,ar,ku}/<module>.json`, each with a single top-level
namespace, e.g. `{ "vouchers": { … } }`. The loader picks it up automatically
(`import.meta.glob`); a namespace collision throws at startup and fails
i18n:check.

### 4. Bidi-safe fields (NFR-6.2)

Usernames, MAC addresses, IPs, phone numbers, code/config snippets **always**
render inside `<Ltr>` so they read left-to-right inside Arabic/Kurdish text:

```tsx
<Ltr className="font-mono">{subscriber.username}</Ltr>
```

Charts always live inside `<ChartContainer>` — charts stay LTR even on RTL
pages. IQD amounts use `<IQDAmount amount={…}/>` (isolated, locale digits).

### 5. Layout is logical — no physical left/right

CSS logical properties and logical Tailwind utilities only
(`ms-`/`me-`/`ps-`/`pe-`/`start-`/`end-`/`text-start`/`border-s`/`rounded-e`…).
Enforced by stylelint (`stylelint.config.mjs`, shared by portal — adopt it in
new apps) and an ESLint rule against physical Tailwind classes. `top/bottom`
are fine — they don't mirror. The component-library decision is frozen
(Phase-1 merge notes): **Tailwind CSS + Radix UI + logical properties**.

### 6. Numerals, currency, dates (NFR-6.3)

Use `useFormatters()` (bound to the active locale and the user's
numeral preference) or the pure `formatIQD/formatDate/formatNumber(value,
locale, opts)`. `numerals: 'arab'` forces Eastern-Arabic digits (٠١٢…),
`'latn'` Western, `'auto'` (default) follows the locale. Dates default to
`Asia/Baghdad`. Server-driven defaults (FR-53.2) arrive in a later phase via
`<I18nProvider defaultNumerals>`.

## i18n:check

```
npm run i18n:check                      # from frontend/ — what CI runs
node shared/scripts/i18n-check.mjs --strict-untranslated   # v1-cut gate
```

Fails on: hardcoded strings, keys used in code but missing from any locale,
key-set drift between en/ar/ku, missing per-locale files, namespace
collisions. Reports (non-fatal until `--strict-untranslated`): ar/ku values
still identical to English.

## Tests

`npm test` (Vitest): ICU interpolation/plurals, fallback chain (ku→en, never
ku→ar), Eastern-Arabic `formatIQD`, `<Ltr>` isolation, UI primitives, and the
i18n:check gate itself against planted-violation fixtures
(`test/fixtures/i18n/`).
