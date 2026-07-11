import { useState } from 'react'
import { useParams } from 'react-router-dom'

import { Ltr, ErrorState, LoadingState, StatusBadge, useFormatters, useT } from '@hikrad/shared'

import { disconnectSession, openLiveStream } from '../../api/live'
import { listProfiles } from '../../api/profiles'
import { listManagers, type ManagerView } from '../../api/managers'
import { getSubscriber, resetMac } from '../../api/subscribers'
import type { LiveSession, Profile, SubscriberDetail } from '../../api/types'
import { useAuth } from '../../auth/AuthContext'
import { PERM_DISCONNECT, PERM_RENEW } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { PageHeader } from '../../components/PageHeader'
import { useToast } from '../../components/Toast'
import { useAsync } from '../../hooks/useAsync'
import { AuditTrail } from './AuditTrail'
import { LiveWidget } from './LiveWidget'
import { RedeemVoucherModal } from './RedeemVoucherModal'
import { RenewModal } from './RenewModal'
import { SessionHistory } from './SessionHistory'
import { SubscriberFormModal } from './SubscriberFormModal'
import { UsagePanel } from './UsagePanel'

type Tab = 'usage' | 'history' | 'audit'

/** User-detail page (FR-3) — the screen front-desk Sara lives on. */
export function UserDetailPage() {
  const { id = '' } = useParams()
  const t = useT()
  const { can } = useAuth()
  const { toast } = useToast()

  const { data, error, loading, reload } = useAsync<SubscriberDetail>(() => getSubscriber(id), [id])
  const profilesQ = useAsync(() => listProfiles(true), [])
  const managersQ = useAsync(() => listManagers().catch(() => ({ items: [] as ManagerView[] })), [])
  const profiles: Profile[] = profilesQ.data?.items ?? []
  const managers: ManagerView[] = managersQ.data?.items ?? []

  const [tab, setTab] = useState<Tab>('usage')
  const [editOpen, setEditOpen] = useState(false)
  const [renewOpen, setRenewOpen] = useState(false)
  const [redeemOpen, setRedeemOpen] = useState(false)
  const [resetOpen, setResetOpen] = useState(false)
  const [disconnectOffer, setDisconnectOffer] = useState(false)

  if (error) return <ErrorState onRetry={reload} />
  if (loading || !data) return <LoadingState />

  const s = data.subscriber
  const canEdit = can('subscribers.edit')

  async function doResetMac() {
    try {
      await resetMac(id)
      toast(t('subscriber.macReset'), 'ok')
      reload()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    }
  }

  return (
    <section>
      <PageHeader
        title={s.username}
        actions={
          <>
            <Button size="sm" disabled={!can(PERM_RENEW)} onClick={() => setRenewOpen(true)}>
              {t('subscriber.renew')}
            </Button>
            {can(PERM_RENEW) && (
              <Button size="sm" variant="secondary" onClick={() => setRedeemOpen(true)}>
                {t('vouchers.redeem')}
              </Button>
            )}
            {canEdit && (
              <Button size="sm" variant="secondary" onClick={() => setResetOpen(true)}>
                {t('subscriber.resetMac')}
              </Button>
            )}
            {canEdit && (
              <Button size="sm" onClick={() => setEditOpen(true)}>
                {t('ui.edit')}
              </Button>
            )}
          </>
        }
      />

      {/* Status banner */}
      <div className="mb-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <Card label={t('subscriber.status')}>
          <div className="flex items-center gap-2">
            <StatusBadge status={s.status} />
            {data.live.online ? (
              <span className="flex items-center gap-1 text-xs text-ok">
                <span className="inline-block h-2 w-2 rounded-full bg-ok" aria-hidden="true" />
                {t('subscriber.online', { count: data.live.sessions })}
              </span>
            ) : (
              <span className="text-xs text-ink-muted">{t('subscriber.offline')}</span>
            )}
          </div>
        </Card>
        <Card label={t('subscriber.expiry')}>
          <ExpiryCountdown iso={s.expires_at} />
        </Card>
        <Card label={t('subscriber.profile')}>
          <span>{data.profile ? data.profile.name : t('ui.none')}</span>
        </Card>
        <Card label={t('subscriber.owner')}>
          <span>{data.owner ? data.owner.username : '—'}</span>
        </Card>
      </div>

      {/* Identity + override badges */}
      <div className="mb-4 flex flex-wrap gap-x-6 gap-y-2 rounded-md border border-surface-sunken bg-surface-raised p-4 text-sm">
        <Info label={t('subscriber.name')}>{s.name ?? '—'}</Info>
        <Info label={t('subscriber.phone')}>
          {s.phone ? (
            <span className="flex items-center gap-1.5">
              <Ltr>{s.phone}</Ltr>
              {s.whatsapp_opt_in && (
                <span className="rounded bg-ok/10 px-1.5 py-0.5 text-xs text-ok">
                  {t('subscriber.whatsappOn')}
                </span>
              )}
            </span>
          ) : (
            '—'
          )}
        </Info>
        <Info label={t('subscriber.macLock')}>
          {t(`subscriber.macMode.${s.mac_lock_mode}`)}
          {s.learned_mac ? (
            <>
              {' · '}
              <Ltr>{s.learned_mac}</Ltr>
            </>
          ) : null}
        </Info>
        {s.static_ip && (
          <Info label={t('subscriber.staticIp')}>
            <Ltr>{s.static_ip}</Ltr>
          </Info>
        )}
        {s.allow_hotspot && <Info label={t('subscriber.allowHotspot')}>{t('ui.yes')}</Info>}
        {overrideList(data, t).length > 0 && (
          <Info label={t('subscriber.overrides')}>
            <span className="flex flex-wrap gap-1">
              {overrideList(data, t).map((o) => (
                <span
                  key={o}
                  className="rounded bg-brand-soft px-1.5 py-0.5 text-xs text-brand-strong"
                >
                  {o}
                </span>
              ))}
            </span>
          </Info>
        )}
      </div>

      {/* Live widget */}
      <Section title={t('subscriber.liveNow')}>
        <LiveWidget subscriberId={id} />
      </Section>

      {/* Tabs */}
      <div className="mt-4">
        <div className="flex gap-1 border-b border-surface-sunken">
          <TabButton active={tab === 'usage'} onClick={() => setTab('usage')}>
            {t('subscriber.tabUsage')}
          </TabButton>
          <TabButton active={tab === 'history'} onClick={() => setTab('history')}>
            {t('subscriber.tabHistory')}
          </TabButton>
          <TabButton active={tab === 'audit'} onClick={() => setTab('audit')}>
            {t('subscriber.tabAudit')}
          </TabButton>
        </div>
        <div className="pt-4">
          {tab === 'usage' && <UsagePanel subscriberId={id} />}
          {tab === 'history' && <SessionHistory subscriberId={id} />}
          {tab === 'audit' && <AuditTrail subscriberId={id} />}
        </div>
      </div>

      <RenewModal
        open={renewOpen}
        onOpenChange={setRenewOpen}
        subscriber={s}
        currentProfileId={s.profile_id}
        profiles={profiles}
        onRenewed={reload}
      />

      <RedeemVoucherModal
        open={redeemOpen}
        onOpenChange={setRedeemOpen}
        subscriberId={id}
        onRedeemed={reload}
      />

      <SubscriberFormModal
        open={editOpen}
        onOpenChange={setEditOpen}
        existing={s}
        profiles={profiles}
        managers={managers}
        onSaved={({ offerDisconnect }) => {
          setEditOpen(false)
          reload()
          // Disabling an online user offers an immediate CoA disconnect (FR-1.2).
          if (offerDisconnect) setDisconnectOffer(true)
        }}
      />

      <ConfirmDialog
        open={resetOpen}
        onOpenChange={setResetOpen}
        title={t('subscriber.resetMacTitle')}
        body={t('subscriber.resetMacBody')}
        confirmLabel={t('subscriber.resetMac')}
        onConfirm={doResetMac}
      />

      {/* Disable-with-CoA-disconnect flow: after disabling an online user, offer
          to drop their live sessions now rather than waiting for re-auth. */}
      <DisconnectOffer
        open={disconnectOffer}
        onOpenChange={setDisconnectOffer}
        subscriberId={id}
        canDisconnect={can(PERM_DISCONNECT)}
      />
    </section>
  )
}

