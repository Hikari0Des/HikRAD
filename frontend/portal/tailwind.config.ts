import type { Config } from 'tailwindcss'

// Same token system as the panel (brandable --hik-* custom properties, see
// src/theme/tokens.css) and the same layout rule: logical utilities only
// (ms-/me-/ps-/pe-/start-/end-/text-start) — physical left/right is forbidden
// (contract C8). Shared package sources are included so any Tailwind classes
// used there generate CSS in this app's build.
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}', '../shared/src/**/*.{ts,tsx}'],
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
        ok: 'rgb(var(--hik-ok) / <alpha-value>)',
        warning: 'rgb(var(--hik-warning) / <alpha-value>)',
      },
      // v3-1 FR-97.1: radius + elevation tokens (mirrors the panel). Radius keys
      // equal Tailwind's defaults → no existing utility shifts; elevation adopts
      // the token shadows (incl. the dark-mode fix).
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
