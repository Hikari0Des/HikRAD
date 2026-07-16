import { useEffect, useState } from 'react'

import { IQDAmount, ErrorState, LoadingState, useFormatters, useT } from '@hikrad/shared'

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
  Profile,
  ProfileWrite,
  QuotaBehavior,
  QuotaMode,
} from '../../api/types'
import { useAuth } from '../../auth/AuthContext'
import { Button } from '../../components/Button'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { Modal } from '../../components/Modal'
import { NasScopePicker } from '../../components/NasScopePicker'
import { Field, Select, TextInput } from '../../components/form'
import { useToast } from '../../components/Toast'
import { useAsync } from '../../hooks/useAsync'
import { formatKbps } from '../../lib/units'

const QUOTA_MODES: QuotaMode[] = ['unlimited', 'total', 'split']
const EXPIRY: ExpiryBehavior[] = ['block', 'expired_pool']
const QUOTA_BEH: QuotaBehavior[] = ['block', 'throttle', 'expired_pool']

/** Service profiles / plans (FR-8). Carries the FR-58.1 optional Hotspot rate. */
export function ProfilesPage() {
  const t = useT()
  const { can } = useAuth()
  const { toast } = useToast()
  const { data, error, loading, reload } = useAsync(() => listProfiles(true), [])

  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<Profile | undefined>(undefined)
  const [archiveTarget, setArchiveTarget] = useState<Profile | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<Profile | null>(null)

  const canCreate = can('profiles.create')
  const canEdit = can('profiles.edit')
  const canDelete = can('profiles.delete')

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

      {error ? (
        <ErrorState onRetry={reload} />
      ) : loading ? (
        <LoadingState />
      ) : (data?.items.length ?? 0) === 0 ? (
        <p className="rounded-md border border-dashed border-surface-sunken p-10 text-center text-sm text-ink-muted">
          {t('profiles.empty')}
        </p>
      ) : (
        <div className="overflow-x-auto rounded-md border border-surface-sunken">
          <table className="w-full min-w-[720px] text-sm">
            <thead className="bg-surface-raised text-xs uppercase tracking-wide text-ink-muted">
              <tr>
                <th className="px-3 py-2 text-start font-medium">{t('profiles.name')}</th>
                <th className="px-3 py-2 text-start font-medium">{t('profiles.price')}</th>
                <th className="px-3 py-2 text-start font-medium">{t('profiles.duration')}</th>
                <th className="px-3 py-2 text-start font-medium">{t('profiles.rate')}</th>
                <th className="px-3 py-2 text-start font-medium">{t('profiles.quota')}</th>
                <th className="px-3 py-2 text-end font-medium">{t('ui.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {data?.items.map((p) => (
                <ProfileRow
                  key={p.id}
                  profile={p}
                  canEdit={canEdit}
                  onEdit={() => {
                    setEditing(p)
                    setFormOpen(true)
                  }}
                  onArchive={() => setArchiveTarget(p)}
                  canDelete={canDelete}
                  onDelete={() => setDeleteTarget(p)}
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
    </section>
  )
}

function PageHeaderRow({ children }: { children: React.ReactNode }) {
  const t = useT()
  return (
    <div className="mb-4 flex items-center justify-between">
      <h1 className="text-xl font-semibold">{t('nav.profiles')}</h1>
      <div className="flex gap-2">{children}</div>
    </div>
  )
}

function ProfileRow({
  profile: p,
  canEdit,
  onEdit,
  onArchive,
  canDelete,
  onDelete,
}: {
  profile: Profile
  canEdit: boolean
  onEdit: () => void
  onArchive: () => void
  canDelete: boolean
  onDelete: () => void
}) {
  const t = useT()
  const { formatNumber } = useFormatters()
  return (
    <tr className={`border-t border-surface-sunken ${p.archived ? 'opacity-50' : ''}`}>
      <td className="px-3 py-2">
        <span className="font-medium">{p.name}</span>
        {p.archived && (
          <span className="ms-2 rounded bg-surface-sunken px-1.5 py-0.5 text-xs text-ink-muted">
            {t('profiles.archivedTag')}
          </span>
        )}
      </td>
      <td className="whitespace-nowrap px-3 py-2">
        <IQDAmount amount={p.price_iqd} />
      </td>
      <td className="whitespace-nowrap px-3 py-2">
        {t('profiles.days', { days: p.duration_days })}
      </td>
      <td className="whitespace-nowrap px-3 py-2" dir="ltr">
        {formatKbps(p.rate_down_kbps, formatNumber)} / {formatKbps(p.rate_up_kbps, formatNumber)}
      </td>
      <td className="whitespace-nowrap px-3 py-2">{t(`profiles.quotaMode.${p.quota_mode}`)}</td>
      <td className="whitespace-nowrap px-3 py-2 text-end">
        {canEdit && !p.archived && (
          <>
            <Button size="sm" variant="ghost" onClick={onEdit}>
              {t('ui.edit')}
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

  async function save() {
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
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label={t('profiles.name')} error={errors.name}>
          <TextInput value={f.name} onChange={(e) => set('name', e.target.value)} />
        </Field>
        <Field label={t('profiles.price')} error={errors.price_iqd}>
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
        <Field label={t('profiles.sessionLimit')} error={errors.session_limit_default}>
          <TextInput
            type="number"
            dir="ltr"
            value={f.sessionLimit}
            onChange={(e) => set('sessionLimit', e.target.value)}
          />
        </Field>
        <Field
          label={t('profiles.rateDown')}
          error={errors.rate_down_kbps}
          hint={t('profiles.kbpsHint')}
        >
          <TextInput
            type="number"
            dir="ltr"
            value={f.rateDown}
            onChange={(e) => set('rateDown', e.target.value)}
          />
        </Field>
        <Field label={t('profiles.rateUp')} error={errors.rate_up_kbps}>
          <TextInput
            type="number"
            dir="ltr"
            value={f.rateUp}
            onChange={(e) => set('rateUp', e.target.value)}
          />
        </Field>

        <Field label={t('profiles.hotspotRateDown')} hint={t('profiles.hotspotHint')}>
          <TextInput
            type="number"
            dir="ltr"
            value={f.hotspotDown}
            onChange={(e) => set('hotspotDown', e.target.value)}
          />
        </Field>
        <Field label={t('profiles.hotspotRateUp')}>
          <TextInput
            type="number"
            dir="ltr"
            value={f.hotspotUp}
            onChange={(e) => set('hotspotUp', e.target.value)}
          />
        </Field>

        {/* FR-64: every subscriber on this profile inherits this scope, unless
            their own is set (subscriber-over-profile). */}
        <NasScopePicker
          scopes={f.nasScopes}
          onChange={(nasScopes) => setF((prev) => ({ ...prev, nasScopes }))}
          errors={errors}
        />

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
            <TextInput
              dir="ltr"
              value={f.throttleRate}
              onChange={(e) => set('throttleRate', e.target.value)}
            />
          </Field>
        )}
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
  nasScopes: NasScope[]
}

function initial(p?: Profile): ProfileForm {
  return {
    name: p?.name ?? '',
    price: p != null ? String(p.price_iqd) : '0',
    duration: p != null ? String(p.duration_days) : '30',
    sessionLimit: p != null ? String(p.session_limit_default) : '1',
    rateDown: p != null ? String(p.rate_down_kbps) : '0',
    rateUp: p != null ? String(p.rate_up_kbps) : '0',
    hotspotDown: p?.hotspot_rate_down_kbps != null ? String(p.hotspot_rate_down_kbps) : '',
    hotspotUp: p?.hotspot_rate_up_kbps != null ? String(p.hotspot_rate_up_kbps) : '',
    quotaMode: p?.quota_mode ?? 'unlimited',
    quotaTotal: p?.quota_total_bytes != null ? String(p.quota_total_bytes) : '',
    quotaDown: p?.quota_down_bytes != null ? String(p.quota_down_bytes) : '',
    quotaUp: p?.quota_up_bytes != null ? String(p.quota_up_bytes) : '',
    expiryBehavior: p?.expiry_behavior ?? 'block',
    quotaBehavior: p?.quota_behavior ?? 'block',
    throttleRate: p?.throttle_rate ?? '',
    nasScopes: p?.nas_scopes ?? [],
  }
}

function numOrNull(s: string): number | null {
  return s.trim() === '' ? null : Number(s)
}

function toWrite(f: ProfileForm): ProfileWrite {
  return {
    name: f.name,
    price_iqd: Number(f.price) || 0,
    duration_days: Number(f.duration) || 1,
    rate_down_kbps: Number(f.rateDown) || 0,
    rate_up_kbps: Number(f.rateUp) || 0,
    session_limit_default: Number(f.sessionLimit) || 1,
    quota_mode: f.quotaMode,
    quota_total_bytes: f.quotaMode === 'total' ? numOrNull(f.quotaTotal) : null,
    quota_down_bytes: f.quotaMode === 'split' ? numOrNull(f.quotaDown) : null,
    quota_up_bytes: f.quotaMode === 'split' ? numOrNull(f.quotaUp) : null,
    // null (blank) = Hotspot falls back to the main rate (FR-58.1).
    hotspot_rate_down_kbps: numOrNull(f.hotspotDown),
    hotspot_rate_up_kbps: numOrNull(f.hotspotUp),
    expiry_behavior: f.expiryBehavior,
    quota_behavior: f.quotaBehavior,
    throttle_rate: f.quotaBehavior === 'throttle' ? f.throttleRate || null : null,
    // An empty set = any NAS (FR-64), which is what the picker yields when the
    // operator selects nothing.
    nas_scopes: f.nasScopes,
  }
}
