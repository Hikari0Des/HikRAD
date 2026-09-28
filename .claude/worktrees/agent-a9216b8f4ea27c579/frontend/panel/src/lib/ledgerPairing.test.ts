import { describe, expect, it } from 'vitest'

import type { LedgerItem } from '../api/billing'
import { indexReversals, runningBalances } from './ledgerPairing'

function item(over: Partial<LedgerItem>): LedgerItem {
  return {
    id: 'x',
    at: '2026-07-11T00:00:00Z',
    type: 'renewal',
    amount: 0,
    currency: 'IQD',
    actor_manager_id: 'm1',
    subscriber_id: null,
    source: 'panel',
    reference: '',
    reverses_id: null,
    note: '',
    ...over,
  }
}

describe('ledger reversing-entry pairing (FR-25)', () => {
  it('links a refund to the original it reverses, both directions', () => {
    const items: LedgerItem[] = [
      item({ id: 'refund1', type: 'refund', amount: -15000, reverses_id: 'renew1' }),
      item({ id: 'renew1', type: 'renewal', amount: 15000 }),
    ]
    const idx = indexReversals(items)
    expect(idx.isReversal.has('refund1')).toBe(true)
    expect(idx.isReversal.has('renew1')).toBe(false)
    expect(idx.reversedBy.get('renew1')).toBe('refund1')
  })

  it('leaves entries without reversals unpaired', () => {
    const idx = indexReversals([item({ id: 'a', type: 'topup', amount: 50000 })])
    expect(idx.isReversal.size).toBe(0)
    expect(idx.reversedBy.size).toBe(0)
  })

  it('computes a running balance oldest-first over a single-manager feed', () => {
    // Newest-first, as the list endpoint returns them.
    const items: LedgerItem[] = [
      item({ id: 'c', amount: -15000, type: 'renewal' }),
      item({ id: 'b', amount: 50000, type: 'topup' }),
      item({ id: 'a', amount: 20000, type: 'topup' }),
    ]
    const bal = runningBalances(items)
    expect(bal.get('a')).toBe(20000)
    expect(bal.get('b')).toBe(70000)
    expect(bal.get('c')).toBe(55000)
  })
})
