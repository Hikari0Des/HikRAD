package billing

// Ledger + balance store layer (FR-24, FR-20). The ledger is append-only (DB
// trigger, migration 0200); balances are DERIVED as the exact sum of a manager's
// entries and cached in manager_balances, recomputed inside the same transaction
// as every entry so cache ≡ ledger holds by construction (property test).

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ledgerEntry is one row to append.
type ledgerEntry struct {
	Type           string
	AmountIQD      int64 // signed balance effect on ActorManagerID
	ActorManagerID string
	SubscriberID   string // "" -> NULL
	Source         string
	Reference      string
	ReversesID     string // "" -> NULL
	Note           string
}

// insertLedger appends one entry within tx and returns its id.
func insertLedger(ctx context.Context, tx pgx.Tx, e ledgerEntry) (string, error) {
	var id string
	err := tx.QueryRow(ctx,
		`INSERT INTO ledger_transactions
		   (type, amount_iqd, actor_manager_id, subscriber_id, source, reference, reverses_id, note)
		 VALUES ($1, $2, NULLIF($3,'')::uuid, NULLIF($4,'')::uuid, $5, $6, NULLIF($7,'')::uuid, $8)
		 RETURNING id::text`,
		e.Type, e.AmountIQD, e.ActorManagerID, e.SubscriberID, e.Source, e.Reference, e.ReversesID, e.Note,
	).Scan(&id)
	return id, err
}

// lockBalance ensures a manager_balances row exists and locks it FOR UPDATE,
// serializing concurrent balance movements for that manager. Returns the current
// cached balance.
func lockBalance(ctx context.Context, tx pgx.Tx, managerID string) (int64, error) {
	if _, err := tx.Exec(ctx,
		`INSERT INTO manager_balances (manager_id, balance_iqd) VALUES ($1::uuid, 0)
		 ON CONFLICT (manager_id) DO NOTHING`, managerID); err != nil {
		return 0, err
	}
	var bal int64
	err := tx.QueryRow(ctx,
		`SELECT balance_iqd FROM manager_balances WHERE manager_id = $1::uuid FOR UPDATE`,
		managerID).Scan(&bal)
	return bal, err
}

// recomputeBalance sets manager_balances.balance_iqd to the exact ledger sum for
// the manager — the invariant that makes cache ≡ ledger true after every entry.
func recomputeBalance(ctx context.Context, tx pgx.Tx, managerID string) error {
	_, err := tx.Exec(ctx,
		`UPDATE manager_balances
		    SET balance_iqd = (SELECT COALESCE(sum(amount_iqd),0)
		                         FROM ledger_transactions WHERE actor_manager_id = $1::uuid),
		        updated_at = now()
		  WHERE manager_id = $1::uuid`, managerID)
	return err
}

// nextReceiptNo returns the next sequential receipt number with the settings
// prefix (FR-21), consuming the DB sequence inside tx.
func nextReceiptNo(ctx context.Context, tx pgx.Tx, prefix string) (string, error) {
	var n int64
	if err := tx.QueryRow(ctx, `SELECT nextval('receipt_seq')`).Scan(&n); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%06d", prefix, n), nil
}

// paymentRow is the gross customer-billing record backing a receipt (FR-21).
type paymentRow struct {
	ReceiptNo    string
	LedgerTxID   string
	SubscriberID string // "" -> NULL
	AmountIQD    int64  // gross (signed: refunds negative)
	Method       string
	Source       string
	ShareToken   string
}

// insertPayment writes the receipt/payment row within tx.
func insertPayment(ctx context.Context, tx pgx.Tx, p paymentRow) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO payments
		   (receipt_no, ledger_tx_id, subscriber_id, amount_iqd, method, source, share_token)
		 VALUES ($1, $2::uuid, NULLIF($3,'')::uuid, $4, $5, $6, $7)`,
		p.ReceiptNo, p.LedgerTxID, p.SubscriberID, p.AmountIQD, p.Method, p.Source, p.ShareToken)
	return err
}

// randToken returns a URL-safe unguessable token for shareable receipt links.
func randToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
