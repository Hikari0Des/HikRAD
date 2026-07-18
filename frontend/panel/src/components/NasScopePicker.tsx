import { useEffect, useMemo, useState } from 'react'

import { useT } from '@hikrad/shared'

import { listNas } from '../api/nas'
import type { Nas, NasScope } from '../api/types'
import { Combobox, Field, type ComboboxOption } from './form'

/**
 * FR-64 NAS-scope picker, shared by the subscriber and profile forms.
 *
 * An account may be allowed on SEVERAL NASes/zones — "the Karrada tower and the
 * Mansour tower", "two of this router's three hotspot zones". A single dropdown
 * could only say one NAS or all of them, so operators picked all of them and the
 * scope went unused. This is therefore a multi-select: a menu of every NAS and
 * its service instances, with the chosen ones shown as removable chips.
 *
 * Selecting NOTHING is the default and means ANY NAS — v1's behaviour. That is
 * stated in the empty state rather than left to inference, because "no chips"
 * could just as easily read as "nowhere", and the two are opposites.
 */
export function NasScopePicker({
  scopes,
  onChange,
  errors,
}: {
  scopes: NasScope[]
  onChange: (next: NasScope[]) => void
  errors?: Record<string, string>
}) {
  const t = useT()
  const [nas, setNas] = useState<Nas[]>([])
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    let alive = true
    listNas()
      .then((r) => alive && setNas(r.items))
      .catch(() => alive && setFailed(true))
    return () => {
      alive = false
    }
  }, [])

  const options = useMemo(() => buildOptions(nas, t), [nas, t])
  const scopeError = firstScopeError(errors)
  const selectedKeys = useMemo(() => scopes.map(scopeKey), [scopes])
  const comboboxOptions: ComboboxOption[] = useMemo(
    () => options.map((o) => ({ value: scopeKey(o.scope), label: o.label, indent: o.isService })),
    [options],
  )

  // Combobox reports the whole next selection, but toggleScope's narrowing
  // rules (a service selection narrows an already-whole-NAS scope, etc.,
  // see its own doc comment) need to know exactly WHICH entry changed — it
  // toggles exactly one per interaction, so the symmetric difference always
  // has exactly one member.
  const handleComboboxChange = (nextKeys: string[]) => {
    const changedKey =
      nextKeys.find((k) => !selectedKeys.includes(k)) ??
      selectedKeys.find((k) => !nextKeys.includes(k))
    const opt = options.find((o) => scopeKey(o.scope) === changedKey)
    if (opt) onChange(toggleScope(scopes, opt.scope))
  }

  return (
    <div className="sm:col-span-2">
      <Field label={t('nasScope.label')} hint={t('nasScope.hint')} error={scopeError}>
        <div className="mb-2 flex flex-wrap items-center gap-1.5">
          {scopes.length === 0 ? (
            <span className="text-sm text-ink-muted">{t('nasScope.anyNas')}</span>
          ) : (
            scopes.map((s) => (
              <span
                key={scopeKey(s)}
                className="flex items-center gap-1 rounded bg-surface-sunken px-2 py-0.5 text-xs"
              >
                {labelForScope(s, options, t)}
                <button
                  type="button"
                  className="text-ink-muted hover:text-danger"
                  aria-label={t('nasScope.remove', { name: labelForScope(s, options, t) })}
                  onClick={() => onChange(scopes.filter((x) => scopeKey(x) !== scopeKey(s)))}
                >
                  ×
                </button>
              </span>
            ))
          )}
        </div>

        <Combobox
          options={comboboxOptions}
          selected={selectedKeys}
          onChange={handleComboboxChange}
          triggerLabel={t('nasScope.add')}
          searchPlaceholder={t('nasScope.search')}
          noOptionsLabel={t('nasScope.noNas')}
          noMatchLabel={t('search.noResults')}
        />
        {failed && <p className="mt-1 text-sm text-[--color-danger]">{t('nasScope.loadFailed')}</p>}
      </Field>
    </div>
  )
}

/** A stable identity for a scope, used as a React key and for set membership. */
export function scopeKey(s: NasScope): string {
  return `${s.nas_id}:${s.nas_service_id || ''}`
}

interface ScopeOption {
  scope: NasScope
  label: string
  /** Service rows render indented under their NAS. */
  isService: boolean
}

/**
 * Flattens the NAS list into menu options: each NAS ("every service on it"),
 * then one row per service instance beneath it.
 */
export function buildOptions(
  nas: Nas[],
  t: (k: string, p?: Record<string, string>) => string,
): ScopeOption[] {
  const out: ScopeOption[] = []
  for (const n of nas) {
    out.push({
      scope: { nas_id: n.id, nas_service_id: '' },
      label: t('nasScope.wholeNas', { name: n.name }),
      isService: false,
    })
    for (const s of n.services ?? []) {
      out.push({
        scope: { nas_id: n.id, nas_service_id: s.id },
        // nas.typeName ("PPPoE"), not serviceType ("PPPoE only") — this names
        // the ROUTER's service, not a subscriber's entitlement.
        label: serviceLabel(s.label, s.ros_server_name, t(`nas.typeName.${s.service}`)),
        isService: true,
      })
    }
  }
  return out
}

/**
 * Adds or removes one scope, keeping the set coherent:
 *
 * - Choosing a whole NAS drops that NAS's per-service scopes — the NAS-wide
 *   entry already allows them, and showing both reads as a contradiction.
 * - Choosing a service on a NAS that is already selected whole NARROWS to that
 *   service, rather than being silently ignored. Ignoring the click would leave
 *   the operator clicking a checkbox that does nothing.
 *
 * The backend dedupes to the same shape (radius.DedupeScopes), so what the
 * operator sees here is what gets saved.
 */
export function toggleScope(scopes: NasScope[], opt: NasScope): NasScope[] {
  const key = scopeKey(opt)
  if (scopes.some((s) => scopeKey(s) === key)) {
    return scopes.filter((s) => scopeKey(s) !== key)
  }
  if (!opt.nas_service_id) {
    return [...scopes.filter((s) => s.nas_id !== opt.nas_id), opt]
  }
  return [...scopes.filter((s) => !(s.nas_id === opt.nas_id && !s.nas_service_id)), opt]
}

/** Names a chip. Falls back to the raw id if the NAS list hasn't loaded yet. */
function labelForScope(
  s: NasScope,
  options: ScopeOption[],
  t: (k: string, p?: Record<string, string>) => string,
): string {
  const match = options.find((o) => scopeKey(o.scope) === scopeKey(s))
  return match?.label ?? t('nasScope.unknown')
}

/**
 * The backend reports per-entry errors as `nas_scopes.<i>`. The set is edited as
 * a whole here, so show the first message rather than trying to pin it to a chip
 * whose index the operator never sees.
 */
function firstScopeError(errors?: Record<string, string>): string | undefined {
  if (!errors) return undefined
  if (errors.nas_scopes) return errors.nas_scopes
  const key = Object.keys(errors).find((k) => k.startsWith('nas_scopes.'))
  return key ? errors[key] : undefined
}

/**
 * Names one service instance for a picker. An operator recognises their zone by
 * its label ("Lobby") or the router's server name — falling back to the bare
 * kind only when neither is set.
 */
export function serviceLabel(label: string, rosServerName: string, kind: string): string {
  const name = label || rosServerName
  return name ? `${name} (${kind})` : kind
}
