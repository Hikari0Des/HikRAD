package billing

// subscribers.Invoicer implementation (v4-5, FR-133): the batch-creation
// endpoint's two "actually charge for this" invoicing modes. "Invoice none"
// needs nothing from this package at all — the caller sets expires_at
// directly and never reaches here.

import (
	"context"

	"github.com/hikrad/hikrad/internal/subscribers"
)

var _ subscribers.Invoicer = (*batchInvoicer)(nil)

type batchInvoicer struct{ m *Module }

// InvoiceSubscriber runs THE renewal path (renew.go) for a freshly created
// subscriber — the exact same code every other renewal source converges on
// (FR-19.3), never a second money-moving implementation for batches.
func (b *batchInvoicer) InvoiceSubscriber(ctx context.Context, subscriberID, actorManagerID string, actorScoped bool) error {
	rp := renewParams{
		subscriberID:   subscriberID,
		actorManagerID: actorManagerID,
		source:         "batch",
		ledgerType:     "renewal",
		method:         "renewal",
		note:           "batch subscriber creation",
		chargeBalance:  actorManagerID != "",
		enforceBalance: actorScoped || !b.m.getBool(ctx, keyAdminBypass, true),
	}
	b.m.fillSettings(ctx, &rp)
	_, err := b.m.renew(ctx, rp)
	return err
}

// InvoiceBatch posts ONE ledger entry for count subscribers on profileID —
// the same "charge at generation, one entry for the whole batch" shape
// voucher.go's generateBatch already establishes for FR-22, reused here
// rather than reinvented.
func (b *batchInvoicer) InvoiceBatch(ctx context.Context, profileID, actorManagerID string, actorScoped bool, count int, reference string) error {
	if actorManagerID == "" || count <= 0 {
		return nil
	}
	var price int64
	if err := b.m.db.QueryRow(ctx, `SELECT price FROM profiles WHERE id = $1::uuid`, profileID).Scan(&price); err != nil {
		return err
	}
	total := price * int64(count)

	tx, err := b.m.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	bal, err := lockBalance(ctx, tx, actorManagerID)
	if err != nil {
		return err
	}
	enforce := actorScoped || !b.m.getBool(ctx, keyAdminBypass, true)
	if enforce && bal < total {
		return errInsufficientFunds
	}

	if _, err := insertLedger(ctx, tx, ledgerEntry{
		Type: "adjustment", Amount: -total, ActorManagerID: actorManagerID,
		Source: "subscriber_batch", Reference: reference, Note: "batch subscriber creation invoice",
	}); err != nil {
		return err
	}
	if err := recomputeBalance(ctx, tx, actorManagerID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
