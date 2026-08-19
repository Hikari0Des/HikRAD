package billing

// Receipts (money in, settling an invoice) and payments (money out,
// settling an expense) — FR-155. Each posts Dr Cash/Cr Accounts Receivable
// (a receipt) or Dr Accounts Payable/Cr Cash (a payment), and advances the
// settled document's unpaid -> partial -> due -> paid status
// (recomputeInvoiceStatus).

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	errInvoiceVoidOrPaid = errors.New("billing: invoice is void or already fully paid")
	errExpenseVoidOrPaid = errors.New("billing: expense is void or already fully paid")
	errOverpayment       = errors.New("billing: amount exceeds the remaining balance")
)

// postARReceipt settles amount against invoiceID: Dr Cash / Cr Accounts
// Receivable (tagged with the invoice's subscriber for the same subledger
// reconciliation the agent wallet already uses), advances amount_paid and
// status.
func postARReceipt(ctx context.Context, tx pgx.Tx, at time.Time, invoiceID string, amount int64, method, memo, actorID, source string) (string, error) {
	if amount <= 0 {
		return "", errors.New("billing: receipt amount must be positive")
	}
	var status string
	var total, amountPaid int64
	var subscriberID *string
	var dueDate time.Time
	err := tx.QueryRow(ctx,
		`SELECT status, total, amount_paid, subscriber_id::text, due_date FROM invoices WHERE id = $1::uuid FOR UPDATE`, invoiceID).
		Scan(&status, &total, &amountPaid, &subscriberID, &dueDate)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errInvoiceNotFound
	}
	if err != nil {
		return "", err
	}
	if status == "void" || amountPaid >= total {
		return "", errInvoiceVoidOrPaid
	}
	if amountPaid+amount > total {
		return "", errOverpayment
	}

	seq, err := nextSeq(ctx, tx, "ar_receipt_seq")
	if err != nil {
		return "", err
	}
	receiptNo := formatDocNo("AR-", seq)

	subID := ""
	if subscriberID != nil {
		subID = *subscriberID
	}
	entryID, err := postJournal(ctx, tx, at, "receipt "+receiptNo, source, receiptNo, "", actorID, []jLine{
		{AccountCode: acctCash, Debit: amount, Memo: "receipt " + receiptNo},
		{AccountCode: acctAR, Credit: amount, Memo: "receipt " + receiptNo, SubscriberID: subID},
	})
	if err != nil {
		return "", err
	}

	var receiptID string
	err = tx.QueryRow(ctx,
		`INSERT INTO ar_receipts (receipt_no, invoice_id, amount, method, memo, journal_entry_id, created_by)
		 VALUES ($1, $2::uuid, $3, $4, $5, $6::uuid, NULLIF($7,'')::uuid) RETURNING id::text`,
		receiptNo, invoiceID, amount, method, memo, entryID, actorID).Scan(&receiptID)
	if err != nil {
		return "", err
	}

	newPaid := amountPaid + amount
	newStatus := recomputeInvoiceStatus(newPaid, total, dueDate, at)
	if _, err := tx.Exec(ctx,
		`UPDATE invoices SET amount_paid = $2, status = $3 WHERE id = $1::uuid`, invoiceID, newPaid, newStatus); err != nil {
		return "", err
	}
	return receiptID, nil
}

// postAPPayment settles amount against expenseID: Dr Accounts Payable /
// Cr Cash.
func postAPPayment(ctx context.Context, tx pgx.Tx, at time.Time, expenseID string, amount int64, method, memo, actorID, source string) (string, error) {
	if amount <= 0 {
		return "", errors.New("billing: payment amount must be positive")
	}
	var status string
	var total, amountPaid int64
	var dueDate time.Time
	err := tx.QueryRow(ctx,
		`SELECT status, amount, amount_paid, due_date FROM expenses WHERE id = $1::uuid FOR UPDATE`, expenseID).
		Scan(&status, &total, &amountPaid, &dueDate)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errExpenseNotFound
	}
	if err != nil {
		return "", err
	}
	if status == "void" || amountPaid >= total {
		return "", errExpenseVoidOrPaid
	}
	if amountPaid+amount > total {
		return "", errOverpayment
	}

	seq, err := nextSeq(ctx, tx, "ap_payment_seq")
	if err != nil {
		return "", err
	}
	paymentNo := formatDocNo("AP-", seq)

	entryID, err := postJournal(ctx, tx, at, "payment "+paymentNo, source, paymentNo, "", actorID, []jLine{
		{AccountCode: acctAP, Debit: amount, Memo: "payment " + paymentNo},
		{AccountCode: acctCash, Credit: amount, Memo: "payment " + paymentNo},
	})
	if err != nil {
		return "", err
	}

	var paymentID string
	err = tx.QueryRow(ctx,
		`INSERT INTO ap_payments (payment_no, expense_id, amount, method, memo, journal_entry_id, created_by)
		 VALUES ($1, $2::uuid, $3, $4, $5, $6::uuid, NULLIF($7,'')::uuid) RETURNING id::text`,
		paymentNo, expenseID, amount, method, memo, entryID, actorID).Scan(&paymentID)
	if err != nil {
		return "", err
	}

	newPaid := amountPaid + amount
	newStatus := recomputeInvoiceStatus(newPaid, total, dueDate, at)
	if _, err := tx.Exec(ctx,
		`UPDATE expenses SET amount_paid = $2, status = $3 WHERE id = $1::uuid`, expenseID, newPaid, newStatus); err != nil {
		return "", err
	}
	return paymentID, nil
}
