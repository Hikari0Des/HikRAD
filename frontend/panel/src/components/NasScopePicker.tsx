import { useEffect, useMemo, useRef, useState } from 'react'

import { useT } from '@hikrad/shared'

import { listNas } from '../api/nas'
import type { Nas, NasScope } from '../api/types'
import { Field } from './form'

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
  const [open, setOpen] = useState(false)
  const menuRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    let alive = true
    listNas()
      .then((r) => alive && setNas(r.items))
      .catch(() => alive && setFailed(true))
    return () => {
      alive = false
    }
  }, [])

  // Close on an outside click or Escape — a menu that traps the operator inside
  // a form is worse than no menu.
  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false)
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  const options = useMemo(() => buildOptions(nas, t), [nas, t])
  const scopeError = firstScopeError(errors)

  const toggle = (opt: NasScope) => {
    onChange(toggleScope(scopes, opt))
  }

  return (
    <div className="sm:col-span-2">
      <Field label={t('nasScope.label')} hint={t('nasScope.hint')} error={scopeError}>
        <div className="relative" ref={menuRef}>
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

          <button
            type="button"
            className="w-full rounded-md border border-surface-sunken px-3 py-2 text-start text-sm"
            aria-expanded={open}
            aria-haspopup="listbox"
            onClick={() => setOpen((o) => !o)}
          >
            {t('nasScope.add')}
          </button>

          {open && (
            <div
              role="listbox"
              aria-multiselectable
              className="absolute z-20 mt-1 max-h-64 w-full overflow-y-auto rounded-md border border-surface-sunken bg-surface shadow-lg"
            >
              {options.length === 0 ? (
                <p className="p-3 text-sm text-ink-muted">{t('nasScope.noNas')}</p>
              ) : (
                options.map((opt) => {
                  const checked = scopes.some((s) => scopeKey(s) === scopeKey(opt.scope))
                  return (
                    <label
                      key={scopeKey(opt.scope)}
                      role="option"
                      aria-selected={checked}
                      className={`flex cursor-pointer items-center gap-2 px-3 py-1.5 text-sm hover:bg-surface-sunken/50 ${
                        opt.isService ? 'ps-8' : 'font-medium'
                      }`}
                    >
                      <input type="checkbox" checked={checked} onChange={() => toggle(opt.scope)} />
                      {opt.label}
                    </label>
                  )
                })
              )}
            </div>
          )}
        </div>
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
