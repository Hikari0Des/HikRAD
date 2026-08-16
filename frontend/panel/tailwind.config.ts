import type { Config } from 'tailwindcss'

// Brandable color tokens live in src/theme/tokens.css as CSS custom properties
// (RGB triplets) so an ISP's brand color can later be injected from server
// settings without a rebuild. All layout uses logical utilities (ms-/me-/ps-/
// pe-/start-/end-/text-start) — physical left/right is forbidden (contract C8).
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        brand: {
          DEFAULT: 'rgb(var(--hik-brand) / <alpha-value>)',
          strong: 'rgb(var(--hik-brand-strong) / <alpha-value>)',
          soft: 'rgb(var(--hik-brand-soft) / <alpha-value>)',
        },
        surface: {
          DEFAULT: 'rgb(var(--hik-surface) / <alpha-value>)',
          raised: 'rgb(var(--hik-surface-raised) / <alpha-value>)',
          sunken: 'rgb(var(--hik-surface-sunken) / <alpha-value>)',
        },
        ink: {
          DEFAULT: 'rgb(var(--hik-ink) / <alpha-value>)',
          muted: 'rgb(var(--hik-ink-muted) / <alpha-value>)',
          inverse: 'rgb(var(--hik-ink-inverse) / <alpha-value>)',
        },
        danger: 'rgb(var(--hik-danger) / <alpha-value>)',
        warn: 'rgb(var(--hik-warn) / <alpha-value>)',
        ok: 'rgb(var(--hik-ok) / <alpha-value>)',
        info: 'rgb(var(--hik-info) / <alpha-value>)',
        // v4-1 FR-115 D3: a real line token — replaces the border-surface-sunken
        // abuse (161 + 23 call sites migrated to border-border/border-border-strong).
        border: {
          DEFAULT: 'rgb(var(--hik-border) / <alpha-value>)',
          strong: 'rgb(var(--hik-border-strong) / <alpha-value>)',
        },
        // v4-1 FR-115 D4: focus ring, state overlays, disabled, and the chart
        // series — the roles the token set was missing entirely.
        focus: 'rgb(var(--hik-focus) / <alpha-value>)',
        state: {
          hover: 'rgb(var(--hik-state-hover) / <alpha-value>)',
          active: 'rgb(var(--hik-state-active) / <alpha-value>)',
          selected: 'rgb(var(--hik-state-selected) / <alpha-value>)',
        },
        disabled: {
          DEFAULT: 'rgb(var(--hik-disabled) / <alpha-value>)',
          ink: 'rgb(var(--hik-disabled-ink) / <alpha-value>)',
        },
        chart: {
          1: 'rgb(var(--hik-chart-1) / <alpha-value>)',
          2: 'rgb(var(--hik-chart-2) / <alpha-value>)',
          3: 'rgb(var(--hik-chart-3) / <alpha-value>)',
          4: 'rgb(var(--hik-chart-4) / <alpha-value>)',
          5: 'rgb(var(--hik-chart-5) / <alpha-value>)',
          6: 'rgb(var(--hik-chart-6) / <alpha-value>)',
          7: 'rgb(var(--hik-chart-7) / <alpha-value>)',
          8: 'rgb(var(--hik-chart-8) / <alpha-value>)',
        },
      },
      // v3-1 FR-97.1: radius + elevation resolve to the CSS tokens in
      // theme/tokens.css. Additive — the DEFAULT keys keep Tailwind's own
      // values so no existing `rounded`/`shadow` utility shifts.
      borderRadius: {
        sm: 'var(--hik-radius-sm)',
        md: 'var(--hik-radius-md)',
        lg: 'var(--hik-radius-lg)',
        xl: 'var(--hik-radius-xl)',
      },
      boxShadow: {
        sm: 'var(--hik-shadow-sm)',
        md: 'var(--hik-shadow-md)',
        lg: 'var(--hik-shadow-lg)',
      },
    },
  },
  plugins: [],
} satisfies Config
