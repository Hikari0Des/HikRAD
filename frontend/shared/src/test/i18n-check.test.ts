import { spawnSync } from 'node:child_process'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const here = path.dirname(fileURLToPath(import.meta.url))
const script = path.resolve(here, '../../scripts/i18n-check.mjs')
const fixtures = path.resolve(here, '../../test/fixtures/i18n')
const frontendRoot = path.resolve(here, '../../..')

function runCheck(root: string) {
  const res = spawnSync(process.execPath, [script, '--root', root], { encoding: 'utf8' })
  return { status: res.status, out: res.stdout + res.stderr }
}

describe('npm run i18n:check (contract C8 CI gate)', () => {
  it('passes on a clean tree', () => {
    const { status, out } = runCheck(path.join(fixtures, 'clean'))
    expect(out).toContain('i18n:check OK')
    expect(status).toBe(0)
  })

  it('fails on a deliberately planted hardcoded string', () => {
    const { status, out } = runCheck(path.join(fixtures, 'violation'))
    expect(status).toBe(1)
    expect(out).toContain('hardcoded JSX text')
    expect(out).toContain('Bad.tsx')
  })

  it('fails on a key missing from a locale file', () => {
    const { status, out } = runCheck(path.join(fixtures, 'missing-key'))
    expect(status).toBe(1)
    expect(out).toContain('app.missing')
  })

  it('passes on the real frontend tree (gate item 5)', () => {
    const { status, out } = runCheck(frontendRoot)
    expect(out).toContain('i18n:check OK')
    expect(status).toBe(0)
  })
})
