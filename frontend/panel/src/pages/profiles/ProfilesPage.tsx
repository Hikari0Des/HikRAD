import { useMemo, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'

import {
  IQDAmount,
  ErrorState,
  LoadingState,
  formatRateKbps,
  normalizeRatePair,
  parseRateKbps,
  useFormatters,
  useT,
} from '@hikrad/shared'

import {
  listCurrencies,
  listProfileCostHistory,
  setProfileCost,
  type CostHistoryEntry,
} from '../../api/billing'
import { listPools } from '../../api/pools'
import {
  archiveProfile,
  createProfile,
  deleteProfile,
  listProfiles,
  updateProfile,
} from '../../api/profiles'
import { ApiError } from '../../api/client'
import type {
  ExpiryBehavior,
  NasScope,
  Pool,
  Profile,
  ProfileBulkFilter,
  ProfileWrite,
  QuotaBehavior,
  QuotaMode,
} from '../../api/types'
import { useAuth } from '../../auth/AuthContext'
import { PERM_PROFILES_CREATE } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { Modal } from '../../components/Modal'
import { NasScopePicker } from '../../components/NasScopePicker'
import { Checkbox, Field, RateInput, Select, TextInput } from '../../components/form'
import { FilterBar, type FilterField } from '../../components/patterns'
import { Pill } from '../../components/Pill'
import { useToast } from '../../components/Toast'
import { useAsync } from '../../hooks/useAsync'
import { usePersistentState } from '../../hooks/usePersistentState'
import { FormSection } from '../../components/patterns'
import { formatKbps } from '../../lib/units'
import { ProfileBulkBar } from './ProfileBulkBar'

const QUOTA_MODES: QuotaMode[] = ['unlimited', 'total', 'split']
const EXPIRY: ExpiryBehavior[] = ['block', 'expired_pool']
const QUOTA_BEH: QuotaBehavior[] = ['block', 'throttle', 'expired_pool']

interface Filters {
  q: string
  archived: string // 'no' (default) | 'yes' | 'any'
  quotaMode: string
  expiryBehavior: string
  poolId: string
}

const EMPTY_FILTERS: Filters = {
  q: '',
  archived: 'no',
  quotaMode: '',
  expiryBehavior: '',
  poolId: '',
}

/**
 * Service profiles / plans (FR-8). Carries the FR-58.1 optional Hotspot rate.
 * v3-6 (FR-111): filter bar + multi-select bulk actions + CSV import — the
 * management parity the Subscribers page already had.
 */
export function ProfilesPage() {
  const t = useT()
  const { can } = useAuth()
  const { toast } = useToast()
  const { data, error, loading, reload } = useAsync(() => listProfiles(true), [])
  const poolsQ = useAsync(() => listPools(), [])
  const pools: Pool[] = useMemo(() => poolsQ.data?.items ?? [], [poolsQ.data])

  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<Profile | undefined>(undefined)
  const [archiveTarget, setArchiveTarget] = useState<Profile | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<Profile | null>(null)
  const [costTarget, setCostTarget] = useState<Profile | null>(null)

  // Remembered per manager until cleared (FR-111.1, same as the subscriber list).
  const [filters, setFilters] = usePersistentState<Filters>('profiles.filters', EMPTY_FILTERS)
  const [showBulk, setShowBulk] = useState(false)
  const [selected, setSelected] = useState<Set<string>>(() => new Set())

  const canCreate = can('profiles.create')
  const canEdit = can('profiles.edit')
  const canDelete = can('profiles.delete')

  const all: Profile[] = useMemo(() => data?.items ?? [], [data])

  // #6/FR-98.1: the same chip FilterBar the Subscribers board uses. `archived`
  // defaults to 'no' (hide archived) — so it is offered only as yes/any here and
  // omitted from the active values when 'no', which keeps the default from
  // showing a stuck, un-removable chip.
  const filterFields: FilterField[] = useMemo(() => {
    const f: FilterField[] = [
      {
        key: 'archived',
        label: t('profiles.filterArchived'),
        options: [
          { value: 'yes', label: t('profiles.archivedYes') },
          { value: 'any', label: t('ui.all') },
        ],
      },
      {
        key: 'quotaMode',
        label: t('profiles.quotaModeLabel'),
        options: QUOTA_MODES.map((m) => ({ value: m, label: t(`profiles.quotaMode.${m}`) })),
      },
      {
        key: 'expiryBehavior',
        label: t('profiles.expiryBehavior'),
        options: EXPIRY.map((b) => ({ value: b, label: t(`profiles.expiry.${b}`) })),
      },
    ]
    if (pools.length > 0) {
      f.push({
        key: 'poolId',
        label: t('profiles.pool'),
        options: pools.map((p) => ({ value: p.id, label: p.name })),
      })
    }
    return f
  }, [t, pools])

  const filterValues: Record<string, string> = {}
  if (filters.archived !== 'no') filterValues.archived = filters.archived
  if (filters.quotaMode) filterValues.quotaMode = filters.quotaMode
  if (filters.expiryBehavior) filterValues.expiryBehavior = filters.expiryBehavior
  if (filters.poolId) filterValues.poolId = filters.poolId

  const filtered = useMemo(() => applyFilters(all, filters), [all, filters])

  const bulkFilter: ProfileBulkFilter = useMemo(() => {
    const f: ProfileBulkFilter = {}
    if (filters.q) f.q = filters.q
    if (filters.archived === 'yes' || filters.archived === 'no') f.archived = filters.archived
    if (filters.quotaMode) f.quota_mode = filters.quotaMode
    if (filters.expiryBehavior) f.expiry_behavior = filters.expiryBehavior
    if (filters.poolId) f.pool_id = filters.poolId
    return f
  }, [filters])

  const toggleSelected = (id: string) =>
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  const allShownSelected = filtered.length > 0 && filtered.every((p) => selected.has(p.id))
  const toggleAllShown = () =>
    setSelected((prev) => {
      const next = new Set(prev)
      if (allShownSelected) filtered.forEach((p) => next.delete(p.id))
      else filtered.forEach((p) => next.add(p.id))
      return next
    })

  async function doArchive(p: Profile) {
    try {
      await archiveProfile(p.id)
      toast(t('profiles.archived'), 'ok')
      reload()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    }
  }

  /**
   * Delete a plan. The backend refuses (409 profile_in_use) anything ever sold —
   * archive is the answer there — so surface that reason rather than a generic
   * failure, because "archive it instead" is the actual next step.
   */
  async function doDelete(p: Profile) {
    try {
      await deleteProfile(p.id)
      toast(t('profiles.deleted', { name: p.name }), 'ok')
      reload()
    } catch (err) {
      if (err instanceof ApiError && err.code === 'profile_in_use') {
        toast(t('profiles.deleteInUse'), 'danger')
        return
      }
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    }
  }

  return (
    <section>
      <PageHeaderRow>
        <Button variant="secondary" size="sm" onClick={() => setShowBulk((s) => !s)}>
          {t('profiles.bulkToggle')}
        </Button>
        {can(PERM_PROFILES_CREATE) && (
          <Link to="/import/profiles">
            <Button variant="secondary" size="sm">
              {t('profiles.importCsv')}
            </Button>
          </Link>
        )}
        {canCreate && (
          <Button
            size="sm"
            onClick={() => {
              setEditing(undefined)
              setFormOpen(true)
            }}
          >
            {t('profiles.add')}
          </Button>
        )}
      </PageHeaderRow>

      {/* #6/FR-111.1: chip FilterBar (search + add-a-filter → removable chips),
          identical to the Subscribers board. */}
      <FilterBar
        search={filters.q}
        onSearchChange={(q) => setFilters((f) => ({ ...f, q }))}
        searchPlaceholder={t('profiles.filterNamePlaceholder')}
        fields={filterFields}
        values={filterValues}
        onChange={(v) =>
          setFilters({
            ...EMPTY_FILTERS,
            q: filters.q,
            archived: v.archived || 'no',
            quotaMode: v.quotaMode || '',
            expiryBehavior: v.expiryBehavior || '',
            poolId: v.poolId || '',
          })
        }
      />

      {showBulk && (
        <div className="mb-3">
          <ProfileBulkBar
            filter={bulkFilter}
            selectedIds={[...selected]}
            pools={pools}
            onDone={() => {
              setSelected(new Set())
              reload()
            }}
          />
        </div>
      )}

      {error ? (
        <ErrorState onRetry={reload} />
      ) : loading ? (
        <LoadingState />
      ) : filtered.length === 0 ? (
        <p className="rounded-md border border-dashed border-surface-sunken p-10 text-center text-sm text-ink-muted">
          {all.length === 0 ? t('profiles.empty') : t('profiles.noneMatch')}
        </p>
      ) : (
        <div className="overflow-x-auto rounded-md border border-surface-sunken">
          <table className="w-full min-w-[720px] text-sm">
            <thead className="bg-surface-raised text-xs uppercase tracking-wide text-ink-muted">
              <tr>
                {showBulk && (
                  <th className="px-3 py-2 text-start font-medium">
                    <Checkbox
                      checked={allShownSelected}
                      onChange={toggleAllShown}
                      aria-label={t('users.selectAllShown')}
                    />
                  </th>
                )}
                <th className="px-3 py-2 text-start font-medium">{t('profiles.name')}</th>
                <th className="px-3 py-2 text-start font-medium">{t('profiles.price')}</th>
                <th className="px-3 py-2 text-start font-medium">{t('profiles.duration')}</th>
                <th className="px-3 py-2 text-start font-medium">{t('profiles.rate')}</th>
                <th className="px-3 py-2 text-start font-medium">{t('profiles.quota')}</th>
                <th className="px-3 py-2 text-end font-medium">{t('ui.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((p) => (
                <ProfileRow
                  key={p.id}
                  profile={p}
                  showBulk={showBulk}
                  selected={selected.has(p.id)}
                  onToggleSelect={() => toggleSelected(p.id)}
                  canEdit={canEdit}
                  onEdit={() => {
                    setEditing(p)
                    setFormOpen(true)
                  }}
                  onArchive={() => setArchiveTarget(p)}
                  canDelete={canDelete}
                  onDelete={() => setDeleteTarget(p)}
                  onCost={() => setCostTarget(p)}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}

      <ProfileFormModal
        open={formOpen}
        onOpenChange={setFormOpen}
        existing={editing}
        onSaved={() => {
          setFormOpen(false)
          reload()
        }}
      />

      <ConfirmDialog
        open={archiveTarget !== null}
        onOpenChange={(o) => !o && setArchiveTarget(null)}
        title={t('profiles.archiveTitle')}
        body={t('profiles.archiveBody')}
        confirmLabel={t('profiles.archive')}
        onConfirm={async () => {
          if (archiveTarget) await doArchive(archiveTarget)
        }}
      />

      <ConfirmDialog
        open={deleteTarget !== null}
        onOpenChange={(o) => !o && setDeleteTarget(null)}
        title={t('profiles.deleteTitle', { name: deleteTarget?.name ?? '' })}
        body={t('profiles.deleteBody')}
        confirmLabel={t('ui.delete')}
        destructive
        onConfirm={async () => {
          if (deleteTarget) await doDelete(deleteTarget)
        }}
      />

      <ProfileCostModal profile={costTarget} onClose={() => setCostTarget(null)} />
    </section>
  )
}

function PageHeaderRow({ children }: { children: React.ReactNode }) {
  const t = useT()
  return (
    <div className="mb-4 flex items-center justify-between">
      <h1 className="text-xl font-semibold">{t('nav.profiles')}</h1>
      <div className="flex flex-wrap gap-2">{children}</div>
    </div>
  )
}

// Client-side filtering over the already-loaded plan list (FR-111.1) — the same
// posture as the subscriber list, justified by the same fact (plans number in
// the dozens, and GET /profiles is unpaginated).
function applyFilters(items: Profile[], f: Filters): Profile[] {
  let out = items
  if (f.archived === 'no') out = out.filter((p) => !p.archived)
  else if (f.archived === 'yes') out = out.filter((p) => p.archived)
  if (f.q) {
    const q = f.q.toLowerCase()
    out = out.filter((p) => p.name.toLowerCase().includes(q))
  }
  if (f.quotaMode) out = out.filter((p) => p.quota_mode === f.quotaMode)
  if (f.expiryBehavior) out = out.filter((p) => p.expiry_behavior === f.expiryBehavior)
  if (f.poolId) out = out.filter((p) => p.pool_id === f.poolId)
  return out
}

function ProfileRow({
  profile: p,
  showBulk,
  selected,
  onToggleSelect,
  canEdit,
  onEdit,
  onArchive,
  canDelete,
  onDelete,
  onCost,
}: {
  profile: Profile
  showBulk: boolean
  selected: boolean
  onToggleSelect: () => void
  canEdit: boolean
  onEdit: () => void
  onArchive: () => void
  canDelete: boolean
  onDelete: () => void
  onCost: () => void
}) {
  const t = useT()
  const { formatNumber } = useFormatters()
  return (
    <tr className={`border-t border-surface-sunken ${p.archived ? 'opacity-50' : ''}`}>
      {showBulk && (
        <td className="px-3 py-2">
          <Checkbox
            checked={selected}
            onChange={onToggleSelect}
            aria-label={t('users.selectRow', { username: p.name })}
          />
        </td>
      )}
      <td className="px-3 py-2">
        <span className="font-medium">{p.name}</span>
        {p.archived && (
          <span className="ms-2 rounded bg-surface-sunken px-1.5 py-0.5 text-xs text-ink-muted">
            {t('profiles.archivedTag')}
          </span>
        )}
      </td>
      <td className="whitespace-nowrap px-3 py-2">
        <IQDAmount amount={p.price} currency={p.currency} />
      </td>
      <td className="whitespace-nowrap px-3 py-2">
        {t('profiles.days', { days: p.duration_days })}
      </td>
      <td className="whitespace-nowrap px-3 py-2" dir="ltr">
        <span className="inline-flex gap-1.5">
          <Pill>{formatKbps(p.rate_down_kbps, formatNumber)} ↓</Pill>
          <Pill>{formatKbps(p.rate_up_kbps, formatNumber)} ↑</Pill>
        </span>
      </td>
      <td className="whitespace-nowrap px-3 py-2">
        <Pill tone="muted">{t(`profiles.quotaMode.${p.quota_mode}`)}</Pill>
      </td>
      <td className="whitespace-nowrap px-3 py-2 text-end">
        {canEdit && !p.archived && (
          <>
            <Button size="sm" variant="ghost" onClick={onEdit}>
              {t('ui.edit')}
            </Button>
            <Button size="sm" variant="ghost" onClick={onCost}>
              {t('profiles.cost')}
            </Button>
            <Button size="sm" variant="ghost" onClick={onArchive}>
              {t('profiles.archive')}
            </Button>
          </>
        )}
        {/* Delete stays available on an ARCHIVED plan too: archiving a
            never-used plan created by mistake is the likeliest way to end up
            wanting it gone, and hiding delete there would strand it. */}
        {canDelete && (
          <Button size="sm" variant="ghost" onClick={onDelete}>
            {t('ui.delete')}
          </Button>
        )}
      </td>
    </tr>
  )
}

function ProfileFormModal({
  open,
  onOpenChange,
  existing,
  onSaved,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  existing?: Profile
  onSaved: () => void
}) {
  const t = useT()
  const { toast } = useToast()
  const editing = Boolean(existing)
  const [f, setF] = useState(() => initial(existing))
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)

  const openKey = `${open}:${existing?.id ?? 'new'}`
  useEffect(() => {
    if (open) {
      setF(initial(existing))
      setErrors({})
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [openKey])

  const set = <K extends keyof ProfileForm>(k: K, v: ProfileForm[K]) =>
    setF((prev) => ({ ...prev, [k]: v }))

  // FR-98.2: the Advanced section is collapsed by default, but auto-expands when
  // editing a plan that already has advanced values set, so they aren't hidden.
  const advancedHasValues =
    editing &&
    (f.hotspotDown !== '' || f.hotspotUp !== '' || f.addressList !== '' || f.nasScopes.length > 0)

  async function save() {
    // Rate syntax is validated before anything is sent (item 5).
    const rateErrors: Record<string, string> = {}
    if (parseRateKbps(f.rateDown) === null) rateErrors.rate_down_kbps = t('rate.invalid')
    if (parseRateKbps(f.rateUp) === null) rateErrors.rate_up_kbps = t('rate.invalid')
    if (f.hotspotDown.trim() !== '' && parseRateKbps(f.hotspotDown) === null)
      rateErrors.hotspot_rate_down_kbps = t('rate.invalid')
    if (f.hotspotUp.trim() !== '' && parseRateKbps(f.hotspotUp) === null)
      rateErrors.hotspot_rate_up_kbps = t('rate.invalid')
    if (f.quotaBehavior === 'throttle' && normalizeRatePair(f.throttleRate) === null)
      rateErrors.throttle_rate = t('rate.invalid')
    if (Object.keys(rateErrors).length > 0) {
      setErrors(rateErrors)
      return
    }
    setBusy(true)
    setErrors({})
    try {
      const body = toWrite(f)
      if (editing && existing) await updateProfile(existing.id, body)
      else await createProfile(body)
      toast(editing ? t('profiles.saved') : t('profiles.created'), 'ok')
      onSaved()
    } catch (err) {
      if (err instanceof ApiError && err.fieldErrors.length > 0) {
        const map: Record<string, string> = {}
        for (const fe of err.fieldErrors) map[fe.field] = fe.message
        setErrors(map)
      } else {
        toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open={open}
      onOpenChange={busy ? () => {} : onOpenChange}
      size="lg"
      title={editing ? t('profiles.editTitle') : t('profiles.createTitle')}
    >
      {/* FR-98.2 form-ordering pass: grouped by task — identity/billing →
          service & limits → advanced (collapsed unless it already has values). */}
      <div className="space-y-5">
        <FormSection title={t('profiles.section.identity')}>
          <Field label={t('profiles.name')} error={errors.name}>
            <TextInput value={f.name} onChange={(e) => set('name', e.target.value)} />
          </Field>
          {/* v3-2 (FR-101.3): plans are IQD-only — no currency selector; price is IQD. */}
          <Field label={t('profiles.price')} error={errors.price}>
            <TextInput
              type="number"
              dir="ltr"
              value={f.price}
              onChange={(e) => set('price', e.target.value)}
            />
          </Field>
          <Field label={t('profiles.durationDays')} error={errors.duration_days}>
            <TextInput
              type="number"
              dir="ltr"
              value={f.duration}
              onChange={(e) => set('duration', e.target.value)}
            />
          </Field>
        </FormSection>

        <FormSection title={t('profiles.section.service')}>
          <Field label={t('profiles.rateDown')} error={errors.rate_down_kbps} hint={t('rate.hint')}>
            <RateInput value={f.rateDown} onChange={(v) => set('rateDown', v)} />
          </Field>
          <Field label={t('profiles.rateUp')} error={errors.rate_up_kbps}>
            <RateInput value={f.rateUp} onChange={(v) => set('rateUp', v)} />
          </Field>
          <Field label={t('profiles.sessionLimit')} error={errors.session_limit_default}>
            <TextInput
              type="number"
              dir="ltr"
              value={f.sessionLimit}
              onChange={(e) => set('sessionLimit', e.target.value)}
            />
          </Field>
          <Field label={t('profiles.quotaModeLabel')}>
            <Select
              value={f.quotaMode}
              onChange={(e) => set('quotaMode', e.target.value as QuotaMode)}
            >
              {QUOTA_MODES.map((m) => (
                <option key={m} value={m}>
                  {t(`profiles.quotaMode.${m}`)}
                </option>
              ))}
            </Select>
          </Field>
          {f.quotaMode === 'total' && (
            <Field
              label={t('profiles.quotaTotal')}
              error={errors.quota_total_bytes}
              hint={t('profiles.bytesHint')}
            >
              <TextInput
                type="number"
                dir="ltr"
                value={f.quotaTotal}
                onChange={(e) => set('quotaTotal', e.target.value)}
              />
            </Field>
          )}
          {f.quotaMode === 'split' && (
            <>
              <Field
                label={t('profiles.quotaDown')}
                error={errors.quota_down_bytes}
                hint={t('profiles.bytesHint')}
              >
                <TextInput
                  type="number"
                  dir="ltr"
                  value={f.quotaDown}
                  onChange={(e) => set('quotaDown', e.target.value)}
                />
              </Field>
              <Field label={t('profiles.quotaUp')}>
                <TextInput
                  type="number"
                  dir="ltr"
                  value={f.quotaUp}
                  onChange={(e) => set('quotaUp', e.target.value)}
                />
              </Field>
            </>
          )}
          <Field label={t('profiles.expiryBehavior')}>
            <Select
              value={f.expiryBehavior}
              onChange={(e) => set('expiryBehavior', e.target.value as ExpiryBehavior)}
            >
              {EXPIRY.map((b) => (
                <option key={b} value={b}>
                  {t(`profiles.expiry.${b}`)}
                </option>
              ))}
            </Select>
          </Field>
          <Field label={t('profiles.quotaBehavior')}>
            <Select
              value={f.quotaBehavior}
              onChange={(e) => set('quotaBehavior', e.target.value as QuotaBehavior)}
            >
              {QUOTA_BEH.map((b) => (
                <option key={b} value={b}>
                  {t(`profiles.quotaBeh.${b}`)}
                </option>
              ))}
            </Select>
          </Field>
          {f.quotaBehavior === 'throttle' && (
            <Field
              label={t('profiles.throttleRate')}
              error={errors.throttle_rate}
              hint={t('profiles.throttleHint')}
            >
              <RateInput pair value={f.throttleRate} onChange={(v) => set('throttleRate', v)} />
            </Field>
          )}
        </FormSection>

        <FormSection
          title={t('profiles.section.advanced')}
          advanced
          defaultOpen={advancedHasValues}
        >
          <Field label={t('profiles.hotspotRateDown')} hint={t('profiles.hotspotHint')}>
            <RateInput value={f.hotspotDown} onChange={(v) => set('hotspotDown', v)} />
          </Field>
          <Field label={t('profiles.hotspotRateUp')}>
            <RateInput value={f.hotspotUp} onChange={(v) => set('hotspotUp', v)} />
          </Field>
          {/* FR-64: every subscriber on this profile inherits this scope, unless
              their own is set (subscriber-over-profile). */}
          <NasScopePicker
            scopes={f.nasScopes}
            onChange={(nasScopes) => setF((prev) => ({ ...prev, nasScopes }))}
            errors={errors}
          />
          {/* FR-109: named address-list a normal login is placed into. Free text. */}
          <Field
            label={t('profiles.addressList')}
            hint={t('profiles.addressListHint')}
            htmlFor="profile-address-list"
          >
            <TextInput
              id="profile-address-list"
              dir="ltr"
              value={f.addressList}
              onChange={(e) => set('addressList', e.target.value)}
            />
          </Field>
        </FormSection>
      </div>

      <div className="mt-5 flex justify-end gap-2">
        <Button variant="ghost" disabled={busy} onClick={() => onOpenChange(false)}>
          {t('ui.cancel')}
        </Button>
        <Button disabled={busy} onClick={save}>
          {busy ? t('ui.working') : t('ui.save')}
        </Button>
      </div>
    </Modal>
  )
}

interface ProfileForm {
  name: string
  price: string
  currency: string
  duration: string
  sessionLimit: string
  rateDown: string
  rateUp: string
  hotspotDown: string
  hotspotUp: string
  quotaMode: QuotaMode
  quotaTotal: string
  quotaDown: string
  quotaUp: string
  expiryBehavior: ExpiryBehavior
  quotaBehavior: QuotaBehavior
  throttleRate: string
  addressList: string
  nasScopes: NasScope[]
}

function initial(p?: Profile): ProfileForm {
  return {
    name: p?.name ?? '',
    price: p != null ? String(p.price) : '0',
    currency: p?.currency ?? 'IQD',
    duration: p != null ? String(p.duration_days) : '30',
    sessionLimit: p != null ? String(p.session_limit_default) : '1',
    rateDown: p != null ? formatRateKbps(p.rate_down_kbps) : '',
    rateUp: p != null ? formatRateKbps(p.rate_up_kbps) : '',
    hotspotDown: p?.hotspot_rate_down_kbps != null ? formatRateKbps(p.hotspot_rate_down_kbps) : '',
    hotspotUp: p?.hotspot_rate_up_kbps != null ? formatRateKbps(p.hotspot_rate_up_kbps) : '',
    quotaMode: p?.quota_mode ?? 'unlimited',
    quotaTotal: p?.quota_total_bytes != null ? String(p.quota_total_bytes) : '',
    quotaDown: p?.quota_down_bytes != null ? String(p.quota_down_bytes) : '',
    quotaUp: p?.quota_up_bytes != null ? String(p.quota_up_bytes) : '',
    expiryBehavior: p?.expiry_behavior ?? 'block',
    quotaBehavior: p?.quota_behavior ?? 'block',
    throttleRate: p?.throttle_rate ?? '',
    addressList: p?.address_list ?? '',
    nasScopes: p?.nas_scopes ?? [],
  }
}

function numOrNull(s: string): number | null {
  return s.trim() === '' ? null : Number(s)
}

function toWrite(f: ProfileForm): ProfileWrite {
  return {
    name: f.name,
    price: Number(f.price) || 0,
    currency: f.currency,
    duration_days: Number(f.duration) || 1,
    // Prefix entry (item 5): "10M"/"1G"/bare kbit; empty or 0 = unlimited.
    rate_down_kbps: parseRateKbps(f.rateDown) ?? 0,
    rate_up_kbps: parseRateKbps(f.rateUp) ?? 0,
    session_limit_default: Number(f.sessionLimit) || 1,
    quota_mode: f.quotaMode,
    quota_total_bytes: f.quotaMode === 'total' ? numOrNull(f.quotaTotal) : null,
    quota_down_bytes: f.quotaMode === 'split' ? numOrNull(f.quotaDown) : null,
    quota_up_bytes: f.quotaMode === 'split' ? numOrNull(f.quotaUp) : null,
    // null (blank) = Hotspot falls back to the main rate (FR-58.1).
    hotspot_rate_down_kbps: f.hotspotDown.trim() === '' ? null : parseRateKbps(f.hotspotDown),
    hotspot_rate_up_kbps: f.hotspotUp.trim() === '' ? null : parseRateKbps(f.hotspotUp),
    expiry_behavior: f.expiryBehavior,
    quota_behavior: f.quotaBehavior,
    throttle_rate:
      f.quotaBehavior === 'throttle' ? normalizeRatePair(f.throttleRate) || null : null,
    address_list: f.addressList.trim() || null,
    // An empty set = any NAS (FR-64), which is what the picker yields when the
    // operator selects nothing.
    nas_scopes: f.nasScopes,
  }
}

/**
 * Plan cost price (v2 phase 9, FR-71): history + a form to record a new
 * version. Append-only — every submission is a new row, never an edit of an
 * old one (mirrors the currency-rates screen's own pattern).
 */
function ProfileCostModal({ profile, onClose }: { profile: Profile | null; onClose: () => void }) {
  const t = useT()
  const { toast } = useToast()
  const { formatDate } = useFormatters()
  const { data: currencies } = useAsync(() => listCurrencies(), [])
  const {
    data: history,
    loading,
    reload,
  } = useAsync(
    () => (profile ? listProfileCostHistory(profile.id) : Promise.resolve(null)),
    [profile?.id],
  )

  const [cost, setCost] = useState('')
  const [currency, setCurrency] = useState('IQD')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (profile) {
      setCost('')
      setCurrency(profile.currency)
    }
  }, [profile])

  async function submit() {
    if (!profile) return
    setBusy(true)
    try {
      await setProfileCost(profile.id, { cost: Number(cost), currency })
      toast(t('profiles.costSaved'), 'ok')
      setCost('')
      reload()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open={profile !== null}
      onOpenChange={(o) => !o && onClose()}
      title={t('profiles.costTitle')}
    >
      <div className="space-y-4">
        <div className="grid grid-cols-2 gap-3">
          <Field label={t('profiles.cost')} htmlFor="cost-value">
            <TextInput
              id="cost-value"
              type="number"
              dir="ltr"
              value={cost}
              onChange={(e) => setCost(e.target.value)}
            />
          </Field>
          <Field label={t('profiles.currency')} htmlFor="cost-currency">
            <Select
              id="cost-currency"
              value={currency}
              onChange={(e) => setCurrency(e.target.value)}
            >
              {(currencies?.items ?? [{ code: 'IQD' }]).map((c) => (
                <option key={c.code} value={c.code}>
                  {c.code}
                </option>
              ))}
            </Select>
          </Field>
        </div>
        <Button size="sm" disabled={busy || !cost} onClick={() => void submit()}>
          {busy ? t('ui.working') : t('profiles.costSave')}
        </Button>

        <div className="border-t border-surface-sunken pt-3">
          <p className="mb-2 text-xs font-medium text-ink-muted">{t('profiles.costHistory')}</p>
          {loading ? (
            <LoadingState />
          ) : (history?.items.length ?? 0) === 0 ? (
            <p className="text-sm text-ink-muted">{t('profiles.costEmpty')}</p>
          ) : (
            <ul className="space-y-1 text-sm">
              {(history?.items ?? []).map((h: CostHistoryEntry) => (
                <li key={h.id} className="flex items-center justify-between">
                  <IQDAmount amount={h.cost} currency={h.currency} />
                  <span className="text-xs text-ink-muted">{formatDate(h.effective_from)}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </Modal>
  )
}
