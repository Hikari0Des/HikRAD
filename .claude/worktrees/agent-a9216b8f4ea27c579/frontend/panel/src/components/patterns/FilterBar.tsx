import { useState } from 'react'

import { useT } from '@hikrad/shared'

import { Select, TextInput } from '../form'

/**
 * v3-1 FR-97.2 / FR-98.1 FilterBar — the owner's "subscribers filters feels
 * like trash" target. Replaces a row of bare selects with one search field plus
 * an "add filter" menu that produces removable typed **chips**.
 *
 * Controlled and generic: the screen passes the field catalogue and the current
 * values, and owns persistence (the existing per-manager usePersistentState) and
 * the server round-trip. This component only renders the search + add-menu +
 * active chips — no data logic, so every list adopts it the same way.
 */
export interface FilterField {
  key: string
  label: string
  /** Select options for this filter. `value: ''` is never an active filter. */
  options: { value: string; label: string }[]
}

export function FilterBar({
  search,
  onSearchChange,
  searchPlaceholder,
  fields,
  values,
  onChange,
}: {
  search: string
  onSearchChange: (v: string) => void
  searchPlaceholder?: string
  fields: FilterField[]
  /** key -> selected value; '' or absent = not applied. */
  values: Record<string, string>
  onChange: (values: Record<string, string>) => void
}) {
  const t = useT()
  // The field currently being configured (two-step: pick field → pick value).
  const [pending, setPending] = useState<string>('')

  const isActive = (key: string) => Boolean(values[key])
  const inactiveFields = fields.filter((f) => !isActive(f.key))
  const pendingField = fields.find((f) => f.key === pending)

  const setValue = (key: string, value: string) => {
    const next = { ...values }
    if (value) next[key] = value
    else delete next[key]
    onChange(next)
  }

  const activeChips = fields
    .filter((f) => isActive(f.key))
    .map((f) => ({
      key: f.key,
      label: `${f.label}: ${f.options.find((o) => o.value === values[f.key])?.label ?? values[f.key]}`,
    }))

  const clearAll = () => {
    const cleared: Record<string, string> = {}
    onChange(cleared)
    onSearchChange('')
    setPending('')
  }

  const anyApplied = activeChips.length > 0 || search.trim() !== ''

  return (
    <div className="mb-3 rounded-md border border-border bg-surface-raised p-3">
      <div className="flex flex-wrap items-center gap-2">
        <TextInput
          value={search}
          onChange={(e) => onSearchChange(e.target.value)}
          placeholder={searchPlaceholder ?? t('ui.search')}
          aria-label={searchPlaceholder ?? t('ui.search')}
          className="min-w-0 flex-1"
        />

        {inactiveFields.length > 0 && (
          <Select
            value={pending}
            onChange={(e) => setPending(e.target.value)}
            aria-label={t('filter.add')}
          >
            <option value="">{t('filter.add')}</option>
            {inactiveFields.map((f) => (
              <option key={f.key} value={f.key}>
                {f.label}
              </option>
            ))}
          </Select>
        )}

        {/* Step two: once a field is picked, choose its value → becomes a chip. */}
        {pendingField && (
          <Select
            value=""
            onChange={(e) => {
              setValue(pendingField.key, e.target.value)
              setPending('')
            }}
            aria-label={pendingField.label}
          >
            <option value="">{t('filter.choose', { field: pendingField.label })}</option>
            {pendingField.options
              .filter((o) => o.value !== '')
              .map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
          </Select>
        )}
      </div>

      {(activeChips.length > 0 || anyApplied) && (
        <div className="mt-2 flex flex-wrap items-center gap-2">
          {activeChips.map((chip) => (
            <span
              key={chip.key}
              className="inline-flex items-center gap-1.5 rounded-full bg-brand-soft px-2.5 py-1 text-xs text-brand-strong"
            >
              {chip.label}
              <button
                type="button"
                aria-label={t('ui.remove')}
                onClick={() => setValue(chip.key, '')}
              >
                <span aria-hidden="true">×</span>
              </button>
            </span>
          ))}
          {anyApplied && (
            <button type="button" className="text-xs text-ink-muted underline" onClick={clearAll}>
              {t('users.clearFilters')}
            </button>
          )}
        </div>
      )}
    </div>
  )
}
