import { useState } from 'react'

import { ErrorState, LoadingState, useFormatters, useT } from '@hikrad/shared'

import {
  auditExportUrl,
  downloadAuthorized,
  listAuditLog,
  type AuditFilters,
  type AuditRow,
} from '../../api/security'
import { useAuth } from '../../auth/AuthContext'
import { PERM_EXPORT } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { TextInput } from '../../components/form'
import { PageHeader } from '../../components/PageHeader'
import { usePaginated } from '../../hooks/usePaginated'
import { usePersistentState } from '../../hooks/usePersistentState'

/** Audit-log viewer (FR-27): human-readable rows, filters, export, diff. */
export function AuditLogPage() {
  const t = useT()
  const { can } = useAuth()
  // Remembered per manager until cleared (item 7).
  const [filters, setFilters] = usePersistentState<AuditFilters>('audit.filters', {})
  const key = JSON.stringify(filters)
  const page = usePaginated<AuditRow>((cursor) => listAuditLog({ cursor }, filters), key)

  function setField(k: keyof AuditFilters, v: string) {
    setFilters((prev) => ({ ...prev, [k]: v || undefined }))
  }

  return (
    <section>
      <PageHeader
        title={t('auditlog.title')}
        actions={
          can(PERM_EXPORT) ? (
            <Button
              size="sm"
              variant="secondary"
              onClick={() => void downloadAuthorized(auditExportUrl(filters), 'audit-log.csv')}
            >
              {t('auditlog.export')}
            </Button>
          ) : null
        }
      />

      <div className="mb-4 grid grid-cols-2 gap-2 sm:grid-cols-4">
        <TextInput
          value={filters.action ?? ''}
          onChange={(e) => setField('action', e.target.value)}
          placeholder={t('auditlog.filter.action')}
          dir="ltr"
        />
        <TextInput
          value={filters.entity_type ?? ''}
          onChange={(e) => setField('entity_type', e.target.value)}
          placeholder={t('auditlog.filter.entity')}
          dir="ltr"
        />
        <TextInput
          type="date"
          value={filters.from ?? ''}
          onChange={(e) => setField('from', e.target.value)}
        />
        <TextInput
          type="date"
          value={filters.to ?? ''}
          onChange={(e) => setField('to', e.target.value)}
        />
      </div>

      {page.error ? (
        <ErrorState onRetry={page.reset} />
      ) : (
        <div className="space-y-2">
          {page.items.map((row) => (
            <AuditRowView key={row.id} row={row} />
          ))}
          {page.loading ? <LoadingState /> : null}
          {page.items.length === 0 && !page.loading ? (
            <p className="p-6 text-center text-sm text-ink-muted">{t('auditlog.empty')}</p>
          ) : null}
          {page.hasMore && !page.loading ? (
            <div className="text-center">
              <Button size="sm" variant="ghost" onClick={page.loadMore}>
                {t('ui.loadMore')}
              </Button>
            </div>
          ) : null}
        </div>
      )}
    </section>
  )
}

/**
 * A readable event title: the per-action locale entry when it exists
 * ("Subscriber renewed"), else a humanised form of the raw action
 * ("currency_rate.create" → "Currency rate — create") so a new backend action
 * can never leak an i18n key into the UI again.
 */
function useActionLabel() {
  const t = useT()
  return (action: string): string => {
    const key = `auditlog.action.${action}`
    const msg = t(key)
    if (msg !== key) return msg
    const dot = action.indexOf('.')
    if (dot === -1) return humanise(action)
    return `${humanise(action.slice(0, dot))} — ${humanise(action.slice(dot + 1))}`
  }
}

function humanise(raw: string): string {
  const words = raw.replace(/[_.]/g, ' ').trim()
  return words.charAt(0).toUpperCase() + words.slice(1)
}

