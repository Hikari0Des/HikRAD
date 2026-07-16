import { useEffect, useState } from 'react'

import { useT } from '@hikrad/shared'

import { listNas } from '../api/nas'
import type { Nas } from '../api/types'
import { Field, Select } from './form'

/**
 * FR-64 NAS-assignment picker, shared by the subscriber and profile forms.
 *
 * Two linked selects: a NAS, then optionally one of its service instances.
 * "Any NAS" (both empty) is the default and v1's behaviour — an account with no
 * scope authenticates anywhere, so the empty option is deliberately first and
 * labelled, not a blank.
 *
 * The pair moves together: clearing the NAS clears the service, because a
 * service scope without its NAS is not a state the backend keeps (the AuthView
 * loader reads the pair as a whole, keyed on nas_id).
 */
export function NasScopePicker({
  nasId,
  nasServiceId,
  onChange,
  errors,
}: {
  nasId: string
  nasServiceId: string
  onChange: (next: { nasId: string; nasServiceId: string }) => void
  errors?: Record<string, string>
}) {
  const t = useT()
  const [nas, setNas] = useState<Nas[]>([])
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    let alive = true
    listNas()
      .then((r) => alive && setNas(r.items))
      .catch(() => alive && setFailed(true))
    return () => {
      alive = false
    }
  }, [])

  const selected = nas.find((n) => n.id === nasId)
  const services = selected?.services ?? []

  return (
    <>
      <Field label={t('nasScope.nas')} hint={t('nasScope.hint')} error={errors?.nas_id}>
        <Select
          value={nasId}
          onChange={(e) => onChange({ nasId: e.target.value, nasServiceId: '' })}
        >
          <option value="">{t('nasScope.anyNas')}</option>
          {nas.map((n) => (
            <option key={n.id} value={n.id}>
              {n.name}
            </option>
          ))}
        </Select>
      </Field>
      <Field label={t('nasScope.service')} error={errors?.nas_service_id}>
        <Select
          value={nasServiceId}
          disabled={!nasId || services.length === 0}
          onChange={(e) => onChange({ nasId, nasServiceId: e.target.value })}
        >
          <option value="">{t('nasScope.anyService')}</option>
          {services.map((s) => (
            <option key={s.id} value={s.id}>
              {/* nas.typeName ("PPPoE"), not serviceType ("PPPoE only") — this
                  names the ROUTER's service, not a subscriber's entitlement. */}
              {serviceLabel(s.label, s.ros_server_name, t(`nas.typeName.${s.service}`))}
            </option>
          ))}
        </Select>
      </Field>
      {failed && (
        <p className="text-sm text-[--color-danger] sm:col-span-2">{t('nasScope.loadFailed')}</p>
      )}
    </>
  )
}

/**
 * Names one service instance for a picker. An operator recognises their zone by
 * its label ("Lobby") or the router's server name — falling back to the bare
 * kind only when neither is set.
 */
export function serviceLabel(label: string, rosServerName: string, kind: string): string {
  const name = label || rosServerName
  return name ? `${name} (${kind})` : kind
}
