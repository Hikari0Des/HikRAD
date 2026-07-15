import { useState } from 'react'

import { Ltr, useT } from '@hikrad/shared'

import { discoverNas } from '../../api/nas'
import type { DiscoveredNas } from '../../api/types'
import { Button } from '../../components/Button'
import { Modal } from '../../components/Modal'
import { Field, TextInput } from '../../components/form'
import { useToast } from '../../components/Toast'
import type { NasPrefill } from './NasWizardModal'

/**
 * NAS auto-discovery (FR-56.1). Listens for MikroTik MNDP announcements (and an
 * optional range scan); results are read-only — nothing is ever sent to a
 * router. Already-registered rows are dimmed; picking a new one pre-fills the
 * wizard.
 */
export function DiscoverModal({
  open,
  onOpenChange,
  onPick,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  onPick: (prefill: NasPrefill) => void
}) {
  const t = useT()
  const { toast } = useToast()
  const [scanCidr, setScanCidr] = useState('')
  const [results, setResults] = useState<DiscoveredNas[] | null>(null)
  const [scanning, setScanning] = useState(false)

  async function scan() {
    setScanning(true)
    setResults(null)
    try {
      const res = await discoverNas({
        mndp_wait_ms: 3000,
        scan_cidr: scanCidr.trim() || undefined,
      })
      setResults(res.items)
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    } finally {
      setScanning(false)
    }
  }

  return (
    <Modal
      open={open}
      onOpenChange={onOpenChange}
      size="lg"
      title={t('nas.discoverTitle')}
      description={t('nas.discoverHint')}
    >
      <div className="flex items-end gap-2">
        <div className="flex-1">
          <Field label={t('nas.scanCidr')} hint={t('nas.scanCidrHint')}>
            <TextInput
              value={scanCidr}
              dir="ltr"
              placeholder="192.168.88.0/24"
              onChange={(e) => setScanCidr(e.target.value)}
            />
          </Field>
        </div>
        <Button disabled={scanning} onClick={scan}>
          {scanning ? t('nas.scanning') : t('nas.scan')}
        </Button>
      </div>

      <div className="mt-4">
        {scanning ? (
          <p className="py-6 text-center text-sm text-ink-muted">{t('nas.scanningBody')}</p>
        ) : results === null ? (
          <p className="py-6 text-center text-sm text-ink-muted">{t('nas.discoverIdle')}</p>
        ) : results.length === 0 ? (
          <div className="space-y-2 py-6 text-center text-sm text-ink-muted">
            <p>{t('nas.discoverNone')}</p>
            {/* MNDP is a LAN broadcast: it only reaches HikRAD when the server
                shares the routers' L2 segment (and, under Docker, when UDP
                5678 is published — see deploy/compose.yml). The range scan
                works regardless. */}
            <p className="mx-auto max-w-md text-xs">{t('nas.discoverNoneHint')}</p>
          </div>
        ) : (
          <ul className="space-y-2">
            {results.map((d) => (
              <li
                key={d.ip}
                className={`flex items-center justify-between gap-3 rounded-md border border-surface-sunken p-3 ${
                  d.already_registered ? 'opacity-50' : ''
                }`}
              >
                <div className="min-w-0 text-sm">
                  <div className="font-medium">
                    <Ltr>{d.identity || d.ip}</Ltr>
                  </div>
                  <div className="text-xs text-ink-muted">
                    <Ltr>{d.ip}</Ltr>
                    {d.ros_version ? ` · ROS ${d.ros_version}` : ''}
                    {d.mac ? (
                      <>
                        {' · '}
                        <Ltr>{d.mac}</Ltr>
                      </>
                    ) : null}
                  </div>
                </div>
                {d.already_registered ? (
                  <span className="text-xs text-ink-muted">{t('nas.alreadyRegistered')}</span>
                ) : (
                  <Button
                    size="sm"
                    onClick={() =>
                      onPick({
                        name: d.identity || undefined,
                        ip: d.ip,
                        ros_version: d.ros_version || undefined,
                      })
                    }
                  >
                    {t('nas.useThis')}
                  </Button>
                )}
              </li>
            ))}
          </ul>
        )}
      </div>
    </Modal>
  )
}
