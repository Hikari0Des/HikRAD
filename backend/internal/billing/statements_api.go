package billing

import (
	"fmt"
	"net/http"
	"time"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
)

const permAccountingView = "accounting.view"
const permAccountingManage = "accounting.manage"

// parseDateRange reads ?from=&to= (YYYY-MM-DD), defaulting to the current
// calendar month in UTC.
func parseDateRange(r *http.Request) (time.Time, time.Time) {
	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, 0)
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			from = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			to = t.AddDate(0, 0, 1) // inclusive end date
		}
	}
	return from, to
}

func parseAsOf(r *http.Request) time.Time {
	if v := r.URL.Query().Get("as_of"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			return t.AddDate(0, 0, 1) // include the whole day
		}
	}
	return time.Now().UTC().AddDate(0, 0, 1)
}

type plView struct {
	From                string `json:"from"`
	To                  string `json:"to"`
	Revenue             int64  `json:"revenue"`
	CostOfService       *int64 `json:"cost_of_service,omitempty"`
	GrossMargin         *int64 `json:"gross_margin,omitempty"`
	CommissionExpense   int64  `json:"commission_expense"`
	OperatingExpense    int64  `json:"operating_expense"`
	DepreciationExpense int64  `json:"depreciation_expense"`
	NetIncome           int64  `json:"net_income"`
}

// profitAndLossHandler serves GET /statements/pl?from=&to=. FR-153's
// reseller-isolation contract (gate item 12): cost_of_service/gross_margin
// are OMITTED (pointer fields left nil -> encoding/json drops them under
// omitempty) from a scoped caller's response, never present-but-null.
func (m *Module) profitAndLossHandler(w http.ResponseWriter, r *http.Request) {
	from, to := parseDateRange(r)
	pl, err := computeProfitAndLoss(r.Context(), m.db, from, to)
	if err != nil {
		m.internalError(w, "profit and loss", err)
		return
	}
	v := plView{
		From: from.Format("2006-01-02"), To: to.AddDate(0, 0, -1).Format("2006-01-02"),
		Revenue: pl.Revenue, CommissionExpense: pl.CommissionExpense,
		OperatingExpense: pl.OperatingExpense, DepreciationExpense: pl.DepreciationExpense, NetIncome: pl.NetIncome,
	}
	if auth.ScopeFilter(r.Context()) == nil {
		v.CostOfService = &pl.CostOfService
		v.GrossMargin = &pl.GrossMargin
	}
	httpapi.JSON(w, http.StatusOK, v)
}

type balanceSheetView struct {
	AsOf             string `json:"as_of"`
	Assets           int64  `json:"assets"`
	Liabilities      int64  `json:"liabilities"`
	Capital          int64  `json:"capital"`
	RetainedEarnings int64  `json:"retained_earnings"`
	Equity           int64  `json:"equity"`
}

