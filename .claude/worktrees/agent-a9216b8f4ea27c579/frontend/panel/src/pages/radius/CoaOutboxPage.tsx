import { useEffect, useState } from 'react'

import {
  EmptyState,
  ErrorState,
  LoadingState,
  Ltr,
  StatCard,
  useFormatters,
  useT,
} from '@hikrad/shared'

import {
  cancelCoAOutbox,
  getCoAOutbox,
  getCoAOutboxCounters,
  listCoAOutbox,
  retryCoAOutbox,
  type CoAOutboxAttempt,
  type CoAOutboxItem,
  type CoAOutboxState,
} from '../../api/coaOutbox'
import { useAuth } from '../../auth/AuthContext'
import { PERM_COA_OUTBOX_MANAGE } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { Modal } from '../../components/Modal'
import { PageHeader } from '../../components/PageHeader'
import { FilterBar } from '../../components/patterns/FilterBar'
import { Pill } from '../../components/Pill'
import { useToast } from '../../components/Toast'
import { useAsync } from '../../hooks/useAsync'
import { usePaginated } from '../../hooks/usePaginated'
import { usePersistentState } from '../../hooks/usePersistentState'

/** How often the queue and counters re-poll (FR-152 auto-refresh) — SAMM's
 * outbox is a screen operators actually watch, so this stays fast. */
const REFRESH_MS = 8000

const STATE_TONE: Record<CoAOutboxState, 'muted' | 'brand' | 'warn' | 'ok' | 'danger'> = {
  pending: 'muted',
  sent: 'brand',
  nack: 'warn',
  done: 'ok',
  failed: 'danger',
}

/**
 * CoA Outbox (v4-8, FR-152): the durable queue's operator surface — six
 * counters, status/action filters, username/NAS/reason search, auto-refresh,
 * per-row retry/cancel/inspect. Everything here is read-only except retry and
 * cancel; nothing on this screen ever dispatches a CoA directly (the
 * dispatcher's worker tick does that — see backend/coa_dispatcher.go).
 */
