#!/usr/bin/env node
/**
 * npm run i18n:check — CI-fatal i18n gate (contract C8 / NFR-6.1, Phase 1,
 * Agent 5 / F). Scans panel + portal + shared sources and the shared locale
 * files. Fails (exit 1) on:
 *
 *   1. Hardcoded user-visible strings: JSX text with letters, string literals
 *      in user-visible JSX attributes (label/title/placeholder/aria-label/…),
 *      and string-literal JSX expression children — anything a user could
 *      read that didn't come through useT().
 *   2. Keys used in source (t('module.key')) missing from any locale file.
 *   3. Locale files out of key parity (a key present in en but missing in
 *      ar/ku, or vice versa), missing per-locale files, or two files claiming
 *      the same top-level namespace.
 *
 * Reports (non-fatal, unless --strict-untranslated — required for the v1
 * cut): ar/ku values still identical to English.
 *
 * Escape hatches, use sparingly:
 *   - a line (or the line above) containing "i18n-exempt"
 *   - JSX text inside <Ltr>, <ChartContainer>, <bdi>, <code>, <pre>, <kbd>,
 *     <samp> — machine values (usernames/IPs/MACs/code) are not UI copy.
 *   - locale-file keys starting with "_" (file-level comments)
 *
 * Usage: node i18n-check.mjs [--root <frontend-dir>] [--strict-untranslated]
 * Parsing uses the workspace's own `typescript` package — no extra deps.
 */
import { readdirSync, readFileSync, existsSync, statSync } from 'node:fs'
import { createRequire } from 'node:module'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const require = createRequire(import.meta.url)
const ts = require('typescript')

const scriptDir = path.dirname(fileURLToPath(import.meta.url))

const args = process.argv.slice(2)
const strictUntranslated = args.includes('--strict-untranslated')
const rootFlag = args.indexOf('--root')
const ROOT =
  rootFlag !== -1 && args[rootFlag + 1]
    ? path.resolve(args[rootFlag + 1])
    : path.resolve(scriptDir, '..', '..')

const LOCALES = ['en', 'ar', 'ku']
const APPS = ['shared', 'panel', 'portal']
const USER_VISIBLE_ATTRS = new Set([
  'label',
  'title',
  'alt',
  'placeholder',
  'aria-label',
  'aria-description',
  'aria-placeholder',
  'aria-roledescription',
  'aria-valuetext',
])
// Elements whose text content is machine values / code, never UI copy.
const MACHINE_VALUE_TAGS = new Set(['Ltr', 'ChartContainer', 'bdi', 'code', 'pre', 'kbd', 'samp'])
// Key patterns whose values are legitimately identical across locales
// (language endonyms), excluded from the untranslated report.
const UNTRANSLATED_EXEMPT = [/^languages\./, /\.languages\./]

// Tested against the path relative to each app's src/ — test helpers, test
// files and dev-only stubs are not user-visible UI.
const EXCLUDED_PATH = /(^|\/)(test|dev)\/|\.test\.|\.spec\.|\.d\.ts$/
const HAS_LETTER = /\p{L}/u

/** ---------- collect source files ---------- */

function walkDir(dir, srcDir, out) {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name)
    const relToSrc = path.relative(srcDir, full).replaceAll('\\', '/')
    if (entry.isDirectory()) {
      if (entry.name === 'node_modules' || entry.name === 'dist') continue
      walkDir(full, srcDir, out)
    } else if (/\.(ts|tsx)$/.test(entry.name) && !EXCLUDED_PATH.test(relToSrc)) {
      out.push(full)
    }
  }
}

const sourceFiles = []
for (const app of APPS) {
  const srcDir = path.join(ROOT, app, 'src')
  if (existsSync(srcDir) && statSync(srcDir).isDirectory()) walkDir(srcDir, srcDir, sourceFiles)
}

/** ---------- load locale files ---------- */

const errors = []
const localeTrees = {} // locale -> merged tree
const localeFileNames = {} // locale -> Set of file basenames

