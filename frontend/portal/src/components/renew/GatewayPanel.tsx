import { useState } from 'react'

import { useT } from '@hikrad/shared'

import { createPayment, listGateways, type Gateway } from '../../api/payments'
import { useAsync } from '../../hooks/useAsync'
import { setPendingIntent } from '../../lib/pendingPayment'

/**
 * E-wallet gateway list → create → redirect/app handoff (FR-42, task 3). All
 * gateways unreachable/disabled degrades to an explanatory message with the
 * voucher path emphasized (NFR-7) — never a dead end.
 */
export function GatewayPanel({ onGoToVoucher }: { onGoToVoucher: () => void }) {
  const t = useT()
  const gateways = useAsync(listGateways, [])
  const [starting, setStarting] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  async function start(gateway: Gateway) {
    setStarting(gateway.id)
    setError(null)
    try {
      const { redirect_url, intent_id } = await createPayment(gateway.id)
      setPendingIntent({ gateway: gateway.id, intentId: intent_id })
      window.location.href = redirect_url
    } catch {
      setError(t('portal.renew.gateway.startError'))
      setStarting(null)
    }
  }

  if (gateways.loading) {
    return <p className="py-4 text-center text-sm text-ink-muted">{t('common.loading')}</p>
  }

  const allDown = gateways.error !== undefined || (gateways.data?.items.length ?? 0) === 0

  if (allDown) {
    return (
      <div className="flex flex-col gap-3 rounded-xl bg-surface-raised p-4 text-sm shadow-sm">
        <p className="font-semibold">{t('portal.renew.gateway.allDownTitle')}</p>
        <p className="text-ink-muted">{t('portal.renew.gateway.allDownBody')}</p>
        <button
          type="button"
          onClick={onGoToVoucher}
          className="self-start rounded-md bg-brand px-4 py-2 font-semibold text-ink-inverse"
        >
          {t('portal.renew.gateway.useVoucher')}
        </button>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-3">
      {error ? (
        <p role="alert" className="text-sm text-danger">
          {error}
        </p>
      ) : null}
      <ul className="flex flex-col gap-2">
        {gateways.data?.items.map((g) => (
          <li key={g.id}>
            <button
              type="button"
              disabled={starting !== null}
              onClick={() => start(g)}
              className="flex w-full items-center justify-between rounded-xl bg-surface-raised p-4 text-sm font-semibold shadow-sm disabled:opacity-60"
            >
              <span>{g.name}</span>
              <span aria-hidden="true">{starting === g.id ? '…' : '›'}</span>
            </button>
          </li>
        ))}
      </ul>
    </div>
  )
}