export function CoaOutboxPage() {
  const t = useT()
  const { can } = useAuth()
  const { formatDate } = useFormatters()
  const { toast } = useToast()
  const canManage = can(PERM_COA_OUTBOX_MANAGE)

  const [filters, setFilters] = usePersistentState<Record<string, string>>('coaOutbox.filters', {})
  const [search, setSearch] = useState('')
  const [detailId, setDetailId] = useState<string | null>(null)

  const key = JSON.stringify({ filters, search })
  const page = usePaginated<CoAOutboxItem>(
    (cursor) =>
      listCoAOutbox({
        cursor,
        state: (filters.state as CoAOutboxState | undefined) || undefined,
        action:
          filters.action === 'update' || filters.action === 'disconnect'
            ? filters.action
            : undefined,
        search: search || undefined,
      }),
    key,
  )
  const counters = useAsync(() => getCoAOutboxCounters(), [])

  useEffect(() => {
    const id = setInterval(() => {
      page.reset()
      counters.reload()
    }, REFRESH_MS)
    return () => clearInterval(id)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key])

  async function doRetry(id: string) {
    try {
      await retryCoAOutbox(id)
      toast(t('coaOutbox.retryDone'), 'ok')
      page.reset()
      counters.reload()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    }
  }

  async function doCancel(id: string) {
    try {
      await cancelCoAOutbox(id)
      toast(t('coaOutbox.cancelDone'), 'ok')
      page.reset()
      counters.reload()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    }
  }

  return (
    <section>
      <PageHeader title={t('coaOutbox.title')} />

      <div className="mb-4 grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-6">
        <StatCard label={t('coaOutbox.counter.pending')} value={counters.data?.pending ?? '—'} />
        <StatCard label={t('coaOutbox.counter.sent')} value={counters.data?.sent ?? '—'} />
        <StatCard label={t('coaOutbox.counter.nack')} value={counters.data?.nack ?? '—'} />
        <StatCard label={t('coaOutbox.counter.done')} value={counters.data?.done ?? '—'} />
        <StatCard
          label={t('coaOutbox.counter.failed')}
          value={counters.data?.failed ?? '—'}
          hint={
            counters.data
              ? t('coaOutbox.counter.failed24h', { n: counters.data.failed_24h })
              : undefined
          }
        />
        <StatCard label={t('coaOutbox.counter.total')} value={counters.data?.total ?? '—'} />
      </div>

      <FilterBar
        search={search}
        onSearchChange={setSearch}
        searchPlaceholder={t('coaOutbox.searchPlaceholder')}
        fields={[
          {
            key: 'state',
            label: t('coaOutbox.filter.state'),
            options: (['pending', 'sent', 'nack', 'done', 'failed'] as const).map((s) => ({
              value: s,
              label: t(`coaOutbox.state.${s}`),
            })),
          },
          {
            key: 'action',
            label: t('coaOutbox.filter.action'),
            options: (['update', 'disconnect'] as const).map((a) => ({
              value: a,
              label: t(`coaOutbox.action.${a}`),
            })),
          },
        ]}
        values={filters}
        onChange={setFilters}
      />

      {page.error ? (
        <ErrorState onRetry={page.reset} />
      ) : page.items.length === 0 && !page.loading ? (
        <EmptyState title={t('coaOutbox.empty.title')} body={t('coaOutbox.empty.body')} />
      ) : (
        <div className="overflow-x-auto rounded-md border border-border">
          <table className="w-full min-w-[60rem] text-sm">
            <thead className="bg-surface-sunken/40 text-start text-xs text-ink-muted">
              <tr>
                <th className="px-3 py-2 text-start font-medium">{t('coaOutbox.col.username')}</th>
                <th className="px-3 py-2 text-start font-medium">{t('coaOutbox.col.nas')}</th>
                <th className="px-3 py-2 text-start font-medium">{t('coaOutbox.col.action')}</th>
                <th className="px-3 py-2 text-start font-medium">{t('coaOutbox.col.state')}</th>
                <th className="px-3 py-2 text-start font-medium">{t('coaOutbox.col.reason')}</th>
                <th className="px-3 py-2 text-start font-medium">{t('coaOutbox.col.updated')}</th>
                <th className="px-3 py-2" />
              </tr>
            </thead>
            <tbody>
              {page.items.map((row) => (
                <tr key={row.id} className="border-t border-border/60">
                  <td className="px-3 py-2">
                    <Ltr>{row.username}</Ltr>
                  </td>
                  <td className="px-3 py-2">{row.nas_name || <Ltr>{row.nas_id}</Ltr>}</td>
                  <td className="px-3 py-2">
                    {t(`coaOutbox.action.${row.action}`)}
                    {row.fallback_used ? (
                      <span className="ms-1 text-xs text-warn">{t('coaOutbox.fallbackBadge')}</span>
                    ) : null}
                  </td>
                  <td className="px-3 py-2">
                    <Pill tone={STATE_TONE[row.state]}>{t(`coaOutbox.state.${row.state}`)}</Pill>
                  </td>
                  <td className="px-3 py-2 text-ink-muted">{row.reason}</td>
                  <td className="px-3 py-2 text-ink-muted">{formatDate(row.updated_at)}</td>
                  <td className="px-3 py-2 text-end">
                    <div className="flex justify-end gap-1">
                      <Button size="sm" variant="ghost" onClick={() => setDetailId(row.id)}>
                        {t('coaOutbox.inspect')}
                      </Button>
                      {canManage && (row.state === 'nack' || row.state === 'failed') ? (
                        <Button size="sm" variant="secondary" onClick={() => void doRetry(row.id)}>
                          {t('coaOutbox.retry')}
                        </Button>
                      ) : null}
                      {canManage && row.state === 'pending' ? (
                        <Button size="sm" variant="danger" onClick={() => void doCancel(row.id)}>
                          {t('coaOutbox.cancel')}
                        </Button>
                      ) : null}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {page.loading ? <LoadingState /> : null}
          {page.hasMore && !page.loading ? (
            <div className="p-3 text-center">
              <Button size="sm" variant="ghost" onClick={page.loadMore}>
                {t('ui.loadMore')}
              </Button>
            </div>
          ) : null}
        </div>
      )}

      <CoAOutboxInspectModal
        id={detailId}
        onClose={() => setDetailId(null)}
        onChanged={() => {
          page.reset()
          counters.reload()
        }}
        canManage={canManage}
      />
    </section>
  )
}

function CoAOutboxInspectModal({
  id,
  onClose,
  onChanged,
  canManage,
}: {
  id: string | null
  onClose: () => void
  onChanged: () => void
  canManage: boolean
}) {
  const t = useT()
  const { formatDate } = useFormatters()
  const { toast } = useToast()
  const {
    data: row,
    loading,
    reload,
  } = useAsync(() => (id ? getCoAOutbox(id) : Promise.resolve(undefined)), [id])
  const [busy, setBusy] = useState(false)

  async function doRetry() {
    if (!row) return
    setBusy(true)
    try {
      await retryCoAOutbox(row.id)
      toast(t('coaOutbox.retryDone'), 'ok')
      reload()
      onChanged()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    } finally {
      setBusy(false)
    }
  }

  async function doCancel() {
    if (!row) return
    setBusy(true)
    try {
      await cancelCoAOutbox(row.id)
      toast(t('coaOutbox.cancelDone'), 'ok')
      reload()
      onChanged()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open={id !== null}
      onOpenChange={(o) => !o && onClose()}
      title={t('coaOutbox.detailTitle')}
      size="lg"
    >
      {loading || !row ? (
        <LoadingState />
      ) : (
        <div className="space-y-4">
          <div className="grid gap-2 text-sm sm:grid-cols-2">
            <div>
              <span className="text-ink-muted">{t('coaOutbox.col.username')}: </span>
              <Ltr>{row.username}</Ltr>
            </div>
            <div>
              <span className="text-ink-muted">{t('coaOutbox.col.nas')}: </span>
              {row.nas_name || <Ltr>{row.nas_id}</Ltr>}
            </div>
            <div>
              <span className="text-ink-muted">{t('coaOutbox.col.action')}: </span>
              {t(`coaOutbox.action.${row.action}`)}
            </div>
            <div>
              <span className="text-ink-muted">{t('coaOutbox.currentOp')}: </span>
              {t(`coaOutbox.action.${row.current_op}`)}
            </div>
            <div>
              <span className="text-ink-muted">{t('coaOutbox.col.state')}: </span>
              <Pill tone={STATE_TONE[row.state]}>{t(`coaOutbox.state.${row.state}`)}</Pill>
            </div>
            <div>
              <span className="text-ink-muted">{t('coaOutbox.col.reason')}: </span>
              {row.reason}
            </div>
            <div>
              <span className="text-ink-muted">{t('coaOutbox.attemptCount')}: </span>
              {row.attempt_count}
            </div>
            <div>
              <span className="text-ink-muted">{t('coaOutbox.col.updated')}: </span>
              {formatDate(row.updated_at)}
            </div>
          </div>

          {row.fallback_used ? (
            <div className="rounded-md bg-warn/10 p-3 text-sm text-warn">
              {t('coaOutbox.fallbackNotice')}
              {row.fallback_reason ? (
                <span className="ms-1 text-ink-muted">— {row.fallback_reason}</span>
              ) : null}
            </div>
          ) : null}

          <div>
            <p className="mb-2 text-sm font-semibold">{t('coaOutbox.intendedAttributes')}</p>
            {row.attributes.length === 0 ? (
              <p className="text-xs text-ink-muted">{t('coaOutbox.noAttributes')}</p>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-xs">
                  <thead>
                    <tr className="text-start text-ink-muted">
                      <th className="py-1 pe-3 text-start font-medium">{t('coaOutbox.intent')}</th>
                      <th className="py-1 text-start font-medium">{t('coaOutbox.value')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {row.attributes.map((a, i) => (
                      <tr key={i} className="border-t border-border/60">
                        <td className="py-1 pe-3">
                          <Ltr>{a.intent}</Ltr>
                        </td>
                        <td className="py-1">
                          <Ltr>{a.value}</Ltr>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>

          <div>
            <p className="mb-2 text-sm font-semibold">{t('coaOutbox.attempts')}</p>
            {row.attempts.length === 0 ? (
              <p className="text-xs text-ink-muted">{t('coaOutbox.noAttempts')}</p>
            ) : (
              <div className="space-y-2">
                {row.attempts.map((a) => (
                  <AttemptRow key={a.attempt_no} attempt={a} />
                ))}
              </div>
            )}
          </div>

          {(row.state === 'pending' || row.state === 'nack' || row.state === 'failed') &&
          canManage ? (
            <div className="flex justify-end gap-2 border-t border-border pt-4">
              {row.state === 'pending' ? (
                <Button variant="danger" disabled={busy} onClick={() => void doCancel()}>
                  {t('coaOutbox.cancel')}
                </Button>
              ) : null}
              {row.state === 'nack' || row.state === 'failed' ? (
                <Button disabled={busy} onClick={() => void doRetry()}>
                  {busy ? t('ui.working') : t('coaOutbox.retry')}
                </Button>
              ) : null}
            </div>
          ) : null}
        </div>
      )}
    </Modal>
  )
}

/** One dispatch attempt: op/outcome/timestamp, with the exact wire-level
 * attributes sent — the packet-capture-verifiable evidence (gate item 6). */
function AttemptRow({ attempt }: { attempt: CoAOutboxAttempt }) {
  const t = useT()
  const { formatDate } = useFormatters()
  const [open, setOpen] = useState(false)

  return (
    <div className="rounded-md border border-border p-2 text-xs">
      <div className="flex items-center justify-between gap-2">
        <span>
          {t('coaOutbox.attemptNo', { n: attempt.attempt_no })} —{' '}
          {t(`coaOutbox.action.${attempt.op}`)} — <Ltr>{attempt.outcome}</Ltr>
        </span>
        <span className="text-ink-muted">{formatDate(attempt.attempted_at)}</span>
      </div>
      {attempt.error ? (
        <p className="mt-1 text-danger">
          <Ltr>{attempt.error}</Ltr>
        </p>
      ) : null}
      {attempt.wire_attributes.length > 0 ? (
        <>
          <button
            type="button"
            onClick={() => setOpen((o) => !o)}
            className="mt-1 text-ink-muted underline"
          >
            {open ? t('coaOutbox.hideWire') : t('coaOutbox.showWire')}
          </button>
          {open ? (
            <div className="overflow-x-auto">
              <table className="mt-2 w-full text-xs">
                <thead>
                  <tr className="text-ink-muted">
                    <th className="py-1 pe-2 text-start font-medium">{t('coaOutbox.wireType')}</th>
                    <th className="py-1 text-start font-medium">{t('coaOutbox.wireValue')}</th>
                  </tr>
                </thead>
                <tbody>
                  {attempt.wire_attributes.map((w, i) => (
                    <tr key={i} className="border-t border-border/60">
                      <td className="py-1 pe-2">
                        <Ltr>{String(w.type)}</Ltr>
                      </td>
                      <td className="py-1">
                        <Ltr>{w.vendor_text ?? w.text ?? w.vendor_hex ?? w.hex}</Ltr>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}
        </>
      ) : null}
    </div>
  )
}