function ExpiryCountdown({ iso }: { iso: string | null }) {
  const t = useT()
  const { formatDate } = useFormatters()
  if (!iso) return <span className="text-ink-muted">{t('subscriber.noExpiry')}</span>
  const ms = new Date(iso).getTime() - Date.now()
  const days = Math.ceil(ms / (24 * 3600 * 1000))
  return (
    <span>
      {formatDate(iso, { timeStyle: undefined })}
      <span className={`ms-2 text-xs ${days < 0 ? 'text-danger' : 'text-ink-muted'}`}>
        {days < 0
          ? t('subscriber.expiredAgo', { days: Math.abs(days) })
          : t('subscriber.expiresIn', { days })}
      </span>
    </span>
  )
}

/** After-disable CoA offer: drops any live sessions for the subscriber. */
function DisconnectOffer({
  open,
  onOpenChange,
  subscriberId,
  canDisconnect,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  subscriberId: string
  canDisconnect: boolean
}) {
  const t = useT()
  const { toast } = useToast()

  async function dropAll() {
    // Snapshot the subscriber's live sessions off a short-lived stream, then CoA
    // each. The stream sends a snapshot immediately on connect.
    const sessions = await new Promise<LiveSession[]>((resolve) => {
      const handle = openLiveStream(
        {},
        {
          onEvent: (evt) => {
            if (evt.type === 'snapshot') {
              handle.close()
              resolve(evt.sessions.filter((s) => s.subscriber_id === subscriberId))
            }
          },
        },
      )
      // Safety timeout so a stalled stream can't hang the action.
      setTimeout(() => {
        handle.close()
        resolve([])
      }, 4000)
    })
    let acked = 0
    for (const s of sessions) {
      try {
        const res = await disconnectSession(s.nas_id, s.acct_session_id)
        if (res.outcome === 'ack') acked++
      } catch {
        /* surfaced in aggregate below */
      }
    }
    toast(t('subscriber.disconnectedCount', { count: acked }), acked > 0 ? 'ok' : 'danger')
  }

  if (!canDisconnect) return null

  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('subscriber.disableDisconnectTitle')}
      body={t('subscriber.disableDisconnectBody')}
      confirmLabel={t('live.disconnect')}
      destructive
      onConfirm={dropAll}
    />
  )
}

