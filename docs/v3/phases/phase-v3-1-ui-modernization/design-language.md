# HikRAD Design Language (v3-1, FR-97.3)

The reference for how HikRAD's UI looks and behaves. Every screen redesigned in v3-1 is reviewed against this. The overriding goal (owner, item #11): **extremely usable, simple, feature-dense, modern — and never looking AI-built.**

## Voice
- Product voice is plain, direct, operator-facing. Empty and error states say what happened and what to do next, in the user's language (trilingual). No marketing tone, no exclamation energy, no emoji in the UI.
- Persona anchors: Sara (front desk, ≤3 clicks), Omar (owner on a phone), Ali (network engineer), Hassan (field agent, phone-first), Noor (subscriber). A screen that serves its persona in the fewest steps wins over a prettier one.

## Density & layout
- **Phone-first**: every screen is specified at 360px before desktop. Body never scrolls horizontally (AC-95a) — wide content (tables, charts, config) scrolls inside its own `overflow-x-auto` container.
- Spacing uses the token scale; no ad-hoc pixel values. Vertical rhythm is consistent within a screen.
- One primary action per screen, above the fold; secondary actions grouped and de-emphasized.
- Cards group related content; a page is a small number of clear regions, not a wall of controls.

## Color & elevation
- Only the semantic tokens (`brand`/`surface`/`ink`/`danger`/`warn`/`ok`) — never a raw hex. Both themes carry every token; a screen is reviewed in light **and** dark.
- Elevation is restrained: `shadow-sm` for raised cards, `shadow-md` for popovers/modals. No heavy drop-shadows, no glow.
- **Banned**: gradient-soup backgrounds, neon accents, decorative gradients on text, faux-3D. Flat, calm, legible.

## Typography
- The type scale is tokenized (display / heading / body / caption). One type scale across both apps.
- Numerals, MACs, IPs, usernames, and phone numbers render **LTR** inside RTL text (`<Ltr>`/bidi isolation), tabular numerals for money/quota figures.

## Motion
- Transitions are short (≤200ms) and purposeful. Everything respects `prefers-reduced-motion` — motion degrades to instant. No spinners longer than necessary; skeletons only where they reduce perceived latency. No parallax, no auto-playing motion.

## Forms
- Grouped by task frequency: identity → service → billing → advanced. Rarely-used fields live in a collapsed **Advanced** section.
- Every field's visible label is programmatically associated with its input (the FR-97 form primitives do this by construction). Errors are inline, specific, and localized.

## The "not AI-built" checklist (enforced at review)
1. No gradient-soup / neon / faux-3D; flat and calm.
2. No emoji in UI chrome; icons are from one consistent set with direction semantics mirrored in RTL.
3. Consistent type scale and spacing rhythm on every screen — nothing looks like a different template.
4. Real, product-voice empty/error states — never "No data" placeholder filler.
5. One primary action, clear hierarchy; no button-soup.
6. Both themes and all three locales reviewed; RTL truly mirrored (logical properties only).
7. Alignment and density match the pattern library — no one-off layouts.

## Patterns (see the `/design` gallery)
`Card` / `StatCard`, `DataTable` (sticky header, density, built-in states), `SectionCard`, `FilterBar` (search + removable typed chips), `FormSection` (collapsible Advanced), `DetailHeader`. Compose these; do not hand-roll a one-off when a pattern fits. When a genuinely new pattern is needed, add it to the library and the gallery, don't inline it.
