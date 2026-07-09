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
        ok: 'rgb(var(--hik-ok) / <alpha-value>)',
      },
    },
  },
  plugins: [],
} satisfies Config
