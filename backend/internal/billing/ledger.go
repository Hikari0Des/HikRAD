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

// ledgerEntry is one row to append. Amount is signed, minor units of Currency
// (v2 phase 4, FR-68/69 — was AmountIQD, always-IQD).
type ledgerEntry struct {
	Type           string
	Amount         int64  // signed balance effect on ActorManagerID, minor units of Currency
	Currency       string // required — every entry moves money in exactly one currency
	ActorManagerID string
	SubscriberID   string // "" -> NULL
	Source         string
	Reference      string
	ReversesID     string // "" -> NULL
	Note           string
	// CurrencyRateID stamps the currency_rates row an "exchange" entry used
	// (FR-68.1: every rate actually used is stamped so history never
	// re-values); "" -> NULL for every other entry type.
	CurrencyRateID string
	// CostAtSale is the plan cost in force at the moment of a renewal (v2
	// phase 9, FR-72.1), in the same Currency as Amount. nil -> NULL, meaning
	// the plan had no recorded cost at that moment (never defaulted to 0 —
	// FR-71.1's "unknown, not free" requirement). Set only by the renewal
	// path; every other entry type leaves this nil.
	CostAtSale *int64
}

// insertLedger appends one entry within tx and returns its id.
func insertLedger(ctx context.Context, tx pgx.Tx, e ledgerEntry) (string, error) {
	if e.Currency == "" {
		e.Currency = "IQD"
	}
	var id string
	err := tx.QueryRow(ctx,
		`INSERT INTO ledger_transactions
		   (type, amount, currency, actor_manager_id, subscriber_id, source, reference, reverses_id, note, currency_rate_id, cost_at_sale)
		 VALUES ($1, $2, $3, NULLIF($4,'')::uuid, NULLIF($5,'')::uuid, $6, $7, NULLIF($8,'')::uuid, $9, NULLIF($10,'')::uuid, $11)
		 RETURNING id::text`,
		e.Type, e.Amount, e.Currency, e.ActorManagerID, e.SubscriberID, e.Source, e.Reference, e.ReversesID, e.Note, e.CurrencyRateID, e.CostAtSale,
	).Scan(&id)
	return id, err
}

// lockBalance ensures a manager_balances row exists for (managerID, currency)
// and locks it FOR UPDATE, serializing concurrent balance movements for that
// manager IN THAT CURRENCY (v2 phase 4: balances are per-currency — a lock on
// one currency never blocks or is confused with another). Returns the current
// cached balance.
func lockBalance(ctx context.Context, tx pgx.Tx, managerID, currency string) (int64, error) {
	if _, err := tx.Exec(ctx,
		`INSERT INTO manager_balances (manager_id, currency, balance) VALUES ($1::uuid, $2, 0)
		 ON CONFLICT (manager_id, currency) DO NOTHING`, managerID, currency); err != nil {
		return 0, err
	}
	var bal int64
	err := tx.QueryRow(ctx,
		`SELECT balance FROM manager_balances WHERE manager_id = $1::uuid AND currency = $2 FOR UPDATE`,
		managerID, currency).Scan(&bal)
	return bal, err
}

// recomputeBalance sets manager_balances.balance to the exact ledger sum for
// the manager IN THAT CURRENCY — the invariant that makes cache ≡ ledger true
// after every entry. Summing across currencies here would be the single most
// consequential bug in this phase (AC-69c) — every caller must pass the
// specific currency the entry it just inserted actually moved.
func recomputeBalance(ctx context.Context, tx pgx.Tx, managerID, currency string) error {
	_, err := tx.Exec(ctx,
		`UPDATE manager_balances
		    SET balance = (SELECT COALESCE(sum(amount),0)
		                     FROM ledger_transactions WHERE actor_manager_id = $1::uuid AND currency = $2),
		        updated_at = now()
		  WHERE manager_id = $1::uuid AND currency = $2`, managerID, currency)
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
	Amount       int64  // gross (signed: refunds negative), minor units of Currency
	Currency     string
	Method       string
	Source       string
	ShareToken   string
}

// insertPayment writes the receipt/payment row within tx.
func insertPayment(ctx context.Context, tx pgx.Tx, p paymentRow) error {
	if p.Currency == "" {
		p.Currency = "IQD"
	}
	_, err := tx.Exec(ctx,
		`INSERT INTO payments
		   (receipt_no, ledger_tx_id, subscriber_id, amount, currency, method, source, share_token)
		 VALUES ($1, $2::uuid, NULLIF($3,'')::uuid, $4, $5, $6, $7, $8)`,
		p.ReceiptNo, p.LedgerTxID, p.SubscriberID, p.Amount, p.Currency, p.Method, p.Source, p.ShareToken)
	return err
}

// randToken returns a URL-safe unguessable token for shareable receipt links.
func randToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
