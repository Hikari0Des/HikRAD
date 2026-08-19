package billing

// Agent commission percentage and credit limit (FR-159), threaded into the
// EXISTING v2-9 wholesale/retail resolution (pricing_v2p9.go) rather than
// replacing it: resolveWholesale still decides what an agent's balance is
// charged; this file only adds (a) how far that balance may go negative
// before renewInTx refuses the charge, and (b) a rebate credited back to the
// agent's wallet on a completed sale.

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// managerCreditLimit returns how far (in IQD, always >= 0) managerID's
// balance may go negative before a balance-enforced renewal is refused.
// Default 0 — byte-identical to pre-v4-9 behavior (a hard block at exactly
// zero) for every manager that has never had a limit configured.
func managerCreditLimit(ctx context.Context, tx pgx.Tx, managerID string) (int64, error) {
	if managerID == "" {
		return 0, nil
	}
	var limit int64
	err := tx.QueryRow(ctx, `SELECT credit_limit FROM managers WHERE id = $1::uuid`, managerID).Scan(&limit)
	if err == pgx.ErrNoRows {
		return 0, nil
	}
	if limit < 0 {
		limit = -limit // stored non-negative by the API, but never trust a sign flip into a bigger limit than intended
	}
	return limit, err
}

// managerCommissionPct returns managerID's commission percentage (0-100,
// default 0 — no commission unless explicitly configured).
func managerCommissionPct(ctx context.Context, tx pgx.Tx, managerID string) (float64, error) {
	if managerID == "" {
		return 0, nil
	}
	var pct float64
	err := tx.QueryRow(ctx, `SELECT commission_pct FROM managers WHERE id = $1::uuid`, managerID).Scan(&pct)
	if err == pgx.ErrNoRows {
		return 0, nil
	}
	return pct, err
}

// postCommission credits actorManagerID's wallet with their commission
// share of baseAmount (the wholesale amount actually charged on this sale),
// as its own ledger row (type=commission, migration 0624 widens the CHECK)
// + journal entry, in the caller's already-open transaction. A no-op when
// the manager has no commission_pct configured (the common case).
func postCommission(ctx context.Context, tx pgx.Tx, at time.Time, actorManagerID string, baseAmount int64, source, reference string) error {
	pct, err := managerCommissionPct(ctx, tx, actorManagerID)
	if err != nil {
		return err
	}
	if pct <= 0 || baseAmount <= 0 {
		return nil
	}
	commission := int64(float64(baseAmount)*pct/100 + 0.5) // round-half-up, amounts non-negative
	if commission <= 0 {
		return nil
	}
	entry := ledgerEntry{
		Type:           "commission",
		Amount:         commission,
		ActorManagerID: actorManagerID,
		Source:         source,
		Reference:      reference,
		Note:           "commission",
	}
	txID, err := insertLedger(ctx, tx, entry)
	if err != nil {
		return err
	}
	if err := recomputeBalance(ctx, tx, actorManagerID); err != nil {
		return err
	}
	return postLedgerJournal(ctx, tx, at, entry, txID, 0, false, "billing.commission", actorManagerID)
}
