package billing

// Journal substrate (FR-153, §3.1/§4). STAGED beside ledger_transactions:
// every money-writing path in this package (renewInTx, refundHandler,
// topupHandler) calls postLedgerJournal inside the SAME database
// transaction as its ledger_transactions/manager_balances write, so the two
// can never diverge even under a crash mid-write — a stronger guarantee than
// the phase brief's minimum ("both are written... a reconciliation test
// asserts they agree"), achieved for free because the money code already
// holds an open transaction at every one of these call sites.
//
// Balanced-or-refused is enforced by migration 0617's DB trigger, not by
// this file being careful — postJournal's own arithmetic check exists only
// to fail fast with a clear Go error before ever reaching the DB; deleting
// it would not weaken the invariant, only make a bug in this file's own
// line-building logic surface as a less friendly Postgres error instead of
// a Go one.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Account codes. Mirrored 1:1 by migration 0616's seed — resolveAccountID
// never looks an account up by anything but its code, so the seed is the
// single source of truth for what each code means; these constants exist
// only so this package's own posting logic never hand-types a code string.
const (
	acctCash                = "1000"
	acctBank                = "1010"
	acctAR                  = "1100"
	acctFixedAssets         = "1500"
	acctAccumDepreciation   = "1590"
	acctAP                  = "2000"
	acctAgentWallet         = "2100"
	acctCostSuspense        = "2900"
	acctOwnerCapital        = "3000"
	acctRetainedEarnings    = "3900"
	acctRevenue             = "4000"
	acctOtherRevenue        = "4900"
	acctCOGS                = "5000"
	acctCommissionExpense   = "5100"
	acctOperatingExpense    = "5200"
	acctDepreciationExpense = "5900"
)

// dbtx is satisfied by both pgx.Tx (every money-writing path here, always
// inside a transaction — see the package comment) and *pgxpool.Pool
// (read-only statement queries, which need no transactional isolation
// stronger than a single SELECT). Structural typing: neither type imports
// this package, so this is the only place the shared shape is named.
type dbtx interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// resolveAccountID looks up an account's id by its code. Accounts are seed
// data an admin may add to (a manager's own bank account) but the codes this
// package posts against are is_system rows never renamed by the API
// (accounts_api.go refuses to edit code/is_system on a system row), so a
// per-call lookup (no cache) trades a little query volume for zero risk of a
// stale-cache money bug.
func resolveAccountID(ctx context.Context, q dbtx, code string) (string, error) {
	var id string
	err := q.QueryRow(ctx, `SELECT id::text FROM accounts WHERE code = $1`, code).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("billing: account code %q not seeded", code)
	}
	return id, err
}

// jLine is one line of a journal entry to post, by account CODE (resolved to
// an id inside postJournal — callers never touch account ids directly).
type jLine struct {
	AccountCode string
	Debit       int64 // exactly one of Debit/Credit is > 0 (DB CHECK, migration 0617)
	Credit      int64
	Memo        string
	// ManagerID/SubscriberID tag the optional subledger dimension (migration
	// 0617) for a line posted against a party-specific control account (the
	// agent wallet, accounts receivable) — "" for every other line.
	ManagerID    string
	SubscriberID string
}

// errUnbalancedEntry is postJournal's fast-fail Go-side guard; migration
// 0617's deferred constraint trigger is the actual binding enforcement
// (gate item 1) and fires even for a caller that bypasses this function
// entirely — see journal_dbtest.go's TestJournal_UnbalancedEntryRefused.
var errUnbalancedEntry = errors.New("billing: journal entry does not balance")

// validateBalanced is postJournal's fast-fail check, factored out so it is
// unit-testable without a database (journal_unit_test.go).
func validateBalanced(memo string, lines []jLine) error {
	if len(lines) < 2 {
		return fmt.Errorf("billing: journal entry %q needs at least 2 lines, got %d", memo, len(lines))
	}
	var d, c int64
	for _, l := range lines {
		if (l.Debit > 0) == (l.Credit > 0) {
			return fmt.Errorf("billing: journal line for %s must be debit XOR credit", l.AccountCode)
		}
		d += l.Debit
		c += l.Credit
	}
	if d != c {
		return fmt.Errorf("%w: debit %d != credit %d (%s)", errUnbalancedEntry, d, c, memo)
	}
	return nil
}

