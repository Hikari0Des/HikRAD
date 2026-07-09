/**
 * Locale message loading (contract C8). All JSON under
 * frontend/shared/locales/{en,ar,ku}/*.json is merged per locale at the ROOT:
 * a file's top-level keys are namespaces ("module.key" keys — e.g.
 * panel.json's "login" → t('login.title')), and the file name is only
 * organization. Namespace collisions across files are a build-time error.
 * Keys whose name starts with "_" (e.g. "_comment") are ignored.
 */
type Tree = Record<string, unknown>

const files = import.meta.glob('../../locales/*/*.json', {
  eager: true,
}) as Record<string, Tree | { default: Tree }>

function buildMessages(): Record<string, Tree> {
  const byLocale: Record<string, Tree> = {}
  const namespaceSource: Record<string, Record<string, string>> = {}
  for (const [path, mod] of Object.entries(files)) {
    const match = /locales\/([^/]+)\/([^/]+)\.json$/.exec(path)
    if (!match) continue
    const locale = match[1]
    const data = ('default' in mod ? mod.default : mod) as Tree
    const target = (byLocale[locale] ??= {})
    const sources = (namespaceSource[locale] ??= {})
    for (const [ns, value] of Object.entries(data)) {
      if (ns.startsWith('_')) continue
      if (ns in target) {
        throw new Error(
          `i18n: namespace "${ns}" in ${path} already defined by ${sources[ns]} — top-level keys must be unique per locale (contract C8)`,
        )
      }
      target[ns] = value
      sources[ns] = path
    }
  }
  return byLocale
}

export const MESSAGES: Record<string, Tree> = buildMessages()

/** Resolve a dot-separated "module.key" in one locale's message tree. */
export function lookup(tree: Tree | undefined, key: string): string | undefined {
  let node: unknown = tree
  for (const part of key.split('.')) {
    if (typeof node !== 'object' || node === null) return undefined
    node = (node as Tree)[part]
  }
  return typeof node === 'string' ? node : undefined
}

/**
 * Fallback chain (NFR-6.1): locale → en → the key itself. NEVER ku→ar —
 * Kurdish Sorani is a distinct locale; missing ku strings fall back to
 * English and are tracked by i18n:check.
 */
export function resolveMessage(trees: Record<string, Tree>, locale: string, key: string): string {
  return lookup(trees[locale], key) ?? (locale !== 'en' ? lookup(trees.en, key) : undefined) ?? key
}
