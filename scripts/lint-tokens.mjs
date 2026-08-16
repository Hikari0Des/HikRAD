#!/usr/bin/env node
// lint-tokens.mjs — implementation for lint-tokens.sh (FR-116 gate 1, v4-1).
//
// Catches exactly the D2 bug class: a `--hik-*` custom property, or a
// bg-/text-/border-/ring-* Tailwind utility that resolves to one, referenced
// in source but undefined for the app rendering it. An undefined Tailwind
// color utility does not error — it silently emits no rule (an element with
// no background/border/text color) — so this can only be caught by checking
// against ground truth, not by "looks like a color name" guessing.
//
// Ground truth for the Tailwind-utility half: each app's own `vite build`
// output CSS. This deliberately does NOT use the standalone `tailwindcss`
// CLI — that tool's postcss-import resolver does not understand this
// package's `@hikrad/shared` bare-specifier CSS `@import` (Vite's resolver
// does), so it fails to find the canonical tokens at all and is not a
// faithful ground truth here.
//
// Ground truth for the `var(--hik-*)` half: frontend/shared/src/theme/tokens.css,
// the one file the FR-115 frozen contract allows to define them.
import { execSync } from 'node:child_process'
import { existsSync, mkdtempSync, readFileSync, readdirSync, rmSync, statSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..')

// Mirrors each app's tailwind.config.ts `content` array (panel scans only
// its own src; portal additionally scans @hikrad/shared, since its config
// lists '../shared/src/**/*.{ts,tsx}'). Kept explicit rather than parsed
// from the config — if a config's content globs change, update this too.
const APP_SOURCE_DIRS = {
  panel: ['frontend/panel/src'],
  portal: ['frontend/portal/src', 'frontend/shared/src'],
}

const VAR_REF_DIRS = ['frontend/panel/src', 'frontend/portal/src', 'frontend/shared/src']

function walk(dir, exts, out = []) {
  if (!existsSync(dir)) return out
  for (const entry of readdirSync(dir)) {
    if (entry === 'node_modules' || entry === 'dist') continue
    const full = join(dir, entry)
    const st = statSync(full)
    if (st.isDirectory()) walk(full, exts, out)
    else if (exts.includes(entry.slice(entry.lastIndexOf('.')))) out.push(full)
  }
  return out
}

// Strips /* ... */ block comments (the only comment style used for doc
// prose in this repo's .css/.ts/.tsx files) so example class names inside a
// comment — e.g. this very file's and tokens.css's own doc comments quoting
// `bg-state-hover/6` as an illustration — aren't mistaken for real usage.
function stripBlockComments(text) {
  return text.replace(/\/\*[\s\S]*?\*\//g, '')
}

function extractCandidates(text) {
  // bg-/text-/border-/ring-<slug>, stopping before any /<opacity> suffix or
  // punctuation — variant/pseudo prefixes (hover:, focus-visible:, dark:,
  // data-[...]:) are simply not part of the match, so they fall away for
  // free. Non-color utilities in the same families (border-t, text-center,
  // ring-offset-1, ...) match too; that's fine, they're real Tailwind
  // utilities and will be found in the build output like anything else.
  const re = /\b(?:bg|text|border|ring)-[A-Za-z][A-Za-z0-9-]*/g
  return new Set(text.match(re) ?? [])
}

function extractVarRefs(text) {
  const re = /var\(--hik-([a-z0-9-]+)/g
  const set = new Set()
  let m
  while ((m = re.exec(text))) set.add(m[1])
  return set
}

function collect(dirs, exts) {
  const files = dirs.flatMap((d) => walk(join(ROOT, d), exts))
  let text = ''
  for (const f of files) text += stripBlockComments(readFileSync(f, 'utf8')) + '\n'
  return text
}

let failed = false

// ---- 1. var(--hik-*) references must be defined in the canonical file ----
const tokensCssPath = join(ROOT, 'frontend/shared/src/theme/tokens.css')
const tokensCss = stripBlockComments(readFileSync(tokensCssPath, 'utf8'))
const definedNames = new Set([...tokensCss.matchAll(/--hik-([a-z0-9-]+)\s*:/g)].map((m) => m[1]))

const varRefText = collect(VAR_REF_DIRS, ['.ts', '.tsx', '.css'])
const varRefs = extractVarRefs(varRefText)
for (const name of [...varRefs].sort()) {
  if (!definedNames.has(name)) {
    console.error(
      `LINT-TOKENS VIOLATION: var(--hik-${name}) is referenced but not defined in frontend/shared/src/theme/tokens.css`,
    )
    failed = true
  }
}
console.log(`lint-tokens: ${varRefs.size} var(--hik-*) references checked against ${definedNames.size} defined tokens`)

// ---- 2. every bg-/text-/border-/ring-* utility must resolve for its app ----
const requestedApps = process.argv.slice(2)
const apps = requestedApps.length ? requestedApps : Object.keys(APP_SOURCE_DIRS)

for (const app of apps) {
  const appDir = join(ROOT, 'frontend', app)
  const srcText = collect(APP_SOURCE_DIRS[app], ['.ts', '.tsx', '.css'])
  const candidates = extractCandidates(srcText)

  const outDir = mkdtempSync(join(tmpdir(), `hikrad-lint-tokens-${app}-`))
  try {
    execSync(`npx vite build --outDir "${outDir}" --emptyOutDir --logLevel error`, {
      cwd: appDir,
      stdio: 'pipe',
    })
  } catch (err) {
    console.error(`LINT-TOKENS: ${app} build failed, cannot verify utilities against ground truth:`)
    console.error(err.stdout?.toString() ?? err.message)
    rmSync(outDir, { recursive: true, force: true })
    failed = true
    continue
  }

  const assetsDir = join(outDir, 'assets')
  const cssFile = existsSync(assetsDir)
    ? readdirSync(assetsDir).find((f) => f.endsWith('.css'))
    : undefined
  if (!cssFile) {
    console.error(`LINT-TOKENS: ${app} build produced no CSS asset — cannot verify utilities`)
    failed = true
    rmSync(outDir, { recursive: true, force: true })
    continue
  }
  const builtCss = readFileSync(join(assetsDir, cssFile), 'utf8')
  rmSync(outDir, { recursive: true, force: true })

  let appFailed = 0
  for (const cls of [...candidates].sort()) {
    if (!builtCss.includes(cls)) {
      console.error(
        `LINT-TOKENS VIOLATION (${app}): utility "${cls}" is used in source but produces no CSS rule (undefined token)`,
      )
      failed = true
      appFailed++
    }
  }
  console.log(
    `lint-tokens (${app}): ${candidates.size} utility candidates checked against the built CSS, ${appFailed} undefined`,
  )
}

process.exit(failed ? 1 : 0)
