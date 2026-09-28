import { useT } from '@hikrad/shared'

import type {
  AutoSetupApplyResultItem,
  AutoSetupConflict,
  AutoSetupPlanItem,
  AutoSetupPreview,
  AutoSetupResolution,
} from '../api/types'
import { RadioGroup, RadioOption } from './form'

/**
 * The plan → preview → per-item resolution rendering, factored out of
 * NasAutoSetupModal/ServiceProvisionModal (v2 phase 2) because v4-7 needs the
 * exact same surface in five more places: the wizard's review step, the
 * Internet tab, the Firewall tab, and the service forms.
 *
 * Two invariants this component exists to keep identical everywhere:
 *
 *   1. **The literal command is always shown.** Every planned item renders
 *      its exact `command` string (gate item 5) — an operator who cannot see
 *      what will be written to their router must not be able to approve it.
 *   2. **Abort is the default.** The resolution RadioGroup's value falls back
 *      to 'abort' whenever the resolutions map has no entry for a conflict,
 *      and 'abort' maps back to *removing* the key rather than sending a
 *      third enum value — the wire contract is "absent = abort" (FR-66.2).
 *
 * `mode` picks the conflict treatment. 'resolve' offers keep/update/abort per
 * conflict; 'abort' shows conflicts read-only, for the write paths whose
 * conflicts have no safe update meaning (a WAN uplink, an unmanaged firewall
 * rule) — there, the only honest choice is cancel.
 */
export function PlanPreview({
  preview,
  mode = 'abort',
  resolutions,
  onResolutionChange,
}: {
  preview: AutoSetupPreview
  mode?: 'resolve' | 'abort'
  resolutions?: Record<string, AutoSetupResolution>
  onResolutionChange?: (key: string, choice: AutoSetupResolution | undefined) => void
}) {
  const t = useT()

  if (preview.conflicts.length > 0) {
    return (
      <div className="rounded-md bg-danger/10 p-3 text-sm">
        <p className="mb-2 font-medium text-danger">{t('nas.autoSetup.conflictsTitle')}</p>
        <ul className="space-y-3">
          {preview.conflicts.map((c) => (
            <ConflictRow
              key={c.key || c.path}
              conflict={c}
              mode={mode}
              choice={resolutions?.[c.key]}
              onChange={onResolutionChange}
            />
          ))}
        </ul>
        <p className="mt-2 text-xs text-ink-muted">
          {mode === 'resolve' ? t('nas.autoSetup.conflictsBody') : t('nas.plan.conflictAbortOnly')}
        </p>
      </div>
    )
  }

  if (preview.items.length === 0) {
    return <p className="text-sm text-ink-muted">{t('nas.plan.nothingToDo')}</p>
  }

  return (
    <div>
      <p className="mb-2 text-sm font-medium">
        {t('nas.autoSetup.itemsTitle', { count: preview.items.length })}
      </p>
      <ul className="space-y-2">
        {preview.items.map((item, i) => (
          <PlanItemRow key={`${item.path}-${i}`} item={item} />
        ))}
      </ul>
    </div>
  )
}

/** One planned command, with its exact wire text (gate item 5). */
function PlanItemRow({ item }: { item: AutoSetupPlanItem }) {
  const t = useT()
  return (
    <li className="rounded-md border border-border p-2 text-sm">
      <span className="me-2 rounded bg-surface-sunken px-1.5 py-0.5 text-xs">
        {t(`nas.autoSetup.action.${item.action}`)}
      </span>
      <code dir="ltr" className="text-xs">
        {item.path}
      </code>
      <pre
        dir="ltr"
        className="mt-1 overflow-x-auto rounded bg-ink/90 p-2 text-xs text-ink-inverse"
      >
        {item.command}
      </pre>
      {item.current_state ? (
        <p className="mt-1 text-xs text-ink-muted">
          {t('nas.plan.currentState')}: <code dir="ltr">{item.current_state}</code>
        </p>
      ) : null}
    </li>
  )
}

function ConflictRow({
  conflict,
  mode,
  choice,
  onChange,
}: {
  conflict: AutoSetupConflict
  mode: 'resolve' | 'abort'
  choice: AutoSetupResolution | undefined
  onChange?: (key: string, choice: AutoSetupResolution | undefined) => void
}) {
  const t = useT()
  return (
    <li className="rounded border border-danger/20 bg-surface p-2">
      <code dir="ltr" className="block text-xs text-ink-muted">
        {conflict.path}
      </code>
      <p>{conflict.reason}</p>
      {conflict.existing ? (
        <p className="mt-1 text-xs text-ink-muted">
          {t('nas.plan.existing')}: <code dir="ltr">{conflict.existing}</code>
        </p>
      ) : null}
      {conflict.resolvable && conflict.update_command ? (
        <pre
          dir="ltr"
          className="mt-1 overflow-x-auto rounded bg-ink/90 p-2 text-xs text-ink-inverse"
        >
          {conflict.update_command}
        </pre>
      ) : null}
      {mode === 'resolve' && onChange ? (
        <RadioGroup
          className="mt-2 text-xs"
          name={`resolution-${conflict.key}`}
          value={choice ?? 'abort'}
          onValueChange={(v) =>
            onChange(conflict.key, v === 'abort' ? undefined : (v as AutoSetupResolution))
          }
        >
          <RadioOption value="abort" label={t('nas.autoSetup.resolution.abort')} />
          <RadioOption value="keep" label={t('nas.autoSetup.resolution.keep')} />
          {conflict.resolvable ? (
            <RadioOption value="update" label={t('nas.autoSetup.resolution.update')} />
          ) : null}
        </RadioGroup>
      ) : null}
      {mode === 'resolve' && !conflict.resolvable ? (
        <p className="mt-1 text-xs text-ink-muted">{t('nas.plan.notResolvable')}</p>
      ) : null}
    </li>
  )
}

/** Per-item apply outcome list (contract C6), shared by every write path. */
export function PlanApplyResults({
  results,
  allOk,
}: {
  results: AutoSetupApplyResultItem[]
  allOk: boolean
}) {
  const t = useT()
  return (
    <div className="space-y-2">
      <p className={`text-sm font-medium ${allOk ? 'text-ok' : 'text-danger'}`}>
        {allOk ? t('nas.autoSetup.applyOk') : t('nas.autoSetup.applyPartial')}
      </p>
      <ul className="space-y-1 text-sm">
        {results.map((r, i) => (
          <li key={`${r.path}-${i}`} className="flex items-center justify-between gap-2">
            <code dir="ltr" className="text-xs">
              {r.path}
            </code>
            <span className={r.ok ? 'text-ok' : 'text-danger'}>
              {r.ok ? t('ui.ok') : (r.error ?? t('common.error.title'))}
            </span>
          </li>
        ))}
      </ul>
    </div>
  )
}
