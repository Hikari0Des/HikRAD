/**
 * Logical-properties lint rule (contract C8 / NFR-6.2, Phase 1, Agent 5 / F).
 *
 * All layout CSS must use logical properties (margin-inline-start,
 * padding-inline-end, inset-inline-start, text-align: start, …) so the whole
 * UI mirrors when <html dir> flips to rtl. Physical left/right properties and
 * values are forbidden. Built-in stylelint rules only — no plugin dependency.
 *
 * Portal and panel both reuse this config verbatim (their own
 * stylelint.config.mjs each re-export it) — the panel adopted it in v2
 * phase 12 (C9), closing the gap this comment used to note as outstanding.
 */
export default {
  rules: {
    'property-disallowed-list': [
      [
        '/^margin-(left|right)$/',
        '/^padding-(left|right)$/',
        '/^border-(left|right)/',
        'left',
        'right',
        'float',
        'clear',
      ],
      {
        message:
          'Physical left/right property — use the logical equivalent (…-inline-start/…-inline-end, inset-inline-*) so RTL mirrors (contract C8).',
      },
    ],
    'declaration-property-value-disallowed-list': [
      {
        'text-align': ['left', 'right'],
      },
      {
        message: 'Use text-align: start/end so RTL mirrors (contract C8).',
      },
    ],
  },
}
