package billing

// Invoices (FR-154). Accrual: issuing an invoice posts Dr Accounts
// Receivable / Cr Subscription Revenue immediately (revenue is recognized
// when billed, not when paid) — a no_charge invoice posts nothing, since
// nothing changed financially. Auto-generated on a priced renewal by
// autoInvoiceForRenewal (renew.go's caller wires this — see renew_api.go).

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	errInvoiceNotFound      = errors.New("billing: invoice not found")
	errInvoiceAlreadyVoid   = errors.New("billing: invoice is already void")
	errInvoiceHasReceipts   = errors.New("billing: invoice has receipts posted against it and cannot be voided directly")
	errNoChargeNotPermitted = errors.New("billing: no-charge invoicing requires the invoices.no_charge permission")
)

type invoiceLineInput struct {
	Description string
	Category    string
	Quantity    float64
	UnitPrice   int64
}

type createInvoiceParams struct {
	SubscriberID string
	Mode         string // full | prorated | no_charge
	IssueDate    time.Time
	DueDate      time.Time
	TaxRatePct   float64
	Lines        []invoiceLineInput
	Memo         string
	CreatedBy    string
	Source       string // for the journal entry's source column
}

// createInvoice computes totals, inserts the invoice + lines, and (unless
// no_charge) posts the accrual journal entry, all within tx. Returns the
// invoice id.
func createInvoice(ctx context.Context, tx pgx.Tx, at time.Time, p createInvoiceParams) (string, error) {
	if p.Mode != "full" && p.Mode != "prorated" && p.Mode != "no_charge" {
		return "", errors.New("billing: invalid invoice mode")
	}

	var subtotal int64
	for _, l := range p.Lines {
		subtotal += l.UnitPrice * int64(l.Quantity+0.5) // quantity is typically integral; round defensively
	}
	taxAmount := int64(float64(subtotal)*p.TaxRatePct/100 + 0.5)
	total := subtotal + taxAmount
	if p.Mode == "no_charge" {
		subtotal, taxAmount, total = 0, 0, 0
	}

	seq, err := nextSeq(ctx, tx, "invoice_seq")
	if err != nil {
		return "", err
	}
	invoiceNo := formatDocNo("INV-", seq)

	var invoiceID string
	err = tx.QueryRow(ctx,
		`INSERT INTO invoices (invoice_no, subscriber_id, mode, issue_date, due_date, tax_rate, subtotal, tax_amount, total, memo, created_by)
		 VALUES ($1, NULLIF($2,'')::uuid, $3, $4, $5, $6, $7, $8, $9, $10, NULLIF($11,'')::uuid)
		 RETURNING id::text`,
		invoiceNo, p.SubscriberID, p.Mode, p.IssueDate, p.DueDate, p.TaxRatePct, subtotal, taxAmount, total, p.Memo, p.CreatedBy).
		Scan(&invoiceID)
	if err != nil {
		return "", err
	}
	for _, l := range p.Lines {
		amt := l.UnitPrice * int64(l.Quantity+0.5)
		if p.Mode == "no_charge" {
			amt = 0
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO invoice_lines (invoice_id, description, category, quantity, unit_price, amount)
			 VALUES ($1::uuid, $2, $3, $4, $5, $6)`,
			invoiceID, l.Description, l.Category, l.Quantity, l.UnitPrice, amt); err != nil {
			return "", err
		}
	}

	if total > 0 {
		entryID, err := postJournal(ctx, tx, at, "invoice "+invoiceNo, p.Source, invoiceNo, "", p.CreatedBy, []jLine{
			{AccountCode: acctAR, Debit: total, Memo: "invoice " + invoiceNo, SubscriberID: p.SubscriberID},
			{AccountCode: acctRevenue, Credit: total, Memo: "invoice " + invoiceNo},
		})
		if err != nil {
			return "", err
		}
		if _, err := tx.Exec(ctx, `UPDATE invoices SET journal_entry_id = $2::uuid WHERE id = $1::uuid`, invoiceID, entryID); err != nil {
			return "", err
		}
	}
	return invoiceID, nil
}

// voidInvoice reverses an invoice's accrual entry (if it had one) and marks
// it void. Refuses an invoice with any receipts already posted against it
// (settle those first — voiding out from under a partial payment is not a
// single well-defined reversal) or one already void.
func voidInvoice(ctx context.Context, tx pgx.Tx, at time.Time, invoiceID, reason, actorID, source string) error {
	var status string
	var journalEntryID *string
	var amountPaid int64
	err := tx.QueryRow(ctx,
		`SELECT status, journal_entry_id::text, amount_paid FROM invoices WHERE id = $1::uuid FOR UPDATE`, invoiceID).
		Scan(&status, &journalEntryID, &amountPaid)
	if errors.Is(err, pgx.ErrNoRows) {
		return errInvoiceNotFound
	}
	if err != nil {
		return err
	}
	if status == "void" {
		return errInvoiceAlreadyVoid
	}
	if amountPaid > 0 {
		return errInvoiceHasReceipts
	}

	var voidEntryID *string
	if journalEntryID != nil {
		id, err := reverseJournal(ctx, tx, *journalEntryID, "void invoice", source, invoiceID, actorID)
		if err != nil {
			return err
		}
		voidEntryID = &id
	}
	_, err = tx.Exec(ctx,
		`UPDATE invoices SET status = 'void', voided_at = $2, voided_by = NULLIF($3,'')::uuid, void_reason = $4, void_journal_entry_id = $5::uuid
		  WHERE id = $1::uuid`,
		invoiceID, at, actorID, reason, voidEntryID)
	return err
}

// createPaidInvoiceForRenewal is FR-154's "auto-generated on priced
// renewals" invoice. It does NOT post its own journal entry — the renewal
// that triggers it already posted a balanced entry via postLedgerJournal
// (renew.go), and posting a second Dr AR/Cr Revenue pair for the identical
// sale would double-count revenue. Instead this invoice is created already
// paid (amount_paid = total, status = paid) and its journal_entry_id points
// at the renewal's own entry — a document/paper-trail record layered over
// money that already moved, not a second money event. Gate item 5 ("priced
// renewal produces an invoice and a balanced entry") is satisfied by that
// shared entry, proven balanced the same way every other entry is.
func createPaidInvoiceForRenewal(ctx context.Context, tx pgx.Tx, at time.Time, subscriberID string, amount int64, description, journalEntryID, createdBy string) (string, error) {
	if amount <= 0 {
		return "", nil
	}
	seq, err := nextSeq(ctx, tx, "invoice_seq")
	if err != nil {
		return "", err
	}
	invoiceNo := formatDocNo("INV-", seq)
	var invoiceID string
	err = tx.QueryRow(ctx,
		`INSERT INTO invoices (invoice_no, subscriber_id, mode, issue_date, due_date, subtotal, total, amount_paid, status, journal_entry_id, created_by)
		 VALUES ($1, $2::uuid, 'full', $3, $3, $4, $4, $4, 'paid', NULLIF($5,'')::uuid, NULLIF($6,'')::uuid)
		 RETURNING id::text`,
		invoiceNo, subscriberID, at, amount, journalEntryID, createdBy).Scan(&invoiceID)
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO invoice_lines (invoice_id, description, category, quantity, unit_price, amount)
		 VALUES ($1::uuid, $2, 'subscription', 1, $3, $3)`,
		invoiceID, description, amount)
	return invoiceID, err
}

// recomputeInvoiceStatus derives the FR-156 unpaid/partial/due/paid
// lifecycle from amountPaid vs total and whether dueDate has passed. 'due'
// takes priority over 'partial' once overdue — an overdue invoice is
// actionable regardless of how much of it has been paid.
func recomputeInvoiceStatus(amountPaid, total int64, dueDate, today time.Time) string {
	if total > 0 && amountPaid >= total {
		return "paid"
	}
	if dueDate.Before(today) {
		return "due"
	}
	if amountPaid > 0 {
		return "partial"
	}
	return "unpaid"
}

// nextSeq consumes a per-install document-number sequence. seqName is
// always a package-controlled constant, never user input.
func nextSeq(ctx context.Context, tx pgx.Tx, seqName string) (int64, error) {
	var n int64
	if err := tx.QueryRow(ctx, `SELECT nextval('`+seqName+`')`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func formatDocNo(prefix string, n int64) string {
	return fmt.Sprintf("%s%06d", prefix, n)
}
