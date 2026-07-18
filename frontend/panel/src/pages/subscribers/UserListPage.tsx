import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'

import { Ltr, LoadingState, ErrorState, StatusBadge, useFormatters, useT } from '@hikrad/shared'

import { listProfiles } from '../../api/profiles'
import { listManagers, type ManagerView } from '../../api/managers'
import { listSubscribers } from '../../api/subscribers'
import { SERVICE_TYPES } from '../../api/types'
import type { BulkFilter, Profile, Subscriber, SubscriberStatus } from '../../api/types'
import { useAuth } from '../../auth/AuthContext'
import { Button } from '../../components/Button'
import { PageHeader } from '../../components/PageHeader'
import { Checkbox, Select } from '../../components/form'
import { useAsync } from '../../hooks/useAsync'
import { usePaginated } from '../../hooks/usePaginated'
import { BulkBar } from './BulkBar'
import { SubscriberFormModal } from './SubscriberFormModal'

const STATUSES: SubscriberStatus[] = ['active', 'expired', 'disabled']
const ALL_COLUMNS = [
  'username',
  'name',
  'phone',
  'email',
  'status',
  'profile',
  'expiry',
  'owner',
] as const
type Column = (typeof ALL_COLUMNS)[number]

interface Filters {
  status: string
  profileId: string
  ownerId: string
  expiringDays: string
  /** FR-61/63: how hotspot-only accounts are found in the ONE unified list. */
  serviceType: string
}

const EMPTY: Filters = {
  status: '',
  profileId: '',
  ownerId: '',
  expiringDays: '',
  serviceType: '',
}

/**
 * User list (FR-1/4). The browse view is cursor-paginated over all subscribers;
 * status/profile/owner/expiring filters narrow the *loaded* rows client-side
 * (the C7-D list endpoint is unfiltered — server-side filtering lives on the
 * bulk endpoint, which the bulk bar uses so its actions cover the whole match
 * set, not just what is on screen). Global text search is the top-bar '/' box.
 */