// postJournal inserts one journal entry and its lines within tx.
// ledgerTxID/reference/createdBy may be "" (-> NULL/empty). Every caller in
// this package builds lines whose debits equal credits by construction (see
// postLedgerJournal) — the Go-side sum check exists only to fail fast and
// readably; the DB trigger is what actually can't be bypassed.
func postJournal(ctx context.Context, tx pgx.Tx, at time.Time, memo, source, reference, ledgerTxID, createdBy string, lines []jLine) (string, error) {
	if err := validateBalanced(memo, lines); err != nil {
		return "", err
	}

	var entryID string
	err := tx.QueryRow(ctx,
		`INSERT INTO journal_entries (at, memo, source, reference, ledger_tx_id, created_by)
		 VALUES ($1, $2, $3, $4, NULLIF($5,'')::uuid, NULLIF($6,'')::uuid)
		 RETURNING id::text`,
		at, memo, source, reference, ledgerTxID, createdBy).Scan(&entryID)
	if err != nil {
		return "", err
	}
	for _, l := range lines {
		acctID, err := resolveAccountID(ctx, tx, l.AccountCode)
		if err != nil {
			return "", err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO journal_lines (entry_id, account_id, debit, credit, memo, manager_id, subscriber_id)
			 VALUES ($1::uuid, $2::uuid, $3, $4, $5, NULLIF($6,'')::uuid, NULLIF($7,'')::uuid)`,
			entryID, acctID, l.Debit, l.Credit, l.Memo, l.ManagerID, l.SubscriberID); err != nil {
			return "", err
		}
	}
	return entryID, nil
}

// reverseJournal posts a new entry with every line's debit/credit swapped
// relative to originalEntryID, linked via reverses_id (FR-158: undo writes a
// reversing entry, never a delete — both sides stay in the (immutable)
// journal, exactly mirroring how refund.go already reverses a ledger row).
func reverseJournal(ctx context.Context, tx pgx.Tx, originalEntryID, memo, source, reference, createdBy string) (string, error) {
	rows, err := tx.Query(ctx,
		`SELECT a.code, jl.debit, jl.credit, jl.memo, COALESCE(jl.manager_id::text,''), COALESCE(jl.subscriber_id::text,'')
		   FROM journal_lines jl
		   JOIN accounts a ON a.id = jl.account_id
		  WHERE jl.entry_id = $1::uuid`, originalEntryID)
	if err != nil {
		return "", err
	}
	var lines []jLine
	for rows.Next() {
		var code, lmemo, mgrID, subID string
		var debit, credit int64
		if err := rows.Scan(&code, &debit, &credit, &lmemo, &mgrID, &subID); err != nil {
			rows.Close()
			return "", err
		}
		lines = append(lines, jLine{AccountCode: code, Debit: credit, Credit: debit, Memo: lmemo, ManagerID: mgrID, SubscriberID: subID})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(lines) == 0 {
		return "", fmt.Errorf("billing: journal entry %s has no lines to reverse", originalEntryID)
	}

	var entryID string
	err = tx.QueryRow(ctx,
		`INSERT INTO journal_entries (memo, source, reference, reverses_id, created_by)
		 VALUES ($1, $2, $3, $4::uuid, NULLIF($5,'')::uuid)
		 RETURNING id::text`,
		memo, source, reference, originalEntryID, createdBy).Scan(&entryID)
	if err != nil {
		return "", err
	}
	for _, l := range lines {
		acctID, err := resolveAccountID(ctx, tx, l.AccountCode)
		if err != nil {
			return "", err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO journal_lines (entry_id, account_id, debit, credit, memo, manager_id, subscriber_id)
			 VALUES ($1::uuid, $2::uuid, $3, $4, $5, NULLIF($6,'')::uuid, NULLIF($7,'')::uuid)`,
			entryID, acctID, l.Debit, l.Credit, l.Memo, l.ManagerID, l.SubscriberID); err != nil {
			return "", err
		}
	}
	return entryID, nil
}

