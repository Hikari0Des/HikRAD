import { useState } from 'react'

import { Ltr, StatCard, useT } from '@hikrad/shared'

import { ApiError } from '../../../api/client'
import { getGeneralTab, pingDevice, rebootDevice } from '../../../api/nasDevice'
import type { DevicePingResult } from '../../../api/types'
import { useAuth } from '../../../auth/AuthContext'
import { PERM_NAS_REBOOT } from '../../../auth/permissions'
import { Button } from '../../../components/Button'
import { Modal } from '../../../components/Modal'
import { TextInput } from '../../../components/form'
import { FormField } from '../../../components/patterns'
import { useToast } from '../../../components/Toast'
import { useAsync } from '../../../hooks/useAsync'
import { DeviceTabShell } from './DeviceTabShell'

/** FR-142 General tab: identity/time/NTP plus the reboot and ping tools —
 * the phase's one exception to "read-only", gated accordingly. */
export function GeneralTab({ nasId }: { nasId: string }) {
  const t = useT()
  const { can } = useAuth()
  const { toast } = useToast()
  const { data, error, loading, reload } = useAsync(() => getGeneralTab(nasId), [nasId])

  const [rebootStage, setRebootStage] = useState<0 | 1 | 2>(0)
  const [rebooting, setRebooting] = useState(false)

  const [pingTarget, setPingTarget] = useState('')
  const [pinging, setPinging] = useState(false)
  const [pingResult, setPingResult] = useState<DevicePingResult | null>(null)
  const [pingErr, setPingErr] = useState<string | null>(null)

  const d = data?.data ?? null

  async function runReboot() {
    setRebooting(true)
    try {
      await rebootDevice(nasId)
      toast(t('nas.device.general.rebootSent'), 'ok')
      setRebootStage(0)
    } catch (err) {
      if (err instanceof ApiError && err.code === 'no_api_credentials') {
        toast(t('nas.device.noApiCredentials'), 'danger')
      } else {
        toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
      }
    } finally {
      setRebooting(false)
    }
  }

  async function runPing() {
    setPinging(true)
    setPingErr(null)
    setPingResult(null)
    try {
      const res = await pingDevice(nasId, pingTarget || '8.8.8.8')
      setPingResult(res)
    } catch (err) {
      setPingErr(err instanceof Error ? err.message : t('common.error.body'))
    } finally {
      setPinging(false)
    }
  }

  return (
    <DeviceTabShell
      nasId={nasId}
      loading={loading}
      error={error}
      reload={reload}
      hasData={d !== null}
      readAt={data?.read_at ?? null}
      stale={data?.stale ?? true}
      ownership={data?.ownership ?? 'router'}
      noApiCredentials={data?.no_api_credentials ?? false}
    >
      {d ? (
        <div className="space-y-6">
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
            <StatCard label={t('nas.device.general.identity')} value={d.identity || '—'} />
            <StatCard label={t('nas.device.general.timeZone')} value={d.time_zone || '—'} />
            <StatCard
              label={t('nas.device.general.time')}
              value={<Ltr>{d.time && d.date ? `${d.date} ${d.time}` : '—'}</Ltr>}
            />
            <StatCard
              label={t('nas.device.general.ntp')}
              value={d.ntp_enabled ? t('ui.yes') : t('ui.no')}
              hint={d.ntp_enabled ? d.ntp_mode : undefined}
            />
            <StatCard
              label={t('nas.device.general.ntpServers')}
              value={
                <Ltr>
                  {d.ntp_servers && d.ntp_servers.length > 0 ? d.ntp_servers.join(', ') : '—'}
                </Ltr>
              }
            />
          </div>

          <div className="rounded-md border border-border bg-surface-raised p-4">
            <h2 className="mb-1 text-sm font-semibold">{t('nas.device.general.pingTitle')}</h2>
            <p className="mb-3 text-xs text-ink-muted">{t('nas.device.general.pingHint')}</p>
            <div className="flex flex-wrap items-end gap-2">
              <div className="w-56">
                <FormField label={t('nas.device.general.pingTarget')}>
                  {(id) => (
                    <TextInput
                      id={id}
                      value={pingTarget}
                      onChange={(e) => setPingTarget(e.target.value)}
                      placeholder="8.8.8.8"
                      dir="ltr"
                    />
                  )}
                </FormField>
              </div>
              <Button size="sm" disabled={pinging} onClick={() => void runPing()}>
                {pinging ? t('ui.working') : t('nas.device.general.pingRun')}
              </Button>
            </div>
            {pingErr ? <p className="mt-2 text-sm text-danger">{pingErr}</p> : null}
            {pingResult ? (
              <dl className="mt-3 grid grid-cols-2 gap-2 text-sm sm:grid-cols-4">
                <div>
                  <dt className="text-xs text-ink-muted">
                    {t('nas.device.general.pingSentReceived')}
                  </dt>
                  <dd>
                    <Ltr>
                      {pingResult.received}/{pingResult.sent}
                    </Ltr>
                  </dd>
                </div>
                <div>
                  <dt className="text-xs text-ink-muted">{t('nas.device.general.pingLoss')}</dt>
                  <dd>
                    <Ltr>{pingResult.packet_loss_percent.toFixed(0)}%</Ltr>
                  </dd>
                </div>
                <div>
                  <dt className="text-xs text-ink-muted">{t('nas.device.general.pingAvg')}</dt>
                  <dd>
                    <Ltr>{pingResult.avg_rtt_ms.toFixed(1)} ms</Ltr>
                  </dd>
                </div>
                <div>
                  <dt className="text-xs text-ink-muted">{t('nas.device.general.pingMinMax')}</dt>
                  <dd>
                    <Ltr>
                      {pingResult.min_rtt_ms.toFixed(1)} / {pingResult.max_rtt_ms.toFixed(1)} ms
                    </Ltr>
                  </dd>
                </div>
              </dl>
            ) : null}
          </div>

          {can(PERM_NAS_REBOOT) ? (
            <div className="rounded-md border border-danger/30 bg-danger/5 p-4">
              <h2 className="mb-1 text-sm font-semibold text-danger">
                {t('nas.device.general.rebootTitle')}
              </h2>
              <p className="mb-3 text-xs text-ink-muted">{t('nas.device.general.rebootHint')}</p>
              <Button variant="danger" size="sm" onClick={() => setRebootStage(1)}>
                {t('nas.device.general.rebootButton')}
              </Button>
            </div>
          ) : null}
        </div>
      ) : null}

      <Modal
        open={rebootStage === 1}
        onOpenChange={(o) => !o && setRebootStage(0)}
        title={t('nas.device.general.rebootConfirm1Title')}
      >
        <p className="text-sm text-ink-muted">{t('nas.device.general.rebootConfirm1Body')}</p>
        <div className="mt-6 flex justify-end gap-2">
          <Button variant="ghost" onClick={() => setRebootStage(0)}>
            {t('ui.cancel')}
          </Button>
          <Button variant="danger" onClick={() => setRebootStage(2)}>
            {t('ui.continue')}
          </Button>
        </div>
      </Modal>

      <Modal
        open={rebootStage === 2}
        onOpenChange={(o) => !o && !rebooting && setRebootStage(0)}
        title={t('nas.device.general.rebootConfirm2Title')}
      >
        <p className="text-sm font-medium text-danger">
          {t('nas.device.general.rebootConfirm2Body')}
        </p>
        <div className="mt-6 flex justify-end gap-2">
          <Button variant="ghost" disabled={rebooting} onClick={() => setRebootStage(0)}>
            {t('ui.cancel')}
          </Button>
          <Button variant="danger" disabled={rebooting} onClick={() => void runReboot()}>
            {rebooting ? t('ui.working') : t('nas.device.general.rebootConfirm2Button')}
          </Button>
        </div>
      </Modal>
    </DeviceTabShell>
  )
}
