package billing

// Manager balances (FR-20) — v2 phase 4 (FR-69.2/69.3): balances are
// PER-CURRENCY, derived from the ledger exactly as before but summed within
// one currency at a time, never across. Converting between a manager's
// currencies is an explicit exchange (FR-69.3) — the only path that ever
// moves value from one currency to another; nothing else (a renewal, a
// top-up, a refund) implicitly converts.

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
)

type balanceResponse struct {
	Currency string `json:"currency"`
	Balance  int64  `json:"balance"`
}

// balanceHandler serves GET /managers/{id}/balance?currency=. A manager may
// always read their own balance; reading another's requires the topup
// permission (agents see only their own header balance). currency defaults to
// IQD when omitted — the header-widget call sites that don't yet carry a
// specific currency context.
func (m *Module) balanceHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	mgr, _ := auth.ManagerFrom(r.Context())
	if mgr == nil || (mgr.ID != id && !mgr.Can(auth.PermTopup)) {
		httpapi.Error(w, http.StatusForbidden, "forbidden", "you may only view your own balance")
		return
	}
	// v3-2 (FR-100.1): the book is single IQD; any ?currency= is ignored.
	bal, err := m.readBalance(r, id)
	if err != nil {
		m.internalError(w, "read balance", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, balanceResponse{Currency: "IQD", Balance: bal})
}

// balancesHandler serves GET /managers/{id}/balances (v2 phase 4, FR-69.2,
// new plural endpoint): every currency the manager has ever touched.
func (m *Module) balancesHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	mgr, _ := auth.ManagerFrom(r.Context())
	if mgr == nil || (mgr.ID != id && !mgr.Can(auth.PermTopup)) {
		httpapi.Error(w, http.StatusForbidden, "forbidden", "you may only view your own balance")
		return
	}
	rows, err := m.db.Query(r.Context(),
		`SELECT currency, balance FROM manager_balances WHERE manager_id = $1::uuid ORDER BY currency`, id)
	if err != nil {
		m.internalError(w, "list balances", err)
		return
	}
	defer rows.Close()
	out := []balanceResponse{}
	for rows.Next() {
		var b balanceResponse
		if err := rows.Scan(&b.Currency, &b.Balance); err != nil {
			m.internalError(w, "scan balance", err)
			return
		}
		out = append(out, b)
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"balances": out})
}

// readBalance returns the manager's cached ledger-derived IQD balance (0 when
// the manager has no entries yet). v3-2: single IQD book.
func (m *Module) readBalance(r *http.Request, managerID string) (int64, error) {
	var bal int64
	err := m.db.QueryRow(r.Context(),
		`SELECT COALESCE((SELECT balance FROM manager_balances WHERE manager_id = $1::uuid AND currency = 'IQD'), 0)`,
		managerID).Scan(&bal)
	return bal, err
}

type topupRequest struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
	Note     string `json:"note"`
}

// topupHandler serves POST /managers/{id}/topup (permission topup, audited).
// v3-2 (FR-101.1): a foreign-currency top-up is converted to IQD at the current
// admin rate at entry and lands as ONE IQD credit stamped with the original
// figure + rate; a native-IQD top-up is unchanged. The book is single IQD.
func (m *Module) topupHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var in topupRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	if in.Amount <= 0 {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "amount", Message: "must be positive"})
		return
	}
	entered := in.Currency // "" or "IQD" = native IQD entry

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

	iqd, rateID, err := convertToIQD(r.Context(), tx, in.Amount, entered)
	if errors.Is(err, errNoRate) {
		httpapi.Error(w, http.StatusUnprocessableEntity, "no_rate",
			"no IQD exchange rate is defined for "+entered+" — add one on the rates screen first")
		return
	}
	if err != nil {
		m.internalError(w, "topup convert", err)
		return
	}

	entry := ledgerEntry{
		Type:           "topup",
		Amount:         iqd, // IQD credit
		ActorManagerID: id,
		Source:         "panel",
		Note:           in.Note,
		CurrencyRateID: rateID,
	}
	if entered != "" && entered != "IQD" {
		ea := in.Amount
		entry.EnteredAmount = &ea
		entry.EnteredCurrency = entered
	}
	txID, err := insertLedger(r.Context(), tx, entry)
	if err != nil {
		m.internalError(w, "topup insert", err)
		return
	}
	if err := recomputeBalance(r.Context(), tx, id); err != nil {
		m.internalError(w, "topup recompute", err)
		return
	}
	// FR-153, §3.1: staged dual-write, same transaction as the ledger row above.
	var dbNow time.Time
	_ = tx.QueryRow(r.Context(), `SELECT now()`).Scan(&dbNow)
	if err := postLedgerJournal(r.Context(), tx, dbNow.UTC(), entry, txID, 0, false, "billing.topup", id); err != nil {
		m.internalError(w, "topup journal", err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		m.internalError(w, "topup commit", err)
		return
	}

	bal, _ := m.readBalance(r, id)
	_ = auth.Audit(r.Context(), "manager.topup", "manager", id, nil, map[string]any{
		"ledger_tx_id": txID, "amount": iqd, "entered_amount": in.Amount, "entered_currency": entered,
		"balance": bal, "note": in.Note,
	})
	actorMgr, _ := auth.ManagerFrom(r.Context())
	undoID, _ := m.recordUndo(r.Context(), "topup", "manager", id, managerID(actorMgr), txID,
		undoTopupSnapshot{ManagerID: id, Amount: iqd}, "")
	httpapi.JSON(w, http.StatusOK, map[string]any{"ledger_tx_id": txID, "currency": "IQD", "balance": bal, "undo_id": undoID})
}