for (const locale of LOCALES) {
  const dir = path.join(ROOT, 'shared', 'locales', locale)
  localeTrees[locale] = {}
  localeFileNames[locale] = new Set()
  if (!existsSync(dir)) {
    errors.push(`locale directory missing: shared/locales/${locale}/`)
    continue
  }
  const sources = {}
  for (const file of readdirSync(dir)
    .filter((f) => f.endsWith('.json'))
    .sort()) {
    localeFileNames[locale].add(file)
    let data
    try {
      data = JSON.parse(readFileSync(path.join(dir, file), 'utf8'))
    } catch (err) {
      errors.push(`shared/locales/${locale}/${file}: invalid JSON (${err.message})`)
      continue
    }
    for (const [ns, value] of Object.entries(data)) {
      if (ns.startsWith('_')) continue
      if (ns in localeTrees[locale]) {
        errors.push(
          `shared/locales/${locale}/${file}: top-level namespace "${ns}" already defined by ${sources[ns]} — namespaces must be unique per locale (contract C8)`,
        )
        continue
      }
      localeTrees[locale][ns] = value
      sources[ns] = file
    }
  }
}

// Same set of locale files in every locale.
const allFileNames = new Set(LOCALES.flatMap((l) => [...localeFileNames[l]]))
for (const file of [...allFileNames].sort()) {
  for (const locale of LOCALES) {
    if (!localeFileNames[locale].has(file)) {
      errors.push(`shared/locales/${locale}/${file} is missing (present in other locales)`)
    }
  }
}

function flatten(tree, prefix, out) {
  for (const [key, value] of Object.entries(tree)) {
    if (key.startsWith('_')) continue
    const full = prefix ? `${prefix}.${key}` : key
    if (typeof value === 'string') out.set(full, value)
    else if (value && typeof value === 'object') flatten(value, full, out)
    else out.set(full, String(value))
  }
  return out
}

const flatKeys = {}
for (const locale of LOCALES) flatKeys[locale] = flatten(localeTrees[locale], '', new Map())

// Key parity: en is the baseline (NFR-6.1) and ar/ku must match exactly.
for (const locale of ['ar', 'ku']) {
  for (const key of flatKeys.en.keys()) {
    if (!flatKeys[locale].has(key)) errors.push(`key "${key}" missing from locale ${locale}`)
  }
  for (const key of flatKeys[locale].keys()) {
    if (!flatKeys.en.has(key)) errors.push(`key "${key}" in locale ${locale} but not in en`)
  }
}

/** ---------- scan sources ---------- */

const usedKeys = [] // { key, file, line }
const usedPatterns = [] // { regex, raw, file, line }

function rel(file) {
  return path.relative(ROOT, file).replaceAll('\\', '/')
}

