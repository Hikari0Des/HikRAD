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
import { Checkbox } from '../../components/form'
import { FilterBar, type FilterField } from '../../components/patterns'
import { useAsync } from '../../hooks/useAsync'
import { useDebouncedValue } from '../../hooks/useDebouncedValue'
import { usePaginated } from '../../hooks/usePaginated'
import { usePersistentState } from '../../hooks/usePersistentState'
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
  /** FR-98.1: text search — sent to the server (all subscribers), with a client
   *  narrowing over loaded rows for instant feedback during the debounce. */
  q: string
  status: string
  profileId: string
  ownerId: string
  expiringDays: string
  /** FR-61/63: how hotspot-only accounts are found in the ONE unified list. */
  serviceType: string
}

const EMPTY: Filters = {
  q: '',
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

  // Remembered per manager until cleared (item 7).
  const [filters, setFilters] = usePersistentState<Filters>('subscribers.filters', EMPTY)
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

  // The list refetches from the server on filter change (debounced so typing in
  // the search box doesn't fire a request per keystroke), so a search matches
  // across ALL subscribers and paginates the matches — not just the loaded page.
  const debouncedFilters = useDebouncedValue(filters, 300)
  const page = usePaginated<Subscriber>(
    (cursor) =>
      listSubscribers({
        cursor,
        limit: 50,
        q: debouncedFilters.q.trim() || undefined,
        status: debouncedFilters.status || undefined,
        service_type: debouncedFilters.serviceType || undefined,
        profile_id: debouncedFilters.profileId || undefined,
        owner_manager_id: debouncedFilters.ownerId || undefined,
        expiring_days: debouncedFilters.expiringDays || undefined,
      }),
    `subscribers:${JSON.stringify(debouncedFilters)}`,
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
    // Every filter the FilterBar exposes must mirror to the server-side bulk
    // filter: a bulk action runs against THIS filter, so omitting one would
    // apply the action to rows the operator can't see. `q` maps to the server's
    // subscriber_fold ILIKE (same username/name match as the client filter).
    if (filters.q) f.q = filters.q
    if (filters.status) f.status = filters.status
    if (filters.profileId) f.profile_id = filters.profileId
    if (filters.ownerId) f.owner_manager_id = filters.ownerId
    if (filters.serviceType) f.service_type = filters.serviceType
    if (filters.expiringDays) {
      const d = new Date()
      d.setDate(d.getDate() + Number(filters.expiringDays))
      f.expiring_before = d.toISOString()
    }
    return f
  }, [filters])

  // FR-98.1: the chip FilterBar's field catalogue, built from the loaded
  // reference data. `q` (search) is handled by the bar's own search box.
  const filterFields: FilterField[] = useMemo(() => {
    const f: FilterField[] = [
      {
        key: 'status',
        label: t('users.filterStatus'),
        options: STATUSES.map((s) => ({ value: s, label: t(`common.status.${s}`) })),
      },
      {
        key: 'serviceType',
        label: t('users.filterServiceType'),
        options: SERVICE_TYPES.map((v) => ({ value: v, label: t(`serviceType.${v}`) })),
      },
      {
        key: 'profileId',
        label: t('users.filterProfile'),
        options: profiles.map((p) => ({ value: p.id, label: p.name })),
      },
      {
        key: 'expiringDays',
        label: t('users.filterExpiring'),
        options: [
          { value: '3', label: t('users.expiring.d3') },
          { value: '7', label: t('users.expiring.d7') },
          { value: '30', label: t('users.expiring.d30') },
        ],
      },
    ]
    if (managers.length > 0) {
      f.push({
        key: 'ownerId',
        label: t('users.filterOwner'),
        options: managers.map((m) => ({ value: m.id, label: m.username })),
      })
    }
    return f
  }, [t, profiles, managers])

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
              <Link to="/import">
                <Button variant="secondary" size="sm">
                  {t('users.importCsv')}
                </Button>
              </Link>
            )}
            {can('subscribers.create') && (
              <Button size="sm" onClick={() => setCreateOpen(true)}>
                {t('users.create')}
              </Button>
            )}
          </>
        }
      />

      {/* FR-98.1: chip FilterBar (search + add-a-filter → removable chips)
          replaces the old row of bare selects. Persistence + bulk parity are
          unchanged — `filters` is still the per-manager usePersistentState. */}
      <div className="mb-3 flex items-start gap-2">
        <div className="min-w-0 flex-1">
          <FilterBar
            search={filters.q}
            onSearchChange={(q) => setFilters((f) => ({ ...f, q }))}
            searchPlaceholder={t('users.searchName')}
            fields={filterFields}
            values={filters as unknown as Record<string, string>}
            onChange={(v) => setFilters({ ...EMPTY, ...(v as Partial<Filters>) })}
          />
        </div>
        <ColumnChooser columns={columns} onToggle={toggleColumn} />
      </div>

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
  if (f.q) {
    const q = f.q.trim().toLowerCase()
    out = out.filter(
      (s) => s.username.toLowerCase().includes(q) || (s.name ?? '').toLowerCase().includes(q),
    )
  }
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