export function UserListPage() {
  const t = useT()
  const { formatDate } = useFormatters()
  const { can } = useAuth()

  const [filters, setFilters] = useState<Filters>(EMPTY)
  const [columns, setColumns] = useState<Set<Column>>(
    () => new Set<Column>(['username', 'name', 'phone', 'status', 'profile', 'expiry']),
  )
  const [showBulk, setShowBulk] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  // Ticked rows. A bulk action runs against these when any are ticked, and
  // against the server-side filter otherwise — see BulkBar.
  const [selected, setSelected] = useState<Set<string>>(() => new Set())

  const profilesQ = useAsync(() => listProfiles(true), [])
  // Managers list is admin-only; a 403 for operators just hides owner controls.
  const managersQ = useAsync(() => listManagers().catch(() => ({ items: [] as ManagerView[] })), [])
  const profiles: Profile[] = useMemo(() => profilesQ.data?.items ?? [], [profilesQ.data])
  const managers: ManagerView[] = useMemo(() => managersQ.data?.items ?? [], [managersQ.data])

  const page = usePaginated<Subscriber>(
    (cursor) => listSubscribers({ cursor, limit: 50 }),
    'subscribers',
  )

  const profileName = useMemo(() => {
    const map = new Map(profiles.map((p) => [p.id, p.name]))
    return (id: string | null) => (id ? (map.get(id) ?? id) : '—')
  }, [profiles])
  const ownerName = useMemo(() => {
    const map = new Map(managers.map((m) => [m.id, m.username]))
    return (id: string | null) => (id ? (map.get(id) ?? id) : '—')
  }, [managers])

  const filtered = useMemo(() => applyFilters(page.items, filters), [page.items, filters])

  const bulkFilter: BulkFilter = useMemo(() => {
    const f: BulkFilter = {}
    if (filters.status) f.status = filters.status
    if (filters.profileId) f.profile_id = filters.profileId
    if (filters.ownerId) f.owner_manager_id = filters.ownerId
    // Must mirror the client-side filter: a bulk action runs against THIS
    // filter server-side, so omitting it here would apply the action to every
    // service type while the operator is looking at one.
    if (filters.serviceType) f.service_type = filters.serviceType
    if (filters.expiringDays) {
      const d = new Date()
      d.setDate(d.getDate() + Number(filters.expiringDays))
      f.expiring_before = d.toISOString()
    }
    return f
  }, [filters])

  const activeChips = useMemo(
    () => buildChips(filters, profileName, ownerName, t),
    [filters, profileName, ownerName, t],
  )

  const toggleColumn = (col: Column) =>
    setColumns((prev) => {
      const next = new Set(prev)
      if (next.has(col)) next.delete(col)
      else next.add(col)
      return next
    })

  const has = (c: Column) => columns.has(c)

  const toggleSelected = (id: string) =>
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })

  // Select-all covers the rows actually on screen, not the whole match set —
  // ticking a box must never silently enlist rows the operator cannot see.
  const allShownSelected = filtered.length > 0 && filtered.every((s) => selected.has(s.id))
  const toggleAllShown = () =>
    setSelected((prev) => {
      const next = new Set(prev)
      if (allShownSelected) filtered.forEach((s) => next.delete(s.id))
      else filtered.forEach((s) => next.add(s.id))
      return next
    })

  return (
    <section>
      <PageHeader
        title={t('nav.subscribers')}
        actions={
          <>
            {can('subscribers.view') && (
              <Button variant="secondary" size="sm" onClick={() => setShowBulk((s) => !s)}>
                {t('users.bulkToggle')}
              </Button>
            )}
            {can('subscribers.create') && (
              <Button size="sm" onClick={() => setCreateOpen(true)}>
                {t('users.create')}
              </Button>
            )}
          </>
        }
      />

      {/* Filters */}
      <div className="mb-3 flex flex-wrap items-end gap-3 rounded-md border border-surface-sunken bg-surface-raised p-3">
        <label className="text-sm">
          <span className="mb-1 block text-xs text-ink-muted">{t('users.filterStatus')}</span>
          <Select
            value={filters.status}
            onChange={(e) => setFilters({ ...filters, status: e.target.value })}
          >
            <option value="">{t('ui.all')}</option>
            {STATUSES.map((s) => (
              <option key={s} value={s}>
                {t(`common.status.${s}`)}
              </option>
            ))}
          </Select>
        </label>
        <label className="text-sm">
          <span className="mb-1 block text-xs text-ink-muted">{t('users.filterServiceType')}</span>
          <Select
            value={filters.serviceType}
            onChange={(e) => setFilters({ ...filters, serviceType: e.target.value })}
          >
            <option value="">{t('ui.all')}</option>
            {SERVICE_TYPES.map((v) => (
              <option key={v} value={v}>
                {t(`serviceType.${v}`)}
              </option>
            ))}
          </Select>
        </label>
        <label className="text-sm">
          <span className="mb-1 block text-xs text-ink-muted">{t('users.filterProfile')}</span>
          <Select
            value={filters.profileId}
            onChange={(e) => setFilters({ ...filters, profileId: e.target.value })}
          >
            <option value="">{t('ui.all')}</option>
            {profiles.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </Select>
        </label>
        {managers.length > 0 && (
          <label className="text-sm">
            <span className="mb-1 block text-xs text-ink-muted">{t('users.filterOwner')}</span>
            <Select
              value={filters.ownerId}
              onChange={(e) => setFilters({ ...filters, ownerId: e.target.value })}
            >
              <option value="">{t('ui.all')}</option>
              {managers.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.username}
                </option>
              ))}
            </Select>
          </label>
        )}
        <label className="text-sm">
          <span className="mb-1 block text-xs text-ink-muted">{t('users.filterExpiring')}</span>
          <Select
            value={filters.expiringDays}
            onChange={(e) => setFilters({ ...filters, expiringDays: e.target.value })}
          >
            <option value="">{t('ui.any')}</option>
            <option value="3">{t('users.expiring.d3')}</option>
            <option value="7">{t('users.expiring.d7')}</option>
            <option value="30">{t('users.expiring.d30')}</option>
          </Select>
        </label>
        <ColumnChooser columns={columns} onToggle={toggleColumn} />
      </div>

      {/* Active-filter chips */}
      {activeChips.length > 0 && (
        <div className="mb-3 flex flex-wrap items-center gap-2">
          {activeChips.map((chip) => (
            <span
              key={chip.key}
              className="inline-flex items-center gap-1.5 rounded-full bg-brand-soft px-2.5 py-1 text-xs text-brand-strong"
            >
              {chip.label}
              <button
                type="button"
                aria-label={t('ui.remove')}
                onClick={() => setFilters((f) => ({ ...f, [chip.key]: '' }))}
              >
                <span aria-hidden="true">×</span>
              </button>
            </span>
          ))}
          <button
            type="button"
            className="text-xs text-ink-muted underline"
            onClick={() => setFilters(EMPTY)}
          >
            {t('users.clearFilters')}
          </button>
        </div>
      )}

      {showBulk && (
        <div className="mb-3">
          <BulkBar
            filter={bulkFilter}
            selectedIds={[...selected]}
            profiles={profiles}
            managers={managers}
            onDone={() => {
              setSelected(new Set())
              page.reset()
            }}
          />
        </div>
      )}

      {/* Table */}
      {page.error ? (
        <ErrorState onRetry={page.reset} />
      ) : (
        <div className="overflow-x-auto rounded-md border border-surface-sunken">
          <table className="w-full min-w-[640px] text-sm">
            <thead className="bg-surface-raised text-start text-xs uppercase tracking-wide text-ink-muted">
              <tr>
                {showBulk && (
                  <Th>
                    <Checkbox
                      checked={allShownSelected}
                      onChange={toggleAllShown}
                      aria-label={t('users.selectAllShown')}
                    />
                  </Th>
                )}
                {has('username') && <Th>{t('subscriber.username')}</Th>}
                {has('name') && <Th>{t('subscriber.name')}</Th>}
                {has('phone') && <Th>{t('subscriber.phone')}</Th>}
                {has('email') && <Th>{t('subscriber.email')}</Th>}
                {has('status') && <Th>{t('subscriber.status')}</Th>}
                {has('profile') && <Th>{t('subscriber.profile')}</Th>}
                {has('expiry') && <Th>{t('subscriber.expiry')}</Th>}
                {has('owner') && <Th>{t('subscriber.owner')}</Th>}
              </tr>
            </thead>
            <tbody>
              {filtered.map((s) => (
                <tr key={s.id} className="border-t border-surface-sunken hover:bg-surface-raised">
                  {showBulk && (
                    <Td>
                      <Checkbox
                        checked={selected.has(s.id)}
                        onChange={() => toggleSelected(s.id)}
                        aria-label={t('users.selectRow', { username: s.username })}
                      />
                    </Td>
                  )}
                  {has('username') && (
                    <Td>
                      <Link to={`/subscribers/${s.id}`} className="font-medium text-brand-strong">
                        <Ltr>{s.username}</Ltr>
                      </Link>
                    </Td>
                  )}
                  {has('name') && <Td>{s.name ?? '—'}</Td>}
                  {has('phone') && <Td>{s.phone ? <Ltr>{s.phone}</Ltr> : '—'}</Td>}
                  {has('email') && <Td>{s.email ? <Ltr>{s.email}</Ltr> : '—'}</Td>}
                  {has('status') && (
                    <Td>
                      <StatusBadge status={s.status} />
                    </Td>
                  )}
                  {has('profile') && <Td>{profileName(s.profile_id)}</Td>}
                  {has('expiry') && (
                    <Td>
                      {s.expires_at ? formatDate(s.expires_at, { timeStyle: undefined }) : '—'}
                    </Td>
                  )}
                  {has('owner') && <Td>{ownerName(s.owner_manager_id)}</Td>}
                </tr>
              ))}
            </tbody>
          </table>
          {page.loading && filtered.length === 0 ? (
            <LoadingState />
          ) : filtered.length === 0 ? (
            <p className="p-8 text-center text-sm text-ink-muted">{t('users.empty')}</p>
          ) : null}
        </div>
      )}

      <div className="mt-4 flex items-center justify-center gap-3">
        {page.hasMore && (
          <Button variant="secondary" size="sm" disabled={page.loading} onClick={page.loadMore}>
            {page.loading ? t('common.loading') : t('ui.loadMore')}
          </Button>
        )}
      </div>

      <SubscriberFormModal
        open={createOpen}
        onOpenChange={setCreateOpen}
        profiles={profiles}
        managers={managers}
        onSaved={() => {
          setCreateOpen(false)
          page.reset()
        }}
      />
    </section>
  )
}

