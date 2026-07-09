/**
 * Branding placeholder tokens (FR-43 groundwork). Phase 4/5 fetches these
 * from server settings (ISP name, logo, colors — colors flow through
 * src/theme/tokens.css). Product/ISP names are proper nouns, not translatable
 * UI copy, so they live here rather than in locale files.
 */
export const BRANDING = {
  /** ISP display name; replaced by the branding setting later. */
  name: 'HikRAD',
  /** Placeholder mark shown on the login card until a logo is uploaded. */
  initial: 'H',
} as const
