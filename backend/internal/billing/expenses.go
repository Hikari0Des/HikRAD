package billing

// Expenses (FR-155). Accrual: recording an expense as incurred posts
// Dr Operating Expenses / Cr Accounts Payable immediately; an AP payment
// (ar_ap.go) later settles it (Dr Accounts Payable / Cr Cash).

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	errExpenseNotFound    = errors.New("billing: expense not found")
	errExpenseAlreadyVoid = errors.New("billing: expense is already void")
	errExpenseHasPayments = errors.New("billing: expense has payments posted against it and cannot be voided directly")
)

type createExpenseParams struct {
	Category     string
	Vendor       string
	Amount       int64
	IncurredDate time.Time
	DueDate      time.Time
	Memo         string
	CreatedBy    string
	Source       string
}

func createExpense(ctx context.Context, tx pgx.Tx, at time.Time, p createExpenseParams) (string, error) {
	if p.Amount <= 0 {
		return "", errors.New("billing: expense amount must be positive")
	}
	seq, err := nextSeq(ctx, tx, "expense_seq")
	if err != nil {
		return "", err
	}
	expenseNo := formatDocNo("EXP-", seq)

	var expenseID string
	err = tx.QueryRow(ctx,
		`INSERT INTO expenses (expense_no, category, vendor, amount, incurred_date, due_date, memo, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8,'')::uuid)
		 RETURNING id::text`,
		expenseNo, p.Category, p.Vendor, p.Amount, p.IncurredDate, p.DueDate, p.Memo, p.CreatedBy).
		Scan(&expenseID)
	if err != nil {
		return "", err
	}

	entryID, err := postJournal(ctx, tx, at, "expense "+expenseNo, p.Source, expenseNo, "", p.CreatedBy, []jLine{
		{AccountCode: acctOperatingExpense, Debit: p.Amount, Memo: "expense " + expenseNo},
		{AccountCode: acctAP, Credit: p.Amount, Memo: "expense " + expenseNo},
	})
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `UPDATE expenses SET journal_entry_id = $2::uuid WHERE id = $1::uuid`, expenseID, entryID); err != nil {
		return "", err
	}
	return expenseID, nil
}

func voidExpense(ctx context.Context, tx pgx.Tx, at time.Time, expenseID, reason, actorID, source string) error {
	var status string
	var journalEntryID *string
	var amountPaid int64
	err := tx.QueryRow(ctx,
		`SELECT status, journal_entry_id::text, amount_paid FROM expenses WHERE id = $1::uuid FOR UPDATE`, expenseID).
		Scan(&status, &journalEntryID, &amountPaid)
	if errors.Is(err, pgx.ErrNoRows) {
		return errExpenseNotFound
	}
	if err != nil {
		return err
	}
	if status == "void" {
		return errExpenseAlreadyVoid
	}
	if amountPaid > 0 {
		return errExpenseHasPayments
	}

	var voidEntryID *string
	if journalEntryID != nil {
		id, err := reverseJournal(ctx, tx, *journalEntryID, "void expense", source, expenseID, actorID)
		if err != nil {
			return err
		}
		voidEntryID = &id
	}
	_, err = tx.Exec(ctx,
		`UPDATE expenses SET status = 'void', voided_at = $2, voided_by = NULLIF($3,'')::uuid, void_reason = $4, void_journal_entry_id = $5::uuid
		  WHERE id = $1::uuid`,
		expenseID, at, actorID, reason, voidEntryID)
	return err
}
