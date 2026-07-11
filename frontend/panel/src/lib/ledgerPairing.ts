/**
 * Reversing-entry pairing for the ledger view (FR-24/FR-25). Refunds are
 * append-only reversing entries that carry `reverses_id` pointing at the
 * original transaction; the ledger never mutates the original. This helper
 * builds the two-way index the UI needs to visually pair a refund with the
 * entry it reverses. Pure so it is unit-testable.
 */
import type { LedgerItem } from '../api/billing'

export interface ReversalIndex {
  /** original tx id → the id of the entry that reverses it. */
  reversedBy: Map<string, string>
  /** ids of entries that are themselves reversals (refunds). */
  isReversal: Set<string>
}

export function indexReversals(items: LedgerItem[]): ReversalIndex {
  const reversedBy = new Map<string, string>()
  const isReversal = new Set<string>()
  for (const it of items) {
    if (it.reverses_id) {
      isReversal.add(it.id)
      reversedBy.set(it.reverses_id, it.id)
    }
  }
  return { reversedBy, isReversal }
}

/**
 * Running-balance column: only meaningful when the ledger is filtered to a
 * single manager (a mixed feed has no single balance). Returns a map of tx id →
 * balance after that entry, computed oldest-first over the amounts. `items` are
 * assumed newest-first (the list-endpoint order), so we walk them in reverse.
 */
export function runningBalances(items: LedgerItem[]): Map<string, number> {
  const out = new Map<string, number>()
  let bal = 0
  for (let i = items.length - 1; i >= 0; i--) {
    bal += items[i].amount_iqd
    out.set(items[i].id, bal)
  }
  return out
}
