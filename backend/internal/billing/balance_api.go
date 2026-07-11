package billing

// Manager balances (FR-20): the ledger-derived, cached balance and admin top-ups.
// Balance is never a stored-and-edited field — a top-up is a credit ledger entry
// and the cache is recomputed from the ledger inside the same transaction.

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
)

type balanceResponse struct {
	BalanceIQD int64 `json:"balance_iqd"`
}

// balanceHandler serves GET /managers/{id}/balance. A manager may always read
// their own balance; reading another's requires the topup permission (agents see
// only their own header balance).
func (m *Module) balanceHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	mgr, _ := auth.ManagerFrom(r.Context())
	if mgr == nil || (mgr.ID != id && !mgr.Can(auth.PermTopup)) {
		httpapi.Error(w, http.StatusForbidden, "forbidden", "you may only view your own balance")
		return
	}
	bal, err := m.readBalance(r, id)
	if err != nil {
		m.internalError(w, "read balance", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, balanceResponse{BalanceIQD: bal})
}

// readBalance returns the cached ledger-derived balance (0 when the manager has
// no entries yet).
func (m *Module) readBalance(r *http.Request, managerID string) (int64, error) {
	var bal int64
	err := m.db.QueryRow(r.Context(),
		`SELECT COALESCE((SELECT balance_iqd FROM manager_balances WHERE manager_id = $1::uuid), 0)`,
		managerID).Scan(&bal)
	return bal, err
}

type topupRequest struct {
	AmountIQD int64  `json:"amount_iqd"`
	Note      string `json:"note"`
}

// topupHandler serves POST /managers/{id}/topup (permission topup, audited). It
// appends a credit entry and recomputes the target manager's balance.
func (m *Module) topupHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var in topupRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	if in.AmountIQD <= 0 {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "amount_iqd", Message: "must be positive"})
		return
	}

	tx, err := m.db.Begin(r.Context())
	if err != nil {
		m.internalError(w, "topup begin", err)
		return
	}
	defer tx.Rollback(r.Context())

	if _, err := lockBalance(r.Context(), tx, id); err != nil {
		m.internalError(w, "topup lock", err)
		return
	}
	txID, err := insertLedger(r.Context(), tx, ledgerEntry{
		Type:           "topup",
		AmountIQD:      in.AmountIQD, // credit
		ActorManagerID: id,
		Source:         "panel",
		Note:           in.Note,
	})
	if err != nil {
		m.internalError(w, "topup insert", err)
		return
	}
	if err := recomputeBalance(r.Context(), tx, id); err != nil {
		m.internalError(w, "topup recompute", err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		m.internalError(w, "topup commit", err)
		return
	}

	bal, _ := m.readBalance(r, id)
	_ = auth.Audit(r.Context(), "manager.topup", "manager", id, nil, map[string]any{
		"ledger_tx_id": txID, "amount_iqd": in.AmountIQD, "balance_iqd": bal, "note": in.Note,
	})
	httpapi.JSON(w, http.StatusOK, map[string]any{"ledger_tx_id": txID, "balance_iqd": bal})
}
