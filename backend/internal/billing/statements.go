package billing

// Financial statements (FR-156). BINDING: every statement here reads
// journal_lines/journal_entries/accounts ONLY — never ledger_transactions,
// manager_balances, or payments (§4's frozen contract: "statements read the
// journal only"). Aging (below) is the one exception by necessity, not by
// exception to the rule: it reads invoices/expenses (brand-new tables this
// phase introduces, with no legacy-ledger equivalent at all — due dates and
// payment-progress status live nowhere else), never the pre-existing
// ledger/balance tables the rule actually guards against.

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// accountBalance is one account's signed balance (positive = normal-balance
// side, e.g. a debit-normal asset account with net debits shows positive)
// over lines dated at <= asOf.
type accountBalance struct {
	Code          string
	Name          string
	Type          string
	Subtype       string
	NormalBalance string
	Debit         int64
	Credit        int64
}

// Balance returns the account's own-convention signed net: positive means
// "more of what this account normally holds."
func (a accountBalance) Balance() int64 {
	if a.NormalBalance == "debit" {
		return a.Debit - a.Credit
	}
	return a.Credit - a.Debit
}

// trialBalance returns every account's debit/credit totals for lines dated
// at <= asOf. Nets to zero by construction (every entry balances — gate
// item 1) — the reconciliation test asserts this explicitly rather than
// trusting the constraint alone.
func trialBalance(ctx context.Context, db *pgxpool.Pool, asOf time.Time) ([]accountBalance, error) {
	rows, err := db.Query(ctx,
		`SELECT a.code, a.name, a.type, COALESCE(a.subtype,''), a.normal_balance,
		        COALESCE(sum(jl.debit),0), COALESCE(sum(jl.credit),0)
		   FROM accounts a
		   LEFT JOIN journal_lines jl ON jl.account_id = a.id
		   LEFT JOIN journal_entries je ON je.id = jl.entry_id AND je.at <= $1
		  GROUP BY a.id, a.code, a.name, a.type, a.subtype, a.normal_balance
		  ORDER BY a.code`, asOf)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []accountBalance
	for rows.Next() {
		var b accountBalance
		if err := rows.Scan(&b.Code, &b.Name, &b.Type, &b.Subtype, &b.NormalBalance, &b.Debit, &b.Credit); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// accountBalanceRange is like trialBalance but scoped between [from,to] —
// the shape P&L needs (revenue/expense are period figures, not
// point-in-time balances).
func accountBalanceRange(ctx context.Context, db *pgxpool.Pool, from, to time.Time, types []string) ([]accountBalance, error) {
	rows, err := db.Query(ctx,
		`SELECT a.code, a.name, a.type, COALESCE(a.subtype,''), a.normal_balance,
		        COALESCE(sum(jl.debit),0), COALESCE(sum(jl.credit),0)
		   FROM accounts a
		   LEFT JOIN journal_lines jl ON jl.account_id = a.id
		   LEFT JOIN journal_entries je ON je.id = jl.entry_id AND je.at >= $1 AND je.at < $2
		  WHERE a.type = ANY($3)
		  GROUP BY a.id, a.code, a.name, a.type, a.subtype, a.normal_balance
		  ORDER BY a.code`, from, to, types)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []accountBalance
	for rows.Next() {
		var b accountBalance
		if err := rows.Scan(&b.Code, &b.Name, &b.Type, &b.Subtype, &b.NormalBalance, &b.Debit, &b.Credit); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

type profitAndLoss struct {
	From, To            time.Time
	Revenue             int64
	CostOfService       int64 // omitted from the API response for a scoped caller (gate item 12)
	GrossMargin         int64 // same
	OperatingExpense    int64
	CommissionExpense   int64
	DepreciationExpense int64
	NetIncome           int64
}

func computeProfitAndLoss(ctx context.Context, db *pgxpool.Pool, from, to time.Time) (profitAndLoss, error) {
	pl := profitAndLoss{From: from, To: to}
	balances, err := accountBalanceRange(ctx, db, from, to, []string{"revenue", "expense"})
	if err != nil {
		return pl, err
	}
	for _, b := range balances {
		switch b.Code {
		case acctRevenue, acctOtherRevenue:
			pl.Revenue += b.Balance()
		case acctCOGS:
			pl.CostOfService += b.Balance()
		case acctCommissionExpense:
			pl.CommissionExpense += b.Balance()
		case acctOperatingExpense:
			pl.OperatingExpense += b.Balance()
		case acctDepreciationExpense:
			pl.DepreciationExpense += b.Balance()
		}
	}
	pl.GrossMargin = pl.Revenue - pl.CostOfService
	pl.NetIncome = pl.Revenue - pl.CostOfService - pl.CommissionExpense - pl.OperatingExpense - pl.DepreciationExpense
	return pl, nil
}

type balanceSheet struct {
	AsOf             time.Time
	Assets           int64
	Liabilities      int64
	Capital          int64
	RetainedEarnings int64 // net income from inception to asOf, not yet closed to a capital account
	Equity           int64 // Capital + RetainedEarnings
}

func computeBalanceSheet(ctx context.Context, db *pgxpool.Pool, asOf time.Time) (balanceSheet, error) {
	bs := balanceSheet{AsOf: asOf}
	balances, err := trialBalance(ctx, db, asOf)
	if err != nil {
		return bs, err
	}
	for _, b := range balances {
		switch b.Type {
		case "asset":
			bs.Assets += b.Balance()
		case "liability":
			bs.Liabilities += b.Balance()
		case "equity":
			bs.Capital += b.Balance()
		}
	}
	pl, err := computeProfitAndLoss(ctx, db, time.Unix(0, 0).UTC(), asOf)
	if err != nil {
		return bs, err
	}
	bs.RetainedEarnings = pl.NetIncome
	bs.Equity = bs.Capital + bs.RetainedEarnings
	return bs, nil
}

// cashFlowLine is one cash/bank account's net movement over [from,to] — the
// direct method (v4-9 scope: the substrate + a simple, honest cash-delta
// view; a full indirect-method statement with operating/investing/financing
// sections is a natural follow-up, not built this phase — see the gate
// result).
type cashFlowLine struct {
	AccountCode string
	AccountName string
	NetChange   int64
}

func computeCashFlow(ctx context.Context, db *pgxpool.Pool, from, to time.Time) ([]cashFlowLine, int64, error) {
	rows, err := db.Query(ctx,
		`SELECT a.code, a.name, COALESCE(sum(jl.debit),0) - COALESCE(sum(jl.credit),0)
		   FROM accounts a
		   LEFT JOIN journal_lines jl ON jl.account_id = a.id
		   LEFT JOIN journal_entries je ON je.id = jl.entry_id AND je.at >= $1 AND je.at < $2
		  WHERE a.subtype IN ('cash','bank')
		  GROUP BY a.id, a.code, a.name
		  ORDER BY a.code`, from, to)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []cashFlowLine
	var total int64
	for rows.Next() {
		var l cashFlowLine
		if err := rows.Scan(&l.AccountCode, &l.AccountName, &l.NetChange); err != nil {
			return nil, 0, err
		}
		out = append(out, l)
		total += l.NetChange
	}
	return out, total, rows.Err()
}

type agingBucket struct {
	Bucket string // current | 1-30 | 31-60 | 61-90 | 90+
	Amount int64
}

// computeAging buckets outstanding invoices (receivable=true) or expenses
// (receivable=false) by how many days past due_date `asOf` is.
func computeAging(ctx context.Context, db *pgxpool.Pool, asOf time.Time, receivable bool) ([]agingBucket, error) {
	table := "expenses"
	if receivable {
		table = "invoices"
	}
	rows, err := db.Query(ctx,
		`SELECT CASE
		           WHEN due_date >= $1::date THEN 'current'
		           WHEN $1::date - due_date <= 30 THEN '1-30'
		           WHEN $1::date - due_date <= 60 THEN '31-60'
		           WHEN $1::date - due_date <= 90 THEN '61-90'
		           ELSE '90+'
		        END AS bucket,
		        COALESCE(sum(total_or_amount - amount_paid), 0)
		   FROM (SELECT due_date, `+totalColumn(receivable)+` AS total_or_amount, amount_paid FROM `+table+`
		          WHERE status NOT IN ('paid','void')) x
		  GROUP BY bucket`, asOf)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []agingBucket{}
	for rows.Next() {
		var b agingBucket
		if err := rows.Scan(&b.Bucket, &b.Amount); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func totalColumn(receivable bool) string {
	if receivable {
		return "total"
	}
	return "amount"
}
