/**
 * Minimal ICU MessageFormat subset (contract C8: interpolation + plurals):
 *
 *   "Hello {name}"
 *   "{count, plural, =0 {none} one {# day} other {# days}}"
 *   "{status, select, active {Active} other {Unknown}}"
 *
 * - `#` inside a plural branch renders the count with locale-aware digits.
 * - Plain `{name}` interpolation stringifies the value verbatim (panel relies
 *   on this — callers pre-format machine values and wrap them in <Ltr>).
 * - A missing variable leaves the `{name}` placeholder literally in place so
 *   the gap is visible instead of silently blank.
 * - Not supported (keep messages simple): ICU `''` escaping / literal braces.
 */
import { formatNumber } from '../format/format'
import { intlLocale, type Locale, type Numerals } from './locales'

export type IcuNode =
  | { kind: 'text'; value: string }
  | { kind: 'arg'; name: string }
  | { kind: 'hash' }
  | { kind: 'plural' | 'select'; name: string; branches: Record<string, IcuNode[]> }

/** Index of the '}' matching the '{' at `open`, or -1 when unbalanced. */
function matchBrace(message: string, open: number): number {
  let depth = 0
  for (let i = open; i < message.length; i++) {
    if (message[i] === '{') depth++
    else if (message[i] === '}' && --depth === 0) return i
  }
  return -1
}

function parseBranches(spec: string, message: string): Record<string, IcuNode[]> {
  const branches: Record<string, IcuNode[]> = {}
  let i = 0
  while (i < spec.length) {
    while (i < spec.length && /\s/.test(spec[i])) i++
    if (i >= spec.length) break
    const keyStart = i
    while (i < spec.length && spec[i] !== '{' && !/\s/.test(spec[i])) i++
    const key = spec.slice(keyStart, i)
    while (i < spec.length && /\s/.test(spec[i])) i++
    if (spec[i] !== '{') throw new Error(`i18n: malformed branch "${key}" in message: ${message}`)
    const close = matchBrace(spec, i)
    if (close === -1) throw new Error(`i18n: unbalanced braces in message: ${message}`)
    branches[key] = parseNodes(spec.slice(i + 1, close), true, message)
    i = close + 1
  }
  return branches
}

function parseNodes(fragment: string, inPlural: boolean, message: string): IcuNode[] {
  const nodes: IcuNode[] = []
  let text = ''
  let i = 0
  const flush = () => {
    if (text) nodes.push({ kind: 'text', value: text })
    text = ''
  }
  while (i < fragment.length) {
    const ch = fragment[i]
    if (ch === '#' && inPlural) {
      flush()
      nodes.push({ kind: 'hash' })
      i++
    } else if (ch === '{') {
      const close = matchBrace(fragment, i)
      if (close === -1) throw new Error(`i18n: unbalanced braces in message: ${message}`)
      const body = fragment.slice(i + 1, close)
      const comma = body.indexOf(',')
      flush()
      if (comma === -1) {
        nodes.push({ kind: 'arg', name: body.trim() })
      } else {
        const name = body.slice(0, comma).trim()
        const rest = body.slice(comma + 1)
        const comma2 = rest.indexOf(',')
        const type = (comma2 === -1 ? rest : rest.slice(0, comma2)).trim()
        if (type !== 'plural' && type !== 'select') {
          throw new Error(`i18n: unsupported ICU type "${type}" in message: ${message}`)
        }
        if (comma2 === -1) throw new Error(`i18n: missing branches for "${name}" in: ${message}`)
        nodes.push({ kind: type, name, branches: parseBranches(rest.slice(comma2 + 1), message) })
      }
      i = close + 1
    } else {
      text += ch
      i++
    }
  }
  flush()
  return nodes
}

const parseCache = new Map<string, IcuNode[]>()

export function parseMessage(message: string): IcuNode[] {
  let nodes = parseCache.get(message)
  if (!nodes) {
    nodes = parseNodes(message, false, message)
    parseCache.set(message, nodes)
  }
  return nodes
}

export interface IcuFormatOptions {
  numerals?: Numerals
  /**
   * Wrap interpolated values in Unicode FIRST STRONG ISOLATE … POP
   * DIRECTIONAL ISOLATE (U+2068/U+2069) so machine values (usernames, IPs,
   * "4.2 GB") keep their own direction inside RTL sentences without needing
   * a wrapping element. Off by default (some callers compare rendered text
   * verbatim); the portal turns it on for mixed-content strings.
   */
  isolate?: boolean
}

const FSI = '⁨' // FIRST STRONG ISOLATE
const PDI = '⁩' // POP DIRECTIONAL ISOLATE

function selectBranch(
  node: Extract<IcuNode, { kind: 'plural' | 'select' }>,
  value: unknown,
  locale: Locale,
): IcuNode[] | undefined {
  const { branches } = node
  if (node.kind === 'plural') {
    const count = Number(value)
    if (branches[`=${count}`]) return branches[`=${count}`]
    try {
      const category = new Intl.PluralRules(intlLocale(locale)).select(count)
      if (branches[category]) return branches[category]
    } catch {
      // no CLDR data for this locale — fall through to "other"
    }
    return branches.other
  }
  return branches[String(value)] ?? branches.other
}

function formatNodes(
  nodes: IcuNode[],
  vars: Record<string, string | number>,
  locale: Locale,
  opts: IcuFormatOptions,
  hashValue?: number,
): string {
  let out = ''
  for (const node of nodes) {
    switch (node.kind) {
      case 'text':
        out += node.value
        break
      case 'hash':
        out += hashValue === undefined ? '#' : formatNumber(hashValue, locale, opts)
        break
      case 'arg': {
        if (node.name in vars) {
          const value = String(vars[node.name])
          out += opts.isolate ? FSI + value + PDI : value
        } else {
          out += `{${node.name}}`
        }
        break
      }
      case 'plural':
      case 'select': {
        const branch = selectBranch(node, vars[node.name], locale)
        if (branch === undefined) {
          out += `{${node.name}}`
        } else {
          const count = node.kind === 'plural' ? Number(vars[node.name]) : hashValue
          out += formatNodes(branch, vars, locale, opts, count)
        }
        break
      }
    }
  }
  return out
}

export function formatMessage(
  message: string,
  vars: Record<string, string | number> | undefined,
  locale: Locale,
  opts: IcuFormatOptions = {},
): string {
  if (!message.includes('{')) return message
  return formatNodes(parseMessage(message), vars ?? {}, locale, opts)
}
