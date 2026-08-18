import { useEffect, useState } from 'react'

import { LoadingState, useT } from '@hikrad/shared'

import { getHotspotPage, pushHotspotPage, saveHotspotPage } from '../../api/nasHotspotPage'
import type { HotspotPage, HotspotPageMode } from '../../api/types'
import { Button } from '../../components/Button'
import { Modal } from '../../components/Modal'
import { RadioGroup, RadioOption, Textarea } from '../../components/form'
import { useToast } from '../../components/Toast'
import { useAsync } from '../../hooks/useAsync'
import { routerErrorMessage } from '../../lib/routerErrors'

/**
 * FR-148 hotspot login page management, scoped to one hotspot service
 * instance. Save (a design edit) and push (the one router write) are
 * separate actions — see nasHotspotPage.ts's doc. The stale banner is the
 * phase's explicit requirement: an unpushed local edit and a RouterOS
 * upgrade since the last push are two different, separately-named reasons,
 * not one generic "might be out of date" message.
 */
export function HotspotPageModal({
  nasId,
  serviceId,
  serviceLabel,
  open,
  onOpenChange,
}: {
  nasId: string
  serviceId: string
  serviceLabel: string
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const t = useT()
  const { toast } = useToast()
  const { data, loading, reload } = useAsync(
    () => (open ? getHotspotPage(nasId, serviceId) : Promise.resolve(null as HotspotPage | null)),
    [nasId, serviceId, open],
  )

  const [mode, setMode] = useState<HotspotPageMode>('router_default')
  const [customHtml, setCustomHtml] = useState('')
  const [saving, setSaving] = useState(false)
  const [pushing, setPushing] = useState(false)

  useEffect(() => {
    if (!data) return
    setMode(data.mode)
    setCustomHtml(data.custom_html ?? '')
  }, [data])

  async function runSave() {
    setSaving(true)
    try {
      await saveHotspotPage(nasId, serviceId, mode, customHtml)
      toast(t('nas.hotspotPage.saveOk'), 'ok')
      reload()
    } catch (err) {
      toast(routerErrorMessage(err, t), 'danger')
    } finally {
      setSaving(false)
    }
  }

  async function runPush() {
    setPushing(true)
    try {
      const res = await pushHotspotPage(nasId, serviceId)
      toast(t('nas.hotspotPage.pushOk', { version: res.design_version }), 'ok')
      reload()
    } catch (err) {
      toast(routerErrorMessage(err, t), 'danger')
    } finally {
      setPushing(false)
    }
  }

  return (
    <Modal
      open={open}
      onOpenChange={saving || pushing ? () => {} : onOpenChange}
      size="lg"
      title={`${t('nas.hotspotPage.title')} — ${serviceLabel}`}
    >
      {loading || !data ? (
        <LoadingState />
      ) : (
        <div className="space-y-4">
          <p className="text-sm text-ink-muted">{t('nas.hotspotPage.hint')}</p>

          {data.stale ? (
            <p className="rounded-md bg-warn/10 p-3 text-sm text-warn">
              {t(`nas.hotspotPage.stale.${data.stale_reason ?? 'never_pushed'}`)}
            </p>
          ) : null}

          <RadioGroup
            name="hotspot-page-mode"
            value={mode}
            onValueChange={(v) => setMode(v as HotspotPageMode)}
          >
            <RadioOption value="router_default" label={t('nas.hotspotPage.mode.router_default')} />
            <RadioOption
              value="branded_template"
              label={t('nas.hotspotPage.mode.branded_template')}
            />
            <RadioOption value="custom" label={t('nas.hotspotPage.mode.custom')} />
          </RadioGroup>

          {mode === 'custom' ? (
            <div>
              <label className="mb-1 block text-sm font-medium">
                {t('nas.hotspotPage.customHtml')}
              </label>
              <Textarea
                dir="ltr"
                rows={10}
                value={customHtml}
                onChange={(e) => setCustomHtml(e.target.value)}
              />
            </div>
          ) : null}

          <p className="text-xs text-ink-muted">
            {t('nas.hotspotPage.designVersion', { version: data.design_version })} ·{' '}
            {data.pushed_version != null
              ? t('nas.hotspotPage.pushedVersion', { version: data.pushed_version })
              : t('nas.hotspotPage.neverPushed')}
          </p>

          <div className="flex justify-end gap-2 pt-2">
            <Button variant="secondary" disabled={saving || pushing} onClick={() => void runSave()}>
              {saving ? t('ui.working') : t('nas.hotspotPage.saveButton')}
            </Button>
            {mode !== 'router_default' ? (
              <Button disabled={saving || pushing} onClick={() => void runPush()}>
                {pushing ? t('ui.working') : t('nas.hotspotPage.pushButton')}
              </Button>
            ) : null}
          </div>
        </div>
      )}
    </Modal>
  )
}
