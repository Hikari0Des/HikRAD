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

/** Audit-log viewer (FR-27): human-readable rows, filters, export, diff. */
export function AuditLogPage() {
  const t = useT()
  const { can } = useAuth()
  const [filters, setFilters] = useState<AuditFilters>({})
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

function AuditRowView({ row }: { row: AuditRow }) {
  const t = useT()
  const { formatDate } = useFormatters()
  const [open, setOpen] = useState(false)
  // Prefer the localized summary key; fall back to the raw action string.
  const summary = row.summary_key
    ? t(`auditlog.summary.${row.summary_key}`, row.summary_params)
    : row.action

  return (
    <div className="rounded-md border border-surface-sunken bg-surface-raised p-3 text-sm">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="flex w-full items-start justify-between gap-3 text-start"
      >
        <span>
          <span className="font-medium">{summary}</span>
          <span className="ms-2 text-xs text-ink-muted">
            <bdi dir="ltr">{row.entity_type}</bdi> · {formatDate(row.at)}
          </span>
        </span>
        <span aria-hidden="true" className="text-ink-muted">
          {open ? '▾' : '▸'}
        </span>
      </button>
      {open ? (
        <div className="mt-2 grid gap-2 sm:grid-cols-2">
          <DiffPane label={t('auditlog.before')} value={row.before} />
          <DiffPane label={t('auditlog.after')} value={row.after} />
        </div>
      ) : null}
    </div>
  )
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