function AuditRowView({ row }: { row: AuditRow }) {
  const t = useT()
  const { formatDate } = useFormatters()
  const [open, setOpen] = useState(false)
  const actionLabel = useActionLabel()

  const entityKey = `auditlog.entity.${row.entity_type}`
  const entityMsg = t(entityKey)
  const entity = entityMsg === entityKey ? humanise(row.entity_type) : entityMsg

  return (
    <div className="rounded-md border border-surface-sunken bg-surface-raised p-3 text-sm">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="flex w-full items-start justify-between gap-3 text-start"
      >
        <span className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
          <span className="font-medium">{actionLabel(row.action)}</span>
          <span className="rounded bg-surface-sunken px-1.5 py-0.5 text-xs text-ink-muted">
            {entity}
          </span>
          {row.actor_username ? (
            <span className="text-xs text-ink-muted">
              {t('auditlog.by', { actor: row.actor_username })}
            </span>
          ) : null}
          <span className="text-xs text-ink-muted">{formatDate(row.at)}</span>
        </span>
        <span aria-hidden="true" className="text-ink-muted">
          {open ? '▾' : '▸'}
        </span>
      </button>
      {open ? <AuditDiff row={row} /> : null}
    </div>
  )
}

/**
 * Field-level change view: one row per field that differs between before and
 * after (create shows every after field, delete every before field). Raw JSON
 * stays available behind a toggle for debugging.
 */
function AuditDiff({ row }: { row: AuditRow }) {
  const t = useT()
  const [raw, setRaw] = useState(false)
  const changes = diffFields(row.before, row.after)

  return (
    <div className="mt-2 space-y-2">
      {raw ? (
        <div className="grid gap-2 sm:grid-cols-2">
          <DiffPane label={t('auditlog.before')} value={row.before} />
          <DiffPane label={t('auditlog.after')} value={row.after} />
        </div>
      ) : changes.length === 0 ? (
        <p className="text-xs text-ink-muted">{t('auditlog.noChanges')}</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-xs">
            <thead>
              <tr className="text-start text-ink-muted">
                <th className="py-1 pe-3 text-start font-medium">{t('auditlog.field')}</th>
                <th className="py-1 pe-3 text-start font-medium">{t('auditlog.before')}</th>
                <th className="py-1 text-start font-medium">{t('auditlog.after')}</th>
              </tr>
            </thead>
            <tbody>
              {changes.map((c) => (
                <tr key={c.field} className="border-t border-surface-sunken/60 align-top">
                  <td className="py-1 pe-3 font-medium">
                    <bdi dir="ltr">{c.field}</bdi>
                  </td>
                  <td className="py-1 pe-3 text-ink-muted">
                    <bdi dir="ltr">{c.before ?? '—'}</bdi>
                  </td>
                  <td className="py-1">
                    <bdi dir="ltr">{c.after ?? '—'}</bdi>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <button
        type="button"
        onClick={() => setRaw((v) => !v)}
        className="text-xs text-ink-muted underline"
      >
        {raw ? t('auditlog.showChanges') : t('auditlog.showRaw')}
      </button>
    </div>
  )
}

interface FieldChange {
  field: string
  before: string | null
  after: string | null
}

/** Flatten a JSON value for display: scalars as-is, objects/arrays stringified. */
function display(v: unknown): string {
  if (v == null) return ''
  if (typeof v === 'object') return JSON.stringify(v)
  return String(v)
}

/** Union of before/after keys, keeping only fields whose value changed. */
export function diffFields(before: unknown, after: unknown): FieldChange[] {
  const b = (before ?? {}) as Record<string, unknown>
  const a = (after ?? {}) as Record<string, unknown>
  if (typeof b !== 'object' || typeof a !== 'object' || Array.isArray(b) || Array.isArray(a)) {
    return [
      {
        field: '·',
        before: before == null ? null : display(before),
        after: after == null ? null : display(after),
      },
    ]
  }
  const keys = [...new Set([...Object.keys(b), ...Object.keys(a)])]
  const out: FieldChange[] = []
  for (const k of keys) {
    const bv = k in b ? display(b[k]) : null
    const av = k in a ? display(a[k]) : null
    if (bv !== av) out.push({ field: k, before: bv, after: av })
  }
  return out
}

function DiffPane({ label, value }: { label: string; value: unknown }) {
  return (
    <div>
      <p className="mb-1 text-xs text-ink-muted">{label}</p>
      <pre className="max-h-48 overflow-auto rounded bg-surface-sunken/40 p-2 text-xs" dir="ltr">
        {value == null ? '—' : JSON.stringify(value, null, 2)}
      </pre>
    </div>
  )
}