// postLedgerJournal is the ONE mapping from a ledger_transactions row to a
// balanced journal entry (§3.1's dual-write) — called by every ledger-
// writing path in this package (renewInTx, refundHandler, topupHandler) and
// by migration 0618's backfill logic (re-expressed in PL/pgSQL there, kept
// in lockstep with this function's rules; the dual-write reconciliation test
// is what actually proves the two never drifted apart, not code review
// alone).
//
// Rules (documented once here; 0618's SQL comment repeats them for readers
// who only ever look at the migration):
//   - type=topup: Dr Cash / Cr Agent Prepaid Balances, by entry.Amount
//     (always positive for a topup).
//   - type in (renewal, voucher_redeem, adjustment, refund, manual_payment,
//     discount) AND entry.ActorManagerID set AND entry.Amount != 0: a wallet
//     was actually moved (an agent/reseller spent or was refunded prepaid
//     credit) — Dr/Cr Agent Prepaid Balances vs Subscription Revenue, signed
//     by entry.Amount (negative = wallet spent = Dr wallet/Cr revenue;
//     positive = wallet refunded = Dr revenue/Cr wallet). The retail
//     payment row, if any, is the AGENT's own collection from the
//     subscriber in real life — never the owner's cash — so it is
//     deliberately NOT booked to Cash here.
//   - same type bucket, no wallet movement, but a payment row exists
//     (paymentGross != 0): a direct sale/refund the owner's own till
//     collected — Dr/Cr Cash vs Subscription Revenue, signed by
//     paymentGross.
//   - neither leg applies (a true zero-value adjustment, e.g. a trial
//     grant with no charge and no wallet movement): nothing posted — not
//     every ledger row is an economic event.
//   - entry.CostAtSale set and non-zero (either leg above having posted):
//     an additional Dr/Cr Cost of Service vs Cost Accrual Suspense pair,
//     signed by CostAtSale (positive = expense recognized; negative = a
//     refund's reversal of it) — an allocation, matched in the same period
//     as the revenue it goes with, never a real cash event.
//   - type=exchange (historical only — the exchange endpoint was retired in
//     v3-2; this case exists solely so a pre-v3-2 dataset's rows still
//     backfill): the IQD leg of a currency revaluation with no real revenue
//     or cash behind it — Dr/Cr Owner Capital vs Agent Prepaid Balances,
//     signed by entry.Amount.
//   - type=commission (new in this phase, FR-159): Dr Commission Expense /
//     Cr Agent Prepaid Balances, by entry.Amount (always positive — a
//     rebate credited back to the agent's wallet).
func postLedgerJournal(ctx context.Context, tx pgx.Tx, at time.Time, entry ledgerEntry, ledgerTxID string, paymentGross int64, hasPayment bool, source, createdBy string) error {
	memo := entry.Note
	if memo == "" {
		memo = entry.Type
	}
	ref := entry.Reference

	switch entry.Type {
	case "topup":
		// Sign-aware (not just the always-positive live-topup case): a
		// negative amount is an undo/reversal of a topup (undo.go), which
		// must swap sides — Dr Wallet/Cr Cash — rather than violate the
		// debit>=0 CHECK by writing a negative debit.
		if entry.Amount == 0 {
			return nil
		}
		lines := signedPair(acctCash, acctAgentWallet, entry.Amount, "topup", entry.ActorManagerID)
		_, err := postJournal(ctx, tx, at, memo, source, ref, ledgerTxID, createdBy, lines)
		return err

	case "commission":
		if entry.Amount == 0 {
			return nil
		}
		lines := signedPair(acctCommissionExpense, acctAgentWallet, entry.Amount, "commission", entry.ActorManagerID)
		_, err := postJournal(ctx, tx, at, memo, source, ref, ledgerTxID, createdBy, lines)
		return err

	case "exchange":
		if entry.Amount == 0 {
			return nil
		}
		lines := signedPair(acctOwnerCapital, acctAgentWallet, entry.Amount, "exchange (v3-2 legacy)", entry.ActorManagerID)
		_, err := postJournal(ctx, tx, at, memo, source, ref, ledgerTxID, createdBy, lines)
		return err

	case "renewal", "voucher_redeem", "adjustment", "refund", "manual_payment", "discount":
		var lines []jLine
		switch {
		case entry.ActorManagerID != "" && entry.Amount != 0:
			lines = signedPair(acctAgentWallet, acctRevenue, entry.Amount, "wallet-mediated "+entry.Type, entry.ActorManagerID)
		case hasPayment && paymentGross != 0:
			lines = signedPair(acctCash, acctRevenue, paymentGross, "direct cash "+entry.Type, "")
		default:
			// A true zero-value event (e.g. a $0 trial grant) — nothing to post.
		}
		if entry.CostAtSale != nil && *entry.CostAtSale != 0 {
			lines = append(lines, signedPair(acctCOGS, acctCostSuspense, *entry.CostAtSale, "cost of service", "")...)
		}
		if len(lines) == 0 {
			return nil
		}
		_, err := postJournal(ctx, tx, at, memo, source, ref, ledgerTxID, createdBy, lines)
		return err

	default:
		return fmt.Errorf("billing: postLedgerJournal: unknown ledger type %q", entry.Type)
	}
}

// signedPair builds a balanced 2-line pair for a signed wallet-style amount:
// positive -> Dr debitAcctWhenPositive / Cr creditAcctWhenPositive; negative
// -> the same two accounts with sides swapped. This is the one place the
// "sign already tells you the direction" reasoning in postLedgerJournal's
// doc comment is implemented, so a refund (whose amount is the original's
// exact negation) reverses correctly with no special-casing. managerID ("" =
// untagged) is stamped on whichever line posts against the agent wallet
// control account (acctAgentWallet), for the per-manager subledger
// reconciliation migration 0617's manager_id column exists for.
func signedPair(debitWhenPositive, creditWhenPositive string, amount int64, memo, managerID string) []jLine {
	mag := amount
	if mag < 0 {
		mag = -mag
	}
	tag := func(code string) string {
		if code == acctAgentWallet {
			return managerID
		}
		return ""
	}
	if amount >= 0 {
		return []jLine{
			{AccountCode: debitWhenPositive, Debit: mag, Memo: memo, ManagerID: tag(debitWhenPositive)},
			{AccountCode: creditWhenPositive, Credit: mag, Memo: memo, ManagerID: tag(creditWhenPositive)},
		}
	}
	return []jLine{
		{AccountCode: creditWhenPositive, Debit: mag, Memo: memo, ManagerID: tag(creditWhenPositive)},
		{AccountCode: debitWhenPositive, Credit: mag, Memo: memo, ManagerID: tag(debitWhenPositive)},
	}
}