function Card({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="rounded-md border border-surface-sunken bg-surface-raised p-3">
      <p className="text-xs text-ink-muted">{label}</p>
      <div className="mt-1 text-sm font-medium">{children}</div>
    </div>
  )
}

function Info({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <p className="text-xs text-ink-muted">{label}</p>
      <div className="mt-0.5">{children}</div>
    </div>
  )
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="rounded-md border border-surface-sunken bg-surface-raised p-4">
      <h2 className="mb-2 text-sm font-semibold">{title}</h2>
      {children}
    </div>
  )
}

function TabButton({
  active,
  onClick,
  children,
}: {
  active: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`-mb-px border-b-2 px-3 py-2 text-sm ${
        active
          ? 'border-brand font-medium text-brand-strong'
          : 'border-transparent text-ink-muted hover:text-ink'
      }`}
    >
      {children}
    </button>
  )
}

function overrideList(d: SubscriberDetail, t: (k: string) => string): string[] {
  const out: string[] = []
  if (d.overrides.rate) out.push(t('subscriber.overrideRate'))
  if (d.overrides.price) out.push(t('subscriber.overridePrice'))
  if (d.overrides.session_limit) out.push(t('subscriber.overrideSession'))
  if (d.overrides.static_ip) out.push(t('subscriber.overrideStaticIp'))
  return out
}