func (m *Module) balanceSheetHandler(w http.ResponseWriter, r *http.Request) {
	asOf := parseAsOf(r)
	bs, err := computeBalanceSheet(r.Context(), m.db, asOf)
	if err != nil {
		m.internalError(w, "balance sheet", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, balanceSheetView{
		AsOf: asOf.AddDate(0, 0, -1).Format("2006-01-02"), Assets: bs.Assets, Liabilities: bs.Liabilities,
		Capital: bs.Capital, RetainedEarnings: bs.RetainedEarnings, Equity: bs.Equity,
	})
}

func (m *Module) cashFlowHandler(w http.ResponseWriter, r *http.Request) {
	from, to := parseDateRange(r)
	lines, total, err := computeCashFlow(r.Context(), m.db, from, to)
	if err != nil {
		m.internalError(w, "cash flow", err)
		return
	}
	type line struct {
		AccountCode string `json:"account_code"`
		AccountName string `json:"account_name"`
		NetChange   int64  `json:"net_change"`
	}
	out := make([]line, len(lines))
	for i, l := range lines {
		out[i] = line{l.AccountCode, l.AccountName, l.NetChange}
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{
		"from": from.Format("2006-01-02"), "to": to.AddDate(0, 0, -1).Format("2006-01-02"),
		"lines": out, "net_change": total,
	})
}

func (m *Module) agingReceivablesHandler(w http.ResponseWriter, r *http.Request) {
	buckets, err := computeAging(r.Context(), m.db, time.Now().UTC(), true)
	if err != nil {
		m.internalError(w, "aging receivables", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": buckets})
}

func (m *Module) agingPayablesHandler(w http.ResponseWriter, r *http.Request) {
	buckets, err := computeAging(r.Context(), m.db, time.Now().UTC(), false)
	if err != nil {
		m.internalError(w, "aging payables", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": buckets})
}

type trialBalanceLine struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Debit  int64  `json:"debit"`
	Credit int64  `json:"credit"`
}

// trialBalanceHandler serves GET /statements/trial-balance?as_of=. Nets to
// zero (gate item 6) — proven by TestStatements_TrialBalanceNetsToZero, not
// asserted here.
func (m *Module) trialBalanceHandler(w http.ResponseWriter, r *http.Request) {
	asOf := parseAsOf(r)
	balances, err := trialBalance(r.Context(), m.db, asOf)
	if err != nil {
		m.internalError(w, "trial balance", err)
		return
	}
	out := make([]trialBalanceLine, 0, len(balances))
	var totalDebit, totalCredit int64
	for _, b := range balances {
		if b.Debit == 0 && b.Credit == 0 {
			continue
		}
		out = append(out, trialBalanceLine{Code: b.Code, Name: b.Name, Type: b.Type, Debit: b.Debit, Credit: b.Credit})
		totalDebit += b.Debit
		totalCredit += b.Credit
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{
		"as_of": asOf.AddDate(0, 0, -1).Format("2006-01-02"), "lines": out,
		"total_debit": totalDebit, "total_credit": totalCredit,
	})
}

// generalLedgerHandler serves GET /statements/general-ledger?account=&from=&to=
// — every journal line for one account, in date order (the "Advanced" GL
// view).
func (m *Module) generalLedgerHandler(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("account")
	if code == "" {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "account", Message: "this field is required"})
		return
	}
	from, to := parseDateRange(r)
	rows, err := m.db.Query(r.Context(),
		`SELECT je.at, je.memo, je.source, je.reference, jl.debit, jl.credit, jl.memo
		   FROM journal_lines jl
		   JOIN journal_entries je ON je.id = jl.entry_id
		   JOIN accounts a ON a.id = jl.account_id
		  WHERE a.code = $1 AND je.at >= $2 AND je.at < $3
		  ORDER BY je.at ASC`, code, from, to)
	if err != nil {
		m.internalError(w, "general ledger", err)
		return
	}
	defer rows.Close()
	type line struct {
		At        time.Time `json:"at"`
		EntryMemo string    `json:"entry_memo"`
		Source    string    `json:"source"`
		Reference string    `json:"reference"`
		Debit     int64     `json:"debit"`
		Credit    int64     `json:"credit"`
		LineMemo  string    `json:"line_memo"`
		Running   int64     `json:"running_balance"`
	}
	out := []line{}
	var running int64
	for rows.Next() {
		var l line
		if err := rows.Scan(&l.At, &l.EntryMemo, &l.Source, &l.Reference, &l.Debit, &l.Credit, &l.LineMemo); err != nil {
			m.internalError(w, "scan gl line", err)
			return
		}
		running += l.Debit - l.Credit
		l.Running = running
		out = append(out, l)
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"account": code, "items": out})
}

// journalHandler serves GET /statements/journal?from=&to= — every journal
// entry with its lines (the "Advanced" Journal view), paginated by page/limit
// (keyset on (at,id) matching the rest of the API).
func (m *Module) journalHandler(w http.ResponseWriter, r *http.Request) {
	page, err := httpapi.ParsePage(r)
	if err != nil {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	from, to := parseDateRange(r)
	args := []any{from, to}
	where := "je.at >= $1 AND je.at < $2"
	if len(page.Cursor) == 2 {
		where += " AND (je.at, je.id) < ($3::timestamptz, $4::uuid)"
		args = append(args, page.Cursor[0], page.Cursor[1])
	}
	rows, err := m.db.Query(r.Context(),
		`SELECT je.id::text, je.at, je.memo, je.source, je.reference
		   FROM journal_entries je WHERE `+where+
			fmt.Sprintf(" ORDER BY je.at DESC, je.id DESC LIMIT %d", page.Limit+1), args...)
	if err != nil {
		m.internalError(w, "journal", err)
		return
	}
	type entryView struct {
		ID        string    `json:"id"`
		At        time.Time `json:"at"`
		Memo      string    `json:"memo"`
		Source    string    `json:"source"`
		Reference string    `json:"reference"`
	}
	var entries []entryView
	for rows.Next() {
		var e entryView
		if err := rows.Scan(&e.ID, &e.At, &e.Memo, &e.Source, &e.Reference); err != nil {
			rows.Close()
			m.internalError(w, "scan journal entry", err)
			return
		}
		entries = append(entries, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		m.internalError(w, "journal rows", err)
		return
	}
	next := ""
	if len(entries) > page.Limit {
		last := entries[page.Limit-1]
		entries = entries[:page.Limit]
		next = httpapi.EncodeCursor(last.At.Format(time.RFC3339Nano), last.ID)
	}
	httpapi.JSON(w, http.StatusOK, httpapi.NewListResponse(entries, next))
}
