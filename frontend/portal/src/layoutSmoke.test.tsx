import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'

import { describe, expect, it } from 'vitest'

/**
 * FR-95 / contract C7: the page body must never scroll horizontally — wide
 * content (tables, snippet/config blocks, charts) scrolls inside its own
 * `overflow-x-auto`/`overflow-auto` container instead. jsdom does not
 * compute real layout, so this smoke test takes the same structural
 * approach `shared/scripts/i18n-check.mjs` already uses for a different
 * invariant: scan source for known wide-content markers and assert each has
 * an overflow-scrolling ancestor nearby in the same file. Mirrors the
 * panel's src/layoutSmoke.test.tsx.
 *
 * Portal has no `<table>`/`<pre>` today (verified at kickoff) — this test
 * exists as a regression lock for when one is added, not a currently-firing
 * check.
 */

const SRC = join(__dirname)
const WIDE_MARKERS = [/<table\b/, /<pre\b/]
const OVERFLOW_CLASS = /overflow-x-auto|overflow-auto/

function walk(dir: string, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    const p = join(dir, entry)
    const s = statSync(p)
    if (s.isDirectory()) {
      if (entry === 'node_modules') continue
      walk(p, out)
    } else if (/\.tsx$/.test(entry) && !/\.test\.tsx$/.test(entry)) {
      out.push(p)
    }
  }
  return out
}

function findViolations(): string[] {
  const violations: string[] = []
  for (const file of walk(SRC)) {
    const text = readFileSync(file, 'utf8')
    const lines = text.split('\n')
    lines.forEach((line, i) => {
      const trimmed = line.trim()
      if (trimmed.startsWith('*') || trimmed.startsWith('//')) return
      if (!WIDE_MARKERS.some((re) => re.test(line))) return

      const window = lines.slice(Math.max(0, i - 6), i + 5).join('\n')
      if (!OVERFLOW_CLASS.test(window)) {
        violations.push(`${file.slice(SRC.length + 1)}:${i + 1}`)
      }
    })
  }
  return violations
}

describe('Responsive/overflow audit (FR-95, C7)', () => {
  it('scanned enough of the tree to be a meaningful check', () => {
    expect(walk(SRC).length).toBeGreaterThan(20)
  })

  it('every <table>/<pre> has an overflow-scrolling ancestor nearby — body never scrolls horizontally', () => {
    const violations = findViolations()
    expect(
      violations,
      `Missing overflow-x-auto/overflow-auto wrapper at:\n${violations.join('\n')}`,
    ).toEqual([])
  })
})
