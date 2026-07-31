package billing

// Ledger + balance store layer (FR-24, FR-20). The ledger is append-only (DB
// trigger, migration 0200); balances are DERIVED as the exact sum of a manager's
// entries and cached in manager_balances, recomputed inside the same transaction
// as every entry so cache ≡ ledger holds by construction (property test).

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ledgerEntry is one row to append. Amount is signed IQD (v3-2 FR-100.2: the
// book is single-currency IQD again; new rows are always IQD). A foreign
// amount is converted to IQD BEFORE this struct is built (convertToIQD), and
// the original figure is carried in EnteredAmount/EnteredCurrency (the FR-101.1
// conversion stamp).
type ledgerEntry struct {
	Type           string
	Amount         int64 // signed IQD balance effect on ActorManagerID
	ActorManagerID string
	SubscriberID   string // "" -> NULL
	Source         string
	Reference      string
	ReversesID     string // "" -> NULL
	Note           string
	// CurrencyRateID stamps the currency_rates row an entry-time conversion
	// used (FR-101.1: every rate actually used is stamped so history never
	// re-values); "" -> NULL for a native-IQD entry.
	CurrencyRateID string
	// EnteredAmount/EnteredCurrency are the FR-101.1 conversion stamp: the
	// original foreign figure this IQD row was converted from (for the
	// receipt/audit). Nil/"" for a native-IQD entry.
	EnteredAmount   *int64
	EnteredCurrency string
	// CostAtSale is the plan cost in force at the moment of a renewal (v2
	// phase 9, FR-72.1), in the same Currency as Amount. nil -> NULL, meaning
	// the plan had no recorded cost at that moment (never defaulted to 0 —
	// FR-71.1's "unknown, not free" requirement). Set only by the renewal
	// path; every other entry type leaves this nil.
	CostAtSale *int64
}

// insertLedger appends one entry within tx and returns its id. Every new row is
// IQD by construction (v3-2 FR-100.2 single book): the currency column is
// written 'IQD' unconditionally, and the original foreign figure — if this
// amount came from an entry-time conversion — is carried in the entered_* stamp.
func insertLedger(ctx context.Context, tx pgx.Tx, e ledgerEntry) (string, error) {
	var id string
	err := tx.QueryRow(ctx,
		`INSERT INTO ledger_transactions
		   (type, amount, currency, actor_manager_id, subscriber_id, source, reference, reverses_id, note, currency_rate_id, cost_at_sale, entered_amount, entered_currency)
		 VALUES ($1, $2, 'IQD', NULLIF($3,'')::uuid, NULLIF($4,'')::uuid, $5, $6, NULLIF($7,'')::uuid, $8, NULLIF($9,'')::uuid, $10, $11, NULLIF($12,''))
		 RETURNING id::text`,
		e.Type, e.Amount, e.ActorManagerID, e.SubscriberID, e.Source, e.Reference, e.ReversesID, e.Note, e.CurrencyRateID, e.CostAtSale, e.EnteredAmount, e.EnteredCurrency,
	).Scan(&id)
	return id, err
}

// lockBalance ensures the manager's IQD balance row exists and locks it FOR
// UPDATE, serializing concurrent balance movements for that manager. v3-2
// (FR-100.1): the book is single-currency IQD again, so there is exactly one
// balance row per manager and no currency dimension to confuse. Returns the
// current cached IQD balance.
func lockBalance(ctx context.Context, tx pgx.Tx, managerID string) (int64, error) {
	if _, err := tx.Exec(ctx,
		`INSERT INTO manager_balances (manager_id, currency, balance) VALUES ($1::uuid, 'IQD', 0)
		 ON CONFLICT (manager_id, currency) DO NOTHING`, managerID); err != nil {
		return 0, err
	}
	var bal int64
	err := tx.QueryRow(ctx,
		`SELECT balance FROM manager_balances WHERE manager_id = $1::uuid AND currency = 'IQD' FOR UPDATE`,
		managerID).Scan(&bal)
	return bal, err
}

// recomputeBalance sets the manager's IQD balance to the exact ledger sum — the
// invariant that makes cache ≡ ledger true after every entry (FR-100.4). Only
// IQD rows are summed; historical foreign rows net to zero after the migration
// and never appear in a new balance.
func recomputeBalance(ctx context.Context, tx pgx.Tx, managerID string) error {
	_, err := tx.Exec(ctx,
		`UPDATE manager_balances
		    SET balance = (SELECT COALESCE(sum(amount),0)
		                     FROM ledger_transactions WHERE actor_manager_id = $1::uuid AND currency = 'IQD'),
		        updated_at = now()
		  WHERE manager_id = $1::uuid AND currency = 'IQD'`, managerID)
	return err
}

// convertToIQD converts a foreign minor-unit amount to whole IQD at the current
// admin rate (FR-101.1), rounding half-up. Returns the IQD amount, the
// currency_rates row id used (to stamp on the ledger), or ("",nil) unchanged
// when the input is already IQD. errNoRate when no from->IQD rate exists — the
// caller surfaces it as a validation error (never a silent 1:1 conversion).
func convertToIQD(ctx context.Context, tx pgx.Tx, amount int64, fromCurrency string) (int64, string, error) {
	if fromCurrency == "" || fromCurrency == "IQD" {
		return amount, "", nil
	}
	var rate float64
	var rateID string
	err := tx.QueryRow(ctx,
		`SELECT id::text, rate FROM currency_rates
		   WHERE from_currency = $1 AND to_currency = 'IQD'
		   ORDER BY effective_from DESC LIMIT 1`, fromCurrency).Scan(&rateID, &rate)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", errNoRate
	}
	if err != nil {
		return 0, "", err
	}
	var digits int
	if err := tx.QueryRow(ctx, `SELECT minor_unit_digits FROM currencies WHERE code = $1`, fromCurrency).Scan(&digits); err != nil {
		return 0, "", err
	}
	whole := float64(amount) / pow10(digits)
	iqd := int64(whole*rate + 0.5) // amounts are non-negative → round-half-up
	return iqd, rateID, nil
}

var errNoRate = errors.New("billing: no IQD exchange rate defined for this currency")

// signedToIQD converts a SIGNED foreign amount to signed IQD, preserving sign
// (convertToIQD only handles non-negative magnitudes). Used by the refund path,
// where a reversal can be a credit or a debit. IQD input passes through
// unchanged with rateID "".
func signedToIQD(ctx context.Context, tx pgx.Tx, amount int64, fromCurrency string) (int64, string, error) {
	if fromCurrency == "" || fromCurrency == "IQD" {
		return amount, "", nil
	}
	neg := amount < 0
	mag := amount
	if neg {
		mag = -mag
	}
	iqd, rateID, err := convertToIQD(ctx, tx, mag, fromCurrency)
	if err != nil {
		return 0, "", err
	}
	if neg {
		iqd = -iqd
	}
	return iqd, rateID, nil
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
