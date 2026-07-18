import { useMemo } from 'react'

import { useT } from '@hikrad/shared'

import type { PermissionGroup } from '../../api/security'
import { Checkbox } from '../../components/form'

/**
 * Roles matrix editor (FR-27.1): a modules × verbs grid comprehensible to Omar.
 * Each row is a permission module (subscribers, billing, …); each cell a verb
 * checkbox (view/create/edit/delete or a bare action like renew). Bare action
 * permissions with no dot are grouped under a synthetic "actions" row.
 *
 * Controlled: holds no state of its own so it is trivially unit-testable —
 * toggling a cell calls `onChange` with the next permission set.
 */
export function RoleMatrix({
  catalog,
  value,
  onChange,
  disabled = false,
}: {
  catalog: PermissionGroup[]
  value: ReadonlySet<string>
  onChange: (next: Set<string>) => void
  disabled?: boolean
}) {
  const t = useT()

  // Normalise: split each permission into module + verb. Bare perms (no dot)
  // fall under the "actions" pseudo-module.
  const rows = useMemo(() => normalise(catalog), [catalog])

  function toggle(perm: string, on: boolean) {
    const next = new Set(value)
    if (on) next.add(perm)
    else next.delete(perm)
    onChange(next)
  }

  return (
    // Horizontal-scroll container so the grid survives phone width (edge case).
    <div className="overflow-x-auto">
      <table className="w-full min-w-[32rem] border-collapse text-sm">
        <thead>
          <tr className="border-b border-surface-sunken text-start text-xs text-ink-muted">
            <th className="py-2 pe-4 text-start font-medium">{t('roles.moduleCol')}</th>
            <th className="py-2 text-start font-medium">{t('roles.permsCol')}</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.module} className="border-b border-surface-sunken/60 align-top">
              <th scope="row" className="py-2 pe-4 text-start font-medium">
                {t(`roles.module.${row.module}`)}
              </th>
              <td className="py-2">
                <div className="flex flex-wrap gap-x-4 gap-y-1.5">
                  {row.perms.map(({ perm, verb }) => {
                    const checked = value.has(perm)
                    return (
                      <Checkbox
                        key={perm}
                        label={t(`roles.verb.${verb}`)}
                        checked={checked}
                        disabled={disabled}
                        aria-label={`${row.module}.${verb}`}
                        onChange={(e) => toggle(perm, e.target.checked)}
                      />
                    )
                  })}
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

interface MatrixRow {
  module: string
  perms: { perm: string; verb: string }[]
}

/** Group a flat permission catalog into modules × verbs rows. */
export function normalise(catalog: PermissionGroup[]): MatrixRow[] {
  const byModule = new Map<string, { perm: string; verb: string }[]>()
  for (const group of catalog) {
    for (const perm of group.permissions) {
      const dot = perm.indexOf('.')
      const module = dot === -1 ? 'actions' : perm.slice(0, dot)
      const verb = dot === -1 ? perm : perm.slice(dot + 1)
      const list = byModule.get(module) ?? []
      list.push({ perm, verb })
      byModule.set(module, list)
    }
  }
  return [...byModule.entries()].map(([module, perms]) => ({ module, perms }))
}
