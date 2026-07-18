import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'

import { describe, expect, it } from 'vitest'

/**
 * FR-95 / contract C7: the page body must never scroll horizontally — wide
 * content (tables, snippet/config blocks, charts) scrolls inside its own
 * `overflow-x-auto`/`overflow-auto` container instead. jsdom does not
 * compute real layout (no `scrollWidth`/`clientWidth` from an actual
 * renderer), so this smoke test takes the same structural approach
 * `shared/scripts/i18n-check.mjs` already uses for a different invariant:
 * scan source for the known wide-content markers and assert each one has an
 * overflow-scrolling ancestor within a few lines of it in the same file.
 *
 * This is a regression LOCK, not a fresh audit — kickoff research (v2-12
 * phase brief, problem statement) found every existing <table>/<pre>
 * container in this app already correctly wrapped; this test exists so a
 * FUTURE screen that adds one without the wrapper fails CI instead of
 * shipping a body-scroll regression silently.
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
      // Skip doc/line comments — a marker mentioned in prose (e.g. "is
      // `<pre>` (machine text)") is not a real element to check.
      if (trimmed.startsWith('*') || trimmed.startsWith('//')) return
      if (!WIDE_MARKERS.some((re) => re.test(line))) return

      // The overflow class may be on this same element's own className
      // (often a few lines below the opening tag in multi-line JSX), or on
      // an ancestor within the preceding few lines (the common
      // "<div className='overflow-x-auto'><table ...>" wrapper pattern).
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
    expect(walk(SRC).length).toBeGreaterThan(50)
  })

  it('every <table>/<pre> has an overflow-scrolling ancestor nearby — body never scrolls horizontally', () => {
    const violations = findViolations()
    expect(
      violations,
      `Missing overflow-x-auto/overflow-auto wrapper at:\n${violations.join('\n')}`,
    ).toEqual([])
  })
})
