import { useState } from 'react'

import { Ltr, ErrorState, LoadingState, useT } from '@hikrad/shared'

import { adoptService, deleteNas, listNas, probeNas } from '../../api/nas'
import { ApiError } from '../../api/client'
import type { Nas, NasService } from '../../api/types'
import { useAuth } from '../../auth/AuthContext'
import { Button } from '../../components/Button'
import { Modal } from '../../components/Modal'
import { serviceLabel } from '../../components/NasScopePicker'
import { PageHeader } from '../../components/PageHeader'
import { useToast } from '../../components/Toast'
import { useAsync } from '../../hooks/useAsync'
import { DiscoverModal } from './DiscoverModal'
import { NasAutoSetupModal } from './NasAutoSetupModal'
import { NasWizardModal, type NasPrefill } from './NasWizardModal'
import { ServiceProvisionModal } from './ServiceProvisionModal'
import { SnippetModal } from './SnippetModal'

/** NAS device management (FR-13/14) — persona Ali's copy-paste onboarding. */
export function NasListPage() {
  const t = useT()
  const { can } = useAuth()
  const { toast } = useToast()
  const { data, error, loading, reload } = useAsync(() => listNas(), [])

  const [wizardOpen, setWizardOpen] = useState(false)
  const [editing, setEditing] = useState<Nas | undefined>(undefined)
  const [prefill, setPrefill] = useState<NasPrefill | undefined>(undefined)
  const [discoverOpen, setDiscoverOpen] = useState(false)
  const [snippetFor, setSnippetFor] = useState<Nas | null>(null)
  const [autoSetupFor, setAutoSetupFor] = useState<Nas | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<Nas | null>(null)
  const [deleteNeedsConfirm, setDeleteNeedsConfirm] = useState(false)
  const [deleteBusy, setDeleteBusy] = useState(false)
  const [probing, setProbing] = useState<string | null>(null)
  // v2 phase 2 / FR-67: server management modal state.
  const [serviceModalNas, setServiceModalNas] = useState<Nas | null>(null)
  const [serviceModalEditing, setServiceModalEditing] = useState<NasService | undefined>(undefined)
  const [adoptTarget, setAdoptTarget] = useState<{ nas: Nas; service: NasService } | null>(null)
  const [adopting, setAdopting] = useState(false)

  const canEdit = can('nas.edit')
  const canCreate = can('nas.create')
  const canDelete = can('nas.delete')

  function openCreate(pf?: NasPrefill) {
    setEditing(undefined)
    setPrefill(pf)
    setWizardOpen(true)
  }
  function openEdit(n: Nas) {
    setEditing(n)
    setPrefill(undefined)
    setWizardOpen(true)
  }

  function closeDelete() {
    setDeleteTarget(null)
    setDeleteNeedsConfirm(false)
  }

  /** Item 8: read the live RouterOS version over the router's API. */
  async function runProbe(n: Nas) {
    setProbing(n.id)
    try {
      const res = await probeNas(n.id)
      toast(t('nas.probeDone', { version: res.ros_version }), 'ok')
      reload()
    } catch (err) {
      if (err instanceof ApiError && err.code === 'no_api_credentials') {
        toast(t('nas.probeNoCreds'), 'danger')
      } else if (err instanceof ApiError && err.code === 'router_unreachable') {
        toast(t('nas.probeUnreachable'), 'danger')
      } else {
        toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
      }
    } finally {
      setProbing(null)
    }
  }

  async function runDelete() {
    const n = deleteTarget
    if (!n) return
    setDeleteBusy(true)
    try {
      // First attempt without force; a NAS with live sessions comes back as
      // confirmation_required, and we re-ask with the "delete + mark stale" note.
      await deleteNas(n.id, deleteNeedsConfirm)
      toast(t('nas.deleted'), 'ok')
      closeDelete()
      reload()
    } catch (err) {
      if (err instanceof ApiError && err.code === 'confirmation_required') {
        setDeleteNeedsConfirm(true)
      } else {
        toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
        closeDelete()
      }
    } finally {
      setDeleteBusy(false)
    }
  }

  /** FR-67.5: writes nothing to the router, only flips management_mode — and
   * only after this explicit confirm action. */
  async function runAdopt() {
    if (!adoptTarget) return
    setAdopting(true)
    try {
      await adoptService(adoptTarget.nas.id, adoptTarget.service.id)
      toast(t('nas.serverMgmt.adoptOk'), 'ok')
      setAdoptTarget(null)
      reload()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    } finally {
      setAdopting(false)
    }
  }

  return (
    <section>
      <PageHeader
        title={t('nav.nas')}
        actions={
          <>
            {canCreate && (
              <Button variant="secondary" size="sm" onClick={() => setDiscoverOpen(true)}>
                {t('nas.discover')}
              </Button>
            )}
            {canCreate && (
              <Button size="sm" onClick={() => openCreate()}>
                {t('nas.add')}
              </Button>
            )}
          </>
        }
      />

      {error ? (
        <ErrorState onRetry={reload} />
      ) : loading ? (
        <LoadingState />
      ) : (data?.items.length ?? 0) === 0 ? (
        <div className="rounded-md border border-dashed border-surface-sunken bg-surface-raised p-10 text-center">
          <p className="font-medium">{t('nas.emptyTitle')}</p>
          <p className="mt-1 text-sm text-ink-muted">{t('nas.emptyBody')}</p>
          {canCreate && (
            <div className="mt-4 flex justify-center gap-2">
              <Button size="sm" onClick={() => openCreate()}>
                {t('nas.add')}
              </Button>
              <Button variant="secondary" size="sm" onClick={() => setDiscoverOpen(true)}>
                {t('nas.discover')}
              </Button>
            </div>
          )}
        </div>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {data?.items.map((n) => (
            <div
              key={n.id}
              className="rounded-lg border border-surface-sunken bg-surface-raised p-4"
            >
              <div className="flex items-start justify-between gap-2">
                <div className="min-w-0">
                  <h3 className="truncate font-semibold">{n.name}</h3>
                  <p className="text-xs text-ink-muted">
                    <Ltr>{n.ip}</Ltr>
                  </p>
                </div>
                {/* Health color slot arrives Phase 3; enabled/disabled for now. */}
                <span
                  className={`rounded-full px-2 py-0.5 text-xs ${
                    n.enabled ? 'bg-ok/12 text-ok' : 'bg-ink-muted/15 text-ink-muted'
                  }`}
                >
                  {n.enabled ? t('nas.enabledTag') : t('nas.disabledTag')}
                </span>
              </div>
              {/* FR-63 services sub-list: which instances this router runs,
                  each with its own enabled state and live-session count. */}
              <ul className="mt-3 space-y-1">
                {n.services.map((s) => (
                  <li
                    key={s.id}
                    className="flex items-center justify-between gap-2 rounded bg-surface-sunken/40 px-2 py-1 text-xs"
                  >
                    <span className="min-w-0 truncate">
                      {serviceLabel(s.label, s.ros_server_name, t(`nas.typeName.${s.service}`))}
                    </span>
                    <span className="flex shrink-0 items-center gap-2">
                      <span className="text-ink-muted">
                        {t('nas.serviceSessions', { count: s.live_sessions })}
                      </span>
                      {!s.enabled && (
                        <span className="rounded-full bg-ink-muted/15 px-1.5 text-ink-muted">
                          {t('nas.disabledTag')}
                        </span>
                      )}
                      {/* v2 phase 2 / FR-67: management-mode badge + the
                          mode-appropriate action. */}
                      <span
                        className={`rounded-full px-1.5 ${
                          s.management_mode === 'system'
                            ? 'bg-brand/15 text-brand-strong'
                            : 'bg-ink-muted/15 text-ink-muted'
                        }`}
                      >
                        {s.management_mode === 'system'
                          ? t('nas.serverMgmt.modeSystem')
                          : t('nas.serverMgmt.modeRouter')}
                      </span>
                      {canEdit &&
                        (s.management_mode === 'system' ? (
                          <button
                            type="button"
                            className="text-brand-strong hover:underline"
                            onClick={() => {
                              setServiceModalNas(n)
                              setServiceModalEditing(s)
                            }}
                          >
                            {t('ui.edit')}
                          </button>
                        ) : (
                          <button
                            type="button"
                            className="text-brand-strong hover:underline"
                            onClick={() => setAdoptTarget({ nas: n, service: s })}
                          >
                            {t('nas.serverMgmt.adoptButton')}
                          </button>
                        ))}
                    </span>
                  </li>
                ))}
              </ul>
              {canEdit && (
                <Button
                  size="sm"
                  variant="ghost"
                  className="mt-1"
                  onClick={() => {
                    setServiceModalNas(n)
                    setServiceModalEditing(undefined)
                  }}
                >
                  {t('nas.serverMgmt.addServer')}
                </Button>
              )}
              <dl className="mt-3 space-y-1 text-xs text-ink-muted">
                {n.location ? (
                  <div className="flex justify-between gap-2">
                    <dt>{t('nas.location')}</dt>
                    <dd className="truncate text-ink">{n.location}</dd>
                  </div>
                ) : null}
                <div className="flex justify-between gap-2">
                  <dt>{t('nas.rosVersion')}</dt>
                  <dd className="flex items-center gap-2 text-ink">
                    {n.ros_version ?? t('ui.unknown')}
                    {canEdit && (
                      <button
                        type="button"
                        disabled={probing === n.id}
                        onClick={() => runProbe(n)}
                        className="text-xs text-brand-strong hover:underline disabled:opacity-60"
                      >
                        {probing === n.id ? t('ui.working') : t('nas.probe')}
                      </button>
                    )}
                  </dd>
                </div>
                <div className="flex justify-between gap-2">
                  <dt>{t('nas.lastSeen')}</dt>
                  {/* Real last-seen wiring is Phase 3; Test in the snippet modal
                      checks it on demand. */}
                  <dd className="text-ink">{t('nas.lastSeenSlot')}</dd>
                </div>
              </dl>
              <div className="mt-3 flex flex-wrap gap-2">
                <Button size="sm" variant="secondary" onClick={() => setSnippetFor(n)}>
                  {t('nas.snippet')}
                </Button>
                {canEdit && (
                  <Button size="sm" variant="secondary" onClick={() => setAutoSetupFor(n)}>
                    {t('nas.autoSetup.button')}
                  </Button>
                )}
                {canEdit && (
                  <Button size="sm" variant="ghost" onClick={() => openEdit(n)}>
                    {t('ui.edit')}
                  </Button>
                )}
                {canDelete && (
                  <Button size="sm" variant="ghost" onClick={() => setDeleteTarget(n)}>
                    {t('ui.delete')}
                  </Button>
                )}
              </div>
            </div>
          ))}
        </div>
      )}

      <NasWizardModal
        open={wizardOpen}
        onOpenChange={setWizardOpen}
        existing={editing}
        prefill={prefill}
        onSaved={() => {
          setWizardOpen(false)
          reload()
        }}
      />

      <DiscoverModal
        open={discoverOpen}
        onOpenChange={setDiscoverOpen}
        onPick={(pf) => {
          setDiscoverOpen(false)
          openCreate(pf)
        }}
      />

      {snippetFor && (
        <SnippetModal
          nas={snippetFor}
          open={snippetFor !== null}
          onOpenChange={(o) => !o && setSnippetFor(null)}
        />
      )}

      {autoSetupFor && (
        <NasAutoSetupModal
          nas={autoSetupFor}
          open={autoSetupFor !== null}
          onOpenChange={(o) => !o && setAutoSetupFor(null)}
          onUseSnippet={() => {
            const n = autoSetupFor
            setAutoSetupFor(null)
            setSnippetFor(n)
          }}
        />
      )}

      <Modal
        open={deleteTarget !== null}
        onOpenChange={(o) => !o && !deleteBusy && closeDelete()}
        title={t('nas.deleteTitle')}
      >
        <p className="text-sm text-ink-muted">
          {deleteNeedsConfirm ? t('nas.deleteLiveBody') : t('nas.deleteBody')}
        </p>
        <div className="mt-6 flex justify-end gap-2">
          <Button variant="ghost" disabled={deleteBusy} onClick={closeDelete}>
            {t('ui.cancel')}
          </Button>
          <Button variant="danger" disabled={deleteBusy} onClick={runDelete}>
            {deleteBusy
              ? t('ui.working')
              : deleteNeedsConfirm
                ? t('nas.deleteAnyway')
                : t('ui.delete')}
          </Button>
        </div>
      </Modal>

      {serviceModalNas && (
        <ServiceProvisionModal
          nas={serviceModalNas}
          editing={serviceModalEditing}
          open={serviceModalNas !== null}
          onOpenChange={(o) => {
            if (!o) {
              setServiceModalNas(null)
              setServiceModalEditing(undefined)
            }
          }}
          onSaved={() => {
            setServiceModalNas(null)
            setServiceModalEditing(undefined)
            reload()
          }}
        />
      )}

      <Modal
        open={adoptTarget !== null}
        onOpenChange={(o) => !o && !adopting && setAdoptTarget(null)}
        title={t('nas.serverMgmt.adoptTitle')}
      >
        <p className="text-sm text-ink-muted">{t('nas.serverMgmt.adoptHint')}</p>
        <div className="mt-6 flex justify-end gap-2">
          <Button variant="ghost" disabled={adopting} onClick={() => setAdoptTarget(null)}>
            {t('ui.cancel')}
          </Button>
          <Button disabled={adopting} onClick={() => void runAdopt()}>
            {adopting ? t('ui.working') : t('nas.serverMgmt.adoptConfirm')}
          </Button>
        </div>
      </Modal>
    </section>
  )
}