function scanFile(file) {
  const text = readFileSync(file, 'utf8')
  const lines = text.split(/\r?\n/)
  const sf = ts.createSourceFile(
    file,
    text,
    ts.ScriptTarget.Latest,
    true,
    file.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS,
  )

  const lineOf = (node) => sf.getLineAndCharacterOfPosition(node.getStart(sf)).line
  const isExempt = (node) => {
    const line = lineOf(node)
    return (
      lines[line]?.includes('i18n-exempt') || (line > 0 && lines[line - 1]?.includes('i18n-exempt'))
    )
  }
  const report = (node, message) => errors.push(`${rel(file)}:${lineOf(node) + 1}: ${message}`)

  const enclosingTagName = (node) => {
    for (let cur = node.parent; cur; cur = cur.parent) {
      if (ts.isJsxElement(cur)) return cur.openingElement.tagName.getText(sf)
      if (ts.isJsxFragment(cur)) return null
    }
    return null
  }

  function visit(node) {
    // 1) Raw JSX text: <p>Hello</p>
    if (ts.isJsxText(node) && HAS_LETTER.test(node.text)) {
      const tag = enclosingTagName(node)
      if (!(tag && MACHINE_VALUE_TAGS.has(tag)) && !isExempt(node)) {
        report(
          node,
          `hardcoded JSX text ${JSON.stringify(node.text.trim().slice(0, 60))} — use useT() (contract C8)`,
        )
      }
    }

    // 2) String literal in a user-visible attribute: <input placeholder="…">
    if (
      ts.isJsxAttribute(node) &&
      USER_VISIBLE_ATTRS.has(node.name.getText(sf)) &&
      node.initializer &&
      ts.isStringLiteral(node.initializer) &&
      HAS_LETTER.test(node.initializer.text) &&
      !isExempt(node)
    ) {
      report(
        node,
        `hardcoded ${node.name.getText(sf)}=${JSON.stringify(node.initializer.text.slice(0, 60))} — use useT() (contract C8)`,
      )
    }

    // 3) String-literal JSX expression child: <p>{'Hello'}</p>
    if (
      ts.isJsxExpression(node) &&
      node.parent &&
      (ts.isJsxElement(node.parent) || ts.isJsxFragment(node.parent)) &&
      node.expression &&
      (ts.isStringLiteral(node.expression) ||
        ts.isNoSubstitutionTemplateLiteral(node.expression)) &&
      HAS_LETTER.test(node.expression.text) &&
      !isExempt(node)
    ) {
      const tag = enclosingTagName(node)
      if (!(tag && MACHINE_VALUE_TAGS.has(tag))) {
        report(
          node,
          `hardcoded JSX string ${JSON.stringify(node.expression.text.slice(0, 60))} — use useT() (contract C8)`,
        )
      }
    }

    // 4) t('module.key') usage — collect keys for the locale-file checks.
    if (
      ts.isCallExpression(node) &&
      ts.isIdentifier(node.expression) &&
      node.expression.text === 't' &&
      node.arguments.length > 0
    ) {
      const arg = node.arguments[0]
      const entry = { file: rel(file), line: lineOf(arg) + 1 }
      if (ts.isStringLiteral(arg) || ts.isNoSubstitutionTemplateLiteral(arg)) {
        usedKeys.push({ key: arg.text, ...entry })
      } else if (ts.isTemplateExpression(arg)) {
        // t(`languages.${code}`) — verify at least one key matches the shape.
        const parts = [arg.head.text, ...arg.templateSpans.map((s) => s.literal.text)].map((p) =>
          p.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'),
        )
        usedPatterns.push({
          regex: new RegExp(`^${parts.join('.+')}$`),
          raw: arg.getText(sf),
          ...entry,
        })
      }
      // Other dynamic expressions can't be checked statically — allowed.
    }

    ts.forEachChild(node, visit)
  }
  visit(sf)
}

for (const file of sourceFiles) scanFile(file)

for (const { key, file, line } of usedKeys) {
  for (const locale of LOCALES) {
    if (!flatKeys[locale].has(key)) {
      errors.push(`${file}:${line}: key "${key}" not found in locale ${locale}`)
    }
  }
}

for (const { regex, raw, file, line } of usedPatterns) {
  for (const locale of LOCALES) {
    const any = [...flatKeys[locale].keys()].some((k) => regex.test(k))
    if (!any) {
      errors.push(`${file}:${line}: no key in locale ${locale} matches dynamic ${raw}`)
    }
  }
}

/** ---------- untranslated report (ku may lag in content, never in keys) ---------- */

const untranslated = {}
for (const locale of ['ar', 'ku']) {
  untranslated[locale] = []
  for (const [key, value] of flatKeys[locale]) {
    if (UNTRANSLATED_EXEMPT.some((re) => re.test(key))) continue
    const en = flatKeys.en.get(key)
    if (en !== undefined && en === value && /[A-Za-z]/.test(value)) {
      untranslated[locale].push(key)
    }
  }
}

/** ---------- report ---------- */

const dedup = [...new Set(errors)]
if (dedup.length > 0) {
  console.error(`i18n:check FAILED — ${dedup.length} problem(s):\n`)
  for (const e of dedup) console.error(`  ✗ ${e}`)
  console.error(
    '\nRules: no hardcoded user-visible strings (useT + shared/locales), key parity across en/ar/ku. See frontend/shared/README.md.',
  )
  process.exit(1)
}

console.log(
  `i18n:check OK — ${sourceFiles.length} source files, ${flatKeys.en.size} keys × ${LOCALES.length} locales.`,
)
for (const locale of ['ar', 'ku']) {
  const list = untranslated[locale]
  if (list.length > 0) {
    console.log(
      `  untranslated in ${locale} (identical to en): ${list.length} key(s) — must be 0 for the v1 cut`,
    )
    for (const key of list.slice(0, 15)) console.log(`    • ${key}`)
    if (list.length > 15) console.log(`    … and ${list.length - 15} more`)
  }
}
if (strictUntranslated && (untranslated.ar.length > 0 || untranslated.ku.length > 0)) {
  console.error('\ni18n:check FAILED — --strict-untranslated: translations incomplete (v1 gate).')
  process.exit(1)
}
