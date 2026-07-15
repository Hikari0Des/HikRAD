package reports

// Agent settlement report (C2, FR-45.2/FR-20.4): Hassan's collection report
// IS his ledger slice, so every field here is a plain signed sum over
// ledger_transactions for one actor_manager_id — never payments, never a
// second bookkeeping system. closing_iqd is computed the same way
// billing.recomputeBalance derives manager_balances (sum of all entries up
// to the instant), which is what makes "closing ≡ live balance at to=now"
// hold by construction rather than by a maintained invariant that could
// drift. topups_iqd/refunds_iqd are direct type filters (both ledger
// credits); renewals is a count + the positive magnitude spent (ledger
// debits for type IN ('renewal','voucher_redeem') are negative, so it's
// reported negated) — note a voucher-funded renewal's OWN balance effect is
// 0 (the batch creator was already debited as an 'adjustment' entry when the
// batch was generated), which is correct under this "settlement = literal
// ledger slice" definition, not a bug.

import (
	"net/http"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
)

type settlementResponse struct {
	OpeningIQD int64 `json:"opening_iqd"`
	TopupsIQD  int64 `json:"topups_iqd"`
	Renewals   struct {
		Count     int   `json:"count"`
		AmountIQD int64 `json:"amount_iqd"`
	} `json:"renewals"`
	RefundsIQD int64 `json:"refunds_iqd"`
	ClosingIQD int64 `json:"closing_iqd"`
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
	from, to := parseRange(r)
	ctx := r.Context()

	var resp settlementResponse
	err := m.db.QueryRow(ctx,
		`SELECT COALESCE(sum(amount_iqd),0)::bigint FROM ledger_transactions
		  WHERE actor_manager_id = $1::uuid AND at < $2`, managerID, from).Scan(&resp.OpeningIQD)
	if err != nil {
		m.internalError(w, "settlement opening", err)
		return
	}
	err = m.db.QueryRow(ctx,
		`SELECT COALESCE(sum(amount_iqd),0)::bigint FROM ledger_transactions
		  WHERE actor_manager_id = $1::uuid AND at <= $2`, managerID, to).Scan(&resp.ClosingIQD)
	if err != nil {
		m.internalError(w, "settlement closing", err)
		return
	}
	err = m.db.QueryRow(ctx,
		`SELECT COALESCE(sum(amount_iqd),0)::bigint FROM ledger_transactions
		  WHERE actor_manager_id = $1::uuid AND type = 'topup' AND at >= $2 AND at < $3`,
		managerID, from, to).Scan(&resp.TopupsIQD)
	if err != nil {
		m.internalError(w, "settlement topups", err)
		return
	}
	var renewalDelta int64
	err = m.db.QueryRow(ctx,
		`SELECT count(*)::int, COALESCE(sum(amount_iqd),0)::bigint FROM ledger_transactions
		  WHERE actor_manager_id = $1::uuid AND type IN ('renewal','voucher_redeem')
		    AND at >= $2 AND at < $3`,
		managerID, from, to).Scan(&resp.Renewals.Count, &renewalDelta)
	if err != nil {
		m.internalError(w, "settlement renewals", err)
		return
	}
	resp.Renewals.AmountIQD = -renewalDelta
	err = m.db.QueryRow(ctx,
		`SELECT COALESCE(sum(amount_iqd),0)::bigint FROM ledger_transactions
		  WHERE actor_manager_id = $1::uuid AND type = 'refund' AND at >= $2 AND at < $3`,
		managerID, from, to).Scan(&resp.RefundsIQD)
	if err != nil {
		m.internalError(w, "settlement refunds", err)
		return
	}

	if r.URL.Query().Get("format") == "csv" {
		if !requireExport(w, r) {
			return
		}
		writeCSV(w, "settlement.csv",
			[]string{"opening_iqd", "topups_iqd", "renewals_count", "renewals_iqd", "refunds_iqd", "closing_iqd"},
			[][]string{{
				itoa64(resp.OpeningIQD), itoa64(resp.TopupsIQD), itoa(resp.Renewals.Count),
				itoa64(resp.Renewals.AmountIQD), itoa64(resp.RefundsIQD), itoa64(resp.ClosingIQD),
			}})
		return
	}
	httpapi.JSON(w, http.StatusOK, resp)
}