function Th({ children }: { children: React.ReactNode }) {
  return <th className="whitespace-nowrap px-3 py-2 text-start font-medium">{children}</th>
}
function Td({ children }: { children: React.ReactNode }) {
  return <td className="whitespace-nowrap px-3 py-2">{children}</td>
}

function ColumnChooser({
  columns,
  onToggle,
}: {
  columns: Set<Column>
  onToggle: (c: Column) => void
}) {
  const t = useT()
  const [open, setOpen] = useState(false)
  return (
    <div className="relative ms-auto">
      <Button variant="secondary" size="sm" onClick={() => setOpen((o) => !o)}>
        {t('users.columns')}
      </Button>
      {open && (
        <div className="absolute end-0 top-full z-20 mt-1 w-44 rounded-md border border-surface-sunken bg-surface-raised p-2 shadow-lg">
          {ALL_COLUMNS.map((c) => (
            <Checkbox
              key={c}
              label={t(`subscriber.${c === 'expiry' ? 'expiry' : c}`)}
              checked={columns.has(c)}
              onChange={() => onToggle(c)}
            />
          ))}
        </div>
      )}
    </div>
  )
}

function applyFilters(items: Subscriber[], f: Filters): Subscriber[] {
  let out = items
  if (f.status) out = out.filter((s) => s.status === f.status)
  if (f.profileId) out = out.filter((s) => s.profile_id === f.profileId)
  if (f.ownerId) out = out.filter((s) => s.owner_manager_id === f.ownerId)
  if (f.serviceType) out = out.filter((s) => s.service_type === f.serviceType)
  if (f.expiringDays) {
    const cutoff = new Date()
    cutoff.setDate(cutoff.getDate() + Number(f.expiringDays))
    out = out.filter((s) => s.expires_at !== null && new Date(s.expires_at) <= cutoff)
  }
  return out
}

function buildChips(
  f: Filters,
  profileName: (id: string | null) => string,
  ownerName: (id: string | null) => string,
  t: (k: string, v?: Record<string, string | number>) => string,
): { key: keyof Filters; label: string }[] {
  const chips: { key: keyof Filters; label: string }[] = []
  if (f.status) chips.push({ key: 'status', label: t(`common.status.${f.status}`) })
  if (f.profileId) chips.push({ key: 'profileId', label: profileName(f.profileId) })
  if (f.ownerId) chips.push({ key: 'ownerId', label: ownerName(f.ownerId) })
  if (f.serviceType) chips.push({ key: 'serviceType', label: t(`serviceType.${f.serviceType}`) })
  if (f.expiringDays)
    chips.push({ key: 'expiringDays', label: t('users.chipExpiring', { days: f.expiringDays }) })
  return chips
}
