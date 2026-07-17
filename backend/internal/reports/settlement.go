package reports

// Agent settlement report (C2, FR-45.2/FR-20.4): Hassan's collection report
// IS his ledger slice, so every field here is a plain signed sum over
// ledger_transactions for one actor_manager_id — never payments, never a
// second bookkeeping system. closing is computed the same way
// billing.recomputeBalance derives manager_balances (sum of all entries up
// to the instant), which is what makes "closing ≡ live balance at to=now"
// hold by construction rather than by a maintained invariant that could
// drift. topups/refunds are direct type filters (both ledger credits);
// renewals is a count + the positive magnitude spent (ledger debits for type
// IN ('renewal','voucher_redeem') are negative, so it's reported negated) —
// note a voucher-funded renewal's OWN balance effect is 0 (the batch creator
// was already debited as an 'adjustment' entry when the batch was
// generated), which is correct under this "settlement = literal ledger
// slice" definition, not a bug.
//
// v2 phase 4 (FR-70.2): a settlement is scoped to ONE currency
// (?currency=IQD, default IQD) — an agent settles each currency separately,
// exactly matching FR-69.2's per-currency balance model. Summing across
// currencies here would be the same class of bug AC-69c locks out of the
// balance layer.

import (
	"net/http"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
)

type settlementResponse struct {
	Currency string `json:"currency"`
	Opening  int64  `json:"opening"`
	Topups   int64  `json:"topups"`
	Renewals struct {
		Count  int   `json:"count"`
		Amount int64 `json:"amount"`
	} `json:"renewals"`
	Refunds int64 `json:"refunds"`
	Closing int64 `json:"closing"`
}

func (m *Module) settlementHandler(w http.ResponseWriter, r *http.Request) {
	scope := auth.ScopeFilter(r.Context())
	managerID := r.URL.Query().Get("manager_id")
	if scope != nil {
		managerID = scope.ManagerID
	}
	if managerID == "" {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "manager_id", Message: "this field is required"})
		return
	}
	currency := r.URL.Query().Get("currency")
	if currency == "" {
		currency = "IQD"
	}
	from, to := parseRange(r)
	ctx := r.Context()

	resp := settlementResponse{Currency: currency}
	err := m.db.QueryRow(ctx,
		`SELECT COALESCE(sum(amount),0)::bigint FROM ledger_transactions
		  WHERE actor_manager_id = $1::uuid AND currency = $2 AND at < $3`, managerID, currency, from).Scan(&resp.Opening)
	if err != nil {
		m.internalError(w, "settlement opening", err)
		return
	}
	err = m.db.QueryRow(ctx,
		`SELECT COALESCE(sum(amount),0)::bigint FROM ledger_transactions
		  WHERE actor_manager_id = $1::uuid AND currency = $2 AND at <= $3`, managerID, currency, to).Scan(&resp.Closing)
	if err != nil {
		m.internalError(w, "settlement closing", err)
		return
	}
	err = m.db.QueryRow(ctx,
		`SELECT COALESCE(sum(amount),0)::bigint FROM ledger_transactions
		  WHERE actor_manager_id = $1::uuid AND currency = $2 AND type = 'topup' AND at >= $3 AND at < $4`,
		managerID, currency, from, to).Scan(&resp.Topups)
	if err != nil {
		m.internalError(w, "settlement topups", err)
		return
	}
	var renewalDelta int64
	err = m.db.QueryRow(ctx,
		`SELECT count(*)::int, COALESCE(sum(amount),0)::bigint FROM ledger_transactions
		  WHERE actor_manager_id = $1::uuid AND currency = $2 AND type IN ('renewal','voucher_redeem')
		    AND at >= $3 AND at < $4`,
		managerID, currency, from, to).Scan(&resp.Renewals.Count, &renewalDelta)
	if err != nil {
		m.internalError(w, "settlement renewals", err)
		return
	}
	resp.Renewals.Amount = -renewalDelta
	err = m.db.QueryRow(ctx,
		`SELECT COALESCE(sum(amount),0)::bigint FROM ledger_transactions
		  WHERE actor_manager_id = $1::uuid AND currency = $2 AND type = 'refund' AND at >= $3 AND at < $4`,
		managerID, currency, from, to).Scan(&resp.Refunds)
	if err != nil {
		m.internalError(w, "settlement refunds", err)
		return
	}

	if r.URL.Query().Get("format") == "csv" {
		if !requireExport(w, r) {
			return
		}
		writeCSV(w, "settlement.csv",
			[]string{"currency", "opening", "topups", "renewals_count", "renewals_amount", "refunds", "closing"},
			[][]string{{
				resp.Currency, itoa64(resp.Opening), itoa64(resp.Topups), itoa(resp.Renewals.Count),
				itoa64(resp.Renewals.Amount), itoa64(resp.Refunds), itoa64(resp.Closing),
			}})
		return
	}
	httpapi.JSON(w, http.StatusOK, resp)
}
