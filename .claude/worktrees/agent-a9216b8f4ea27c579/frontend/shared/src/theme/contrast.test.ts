/**
 * FR-116 gate 3 (v4-1) — WCAG AA contrast, computed from the token values
 * themselves, not eyeballed or screenshotted. This file reads
 * theme/tokens.css as text and parses the actual `--hik-*` RGB triplets out
 * of it, so a value change in that file is what this test verifies —
 * there is no second, hand-copied set of numbers here to drift out of sync.
 *
 * The pairs below are the "legal pairings" design-language.md declares —
 * every foreground/background combination a component is allowed to build
 * (see that doc's role table for the rule: components reference roles only,
 * never a raw color). Each pair carries the WCAG 1.4.3/1.4.11 threshold that
 * applies to how it is used: 4.5:1 for text, 3:1 for a UI-component
 * boundary like a focus ring or an emphasized divider. `disabled`/
 * `disabled-ink` and the `chart-*` series are deliberately absent: WCAG
 * exempts disabled controls from contrast requirements, and the chart
 * palette is graphical (categorical hue separation), not text.
 */
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const TOKENS_CSS_PATH = join(dirname(fileURLToPath(import.meta.url)), 'tokens.css')

type Rgb = [number, number, number]
type Tokens = Record<string, Rgb>

function blockAfter(css: string, anchor: string): string {
  const anchorIndex = css.indexOf(anchor)
  if (anchorIndex === -1)
    throw new Error(`contrast.test.ts: anchor not found in tokens.css: ${anchor}`)
  const start = css.indexOf('{', anchorIndex)
  let depth = 0
  let i = start
  for (; i < css.length; i++) {
    if (css[i] === '{') depth++
    else if (css[i] === '}') {
      depth--
      if (depth === 0) break
    }
  }
  return css.slice(start + 1, i)
}

function parseTokens(block: string): Tokens {
  const out: Tokens = {}
  for (const m of block.matchAll(/--hik-([a-z0-9-]+)\s*:\s*(\d+)\s+(\d+)\s+(\d+)\s*;/g)) {
    out[m[1]] = [Number(m[2]), Number(m[3]), Number(m[4])]
  }
  return out
}

const css = readFileSync(TOKENS_CSS_PATH, 'utf8')

// Anchors match this file's actual, hand-authored selectors (see the header
// comment on each block in tokens.css) — not a general CSS parse, an exact
// read of the one file the FR-115 frozen contract lets define these.
const light = parseTokens(blockAfter(css, ':root {'))
const dark = parseTokens(blockAfter(css, ":root[data-theme='dark'] {"))
const fallback = parseTokens(blockAfter(css, ":root:not([data-theme='light']) {"))

function srgbToLinear(c: number): number {
  const v = c / 255
  return v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4)
}

function relativeLuminance([r, g, b]: Rgb): number {
  return 0.2126 * srgbToLinear(r) + 0.7152 * srgbToLinear(g) + 0.0722 * srgbToLinear(b)
}

function contrastRatio(a: Rgb, b: Rgb): number {
  const [l1, l2] = [relativeLuminance(a), relativeLuminance(b)]
  const [hi, lo] = l1 > l2 ? [l1, l2] : [l2, l1]
  return (hi + 0.05) / (lo + 0.05)
}

const TEXT_AA = 4.5
const UI_AA = 3.0

// [foreground token, background token, required ratio]
const TEXT_PAIRS: Array<[string, string, number]> = [
  ['ink', 'surface', TEXT_AA],
  ['ink', 'surface-raised', TEXT_AA],
  ['ink', 'surface-sunken', TEXT_AA],
  ['ink-muted', 'surface', TEXT_AA],
  ['ink-muted', 'surface-raised', TEXT_AA],
  ['ink-inverse', 'brand', TEXT_AA],
  ['ink-inverse', 'brand-strong', TEXT_AA],
  ['ink-inverse', 'danger', TEXT_AA],
  ['ink-inverse', 'warn', TEXT_AA],
  ['ink-inverse', 'ok', TEXT_AA],
  ['ink-inverse', 'info', TEXT_AA],
  ['danger', 'surface', TEXT_AA],
  ['danger', 'surface-raised', TEXT_AA],
  ['warn', 'surface', TEXT_AA],
  ['warn', 'surface-raised', TEXT_AA],
  ['ok', 'surface', TEXT_AA],
  ['ok', 'surface-raised', TEXT_AA],
  ['info', 'surface', TEXT_AA],
  ['info', 'surface-raised', TEXT_AA],
  ['brand', 'surface', TEXT_AA],
  ['brand', 'surface-raised', TEXT_AA],
]

const UI_PAIRS: Array<[string, string, number]> = [
  ['focus', 'surface', UI_AA],
  ['focus', 'surface-raised', UI_AA],
  ['border-strong', 'surface', UI_AA],
  ['border-strong', 'surface-raised', UI_AA],
]

const ALL_PAIRS = [...TEXT_PAIRS, ...UI_PAIRS]

function runTable(themeName: string, tokens: Tokens) {
  describe(`contrast — ${themeName}`, () => {
    for (const [fg, bg, min] of ALL_PAIRS) {
      it(`${fg} on ${bg} >= ${min}:1`, () => {
        const fgRgb = tokens[fg]
        const bgRgb = tokens[bg]
        expect(fgRgb, `token --hik-${fg} not found in ${themeName} block`).toBeDefined()
        expect(bgRgb, `token --hik-${bg} not found in ${themeName} block`).toBeDefined()
        const ratio = contrastRatio(fgRgb, bgRgb)
        // gate item 5 requires the ratio table pasted into the gate result
        console.log(`  [${themeName}] ${fg}/${bg}: ${ratio.toFixed(2)}:1 (need ${min}:1)`)
        expect(ratio).toBeGreaterThanOrEqual(min)
      })
    }
  })
}

runTable('light', light)
runTable('dark', dark)
// FR-117: the prefers-color-scheme fallback block must carry the same dark
// palette, so it has to clear the identical bar — this is what would catch
// the fallback silently drifting from :root[data-theme='dark'] over time.
runTable('dark (prefers-color-scheme fallback)', fallback)

describe('contrast — fallback parity (FR-117)', () => {
  it('the prefers-color-scheme fallback block matches :root[data-theme="dark"] exactly', () => {
    expect(fallback).toEqual(dark)
  })
})