// --- Currency catalog + admin rate table (FR-68) ---------------------------

type currencyView struct {
	Code            string `json:"code"`
	MinorUnitDigits int    `json:"minor_unit_digits"`
	Symbol          string `json:"symbol"`
}

// currenciesHandler serves GET /currencies — the enabled catalog, for
// building panel currency selectors (FR-68.1).
func (m *Module) currenciesHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := m.db.Query(r.Context(),
		`SELECT code, minor_unit_digits, symbol FROM currencies WHERE enabled ORDER BY code`)
	if err != nil {
		m.internalError(w, "list currencies", err)
		return
	}
	defer rows.Close()
	out := []currencyView{}
	for rows.Next() {
		var c currencyView
		if err := rows.Scan(&c.Code, &c.MinorUnitDigits, &c.Symbol); err != nil {
			m.internalError(w, "scan currency", err)
			return
		}
		out = append(out, c)
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": out})
}

type currencyRateView struct {
	ID            string `json:"id"`
	FromCurrency  string `json:"from_currency"`
	ToCurrency    string `json:"to_currency"`
	Rate          string `json:"rate"`
	EffectiveFrom string `json:"effective_from"`
}

// listCurrencyRatesHandler serves GET /currency-rates?from=&to= (read-only,
// any authenticated manager — FR-68.4).
func (m *Module) listCurrencyRatesHandler(w http.ResponseWriter, r *http.Request) {
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	rows, err := m.db.Query(r.Context(),
		`SELECT id::text, from_currency, to_currency, rate::text, effective_from::text
		   FROM currency_rates
		  WHERE ($1 = '' OR from_currency = $1) AND ($2 = '' OR to_currency = $2)
		  ORDER BY effective_from DESC`, from, to)
	if err != nil {
		m.internalError(w, "list currency rates", err)
		return
	}
	defer rows.Close()
	out := []currencyRateView{}
	for rows.Next() {
		var v currencyRateView
		if err := rows.Scan(&v.ID, &v.FromCurrency, &v.ToCurrency, &v.Rate, &v.EffectiveFrom); err != nil {
			m.internalError(w, "scan currency rate", err)
			return
		}
		out = append(out, v)
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": out})
}

type createCurrencyRateRequest struct {
	FromCurrency string  `json:"from_currency" validate:"required"`
	ToCurrency   string  `json:"to_currency" validate:"required"`
	Rate         float64 `json:"rate" validate:"required"`
}

// createCurrencyRateHandler serves POST /currency-rates (currency_rates.manage
// permission, audited — FR-68.4). Always inserts a NEW row with
// effective_from=now(); never updates an existing one, so a rate already
// stamped on a ledger row can never be retroactively altered.
func (m *Module) createCurrencyRateHandler(w http.ResponseWriter, r *http.Request) {
	mgr, _ := auth.ManagerFrom(r.Context())
	var in createCurrencyRateRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	if in.Rate <= 0 {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "rate", Message: "must be positive"})
		return
	}
	var v currencyRateView
	err := m.db.QueryRow(r.Context(),
		`INSERT INTO currency_rates (from_currency, to_currency, rate, created_by)
		 VALUES ($1, $2, $3, NULLIF($4,'')::uuid)
		 RETURNING id::text, from_currency, to_currency, rate::text, effective_from::text`,
		in.FromCurrency, in.ToCurrency, in.Rate, managerID(mgr)).
		Scan(&v.ID, &v.FromCurrency, &v.ToCurrency, &v.Rate, &v.EffectiveFrom)
	if err != nil {
		m.internalError(w, "create currency rate", err)
		return
	}
	_ = auth.Audit(r.Context(), "currency_rate.create", "currency_rate", v.ID, nil, v)
	httpapi.JSON(w, http.StatusCreated, v)
}

func pow10(n int) float64 {
	f := 1.0
	for i := 0; i < n; i++ {
		f *= 10
	}
	return f
}
