package billing

// Manager balances (FR-20) — v2 phase 4 (FR-69.2/69.3): balances are
// PER-CURRENCY, derived from the ledger exactly as before but summed within
// one currency at a time, never across. Converting between a manager's
// currencies is an explicit exchange (FR-69.3) — the only path that ever
// moves value from one currency to another; nothing else (a renewal, a
// top-up, a refund) implicitly converts.

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/jackc/pgx/v5"
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
	currency := r.URL.Query().Get("currency")
	if currency == "" {
		currency = "IQD"
	}
	bal, err := m.readBalance(r, id, currency)
	if err != nil {
		m.internalError(w, "read balance", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, balanceResponse{Currency: currency, Balance: bal})
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

// readBalance returns the cached ledger-derived balance for one currency (0
// when the manager has no entries in it yet).
func (m *Module) readBalance(r *http.Request, managerID, currency string) (int64, error) {
	var bal int64
	err := m.db.QueryRow(r.Context(),
		`SELECT COALESCE((SELECT balance FROM manager_balances WHERE manager_id = $1::uuid AND currency = $2), 0)`,
		managerID, currency).Scan(&bal)
	return bal, err
}

type topupRequest struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
	Note     string `json:"note"`
}

// topupHandler serves POST /managers/{id}/topup (permission topup, audited).
// It appends a credit entry in the request's currency and recomputes the
// target manager's balance in that same currency.
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
	currency := in.Currency
	if currency == "" {
		currency = "IQD"
	}

	tx, err := m.db.Begin(r.Context())
	if err != nil {
		m.internalError(w, "topup begin", err)
		return
	}
	defer tx.Rollback(r.Context())

	if _, err := lockBalance(r.Context(), tx, id, currency); err != nil {
		m.internalError(w, "topup lock", err)
		return
	}
	txID, err := insertLedger(r.Context(), tx, ledgerEntry{
		Type:           "topup",
		Amount:         in.Amount, // credit
		Currency:       currency,
		ActorManagerID: id,
		Source:         "panel",
		Note:           in.Note,
	})
	if err != nil {
		m.internalError(w, "topup insert", err)
		return
	}
	if err := recomputeBalance(r.Context(), tx, id, currency); err != nil {
		m.internalError(w, "topup recompute", err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		m.internalError(w, "topup commit", err)
		return
	}

	bal, _ := m.readBalance(r, id, currency)
	_ = auth.Audit(r.Context(), "manager.topup", "manager", id, nil, map[string]any{
		"ledger_tx_id": txID, "amount": in.Amount, "currency": currency, "balance": bal, "note": in.Note,
	})
	httpapi.JSON(w, http.StatusOK, map[string]any{"ledger_tx_id": txID, "currency": currency, "balance": bal})
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

// --- Exchange (FR-69.3): the only currency-conversion path -----------------

type exchangeRequest struct {
	FromCurrency   string `json:"from_currency" validate:"required"`
	ToCurrency     string `json:"to_currency" validate:"required"`
	Amount         int64  `json:"amount" validate:"required"`
	CurrencyRateID string `json:"currency_rate_id" validate:"required"`
}

type exchangeResponse struct {
	ExchangeReference string `json:"exchange_reference"`
	FromLedgerTxID     string `json:"from_ledger_tx_id"`
	ToLedgerTxID       string `json:"to_ledger_tx_id"`
	FromBalance        int64  `json:"from_balance"`
	ToBalance          int64  `json:"to_balance"`
}

var (
	errExchangeSameCurrency  = errors.New("billing: cannot exchange a currency into itself")
	errExchangeBadRate       = errors.New("billing: currency_rate_id does not convert from_currency to to_currency")
	errExchangeInsufficient  = errors.New("billing: insufficient balance in from_currency")
)

// exchangeHandler serves POST /managers/{id}/exchange (FR-69.3). One
// transaction: locks both currency balance rows (alphabetical order to avoid
// deadlocking against a concurrent reverse exchange), verifies the given
// currency_rates row actually converts from_currency -> to_currency, inserts
// TWO linked ledger rows (a from_currency debit + a to_currency credit)
// sharing one reference and both stamped with the rate used, then recomputes
// both balances. This is the ONLY path that ever moves value between a
// manager's currencies.
func (m *Module) exchangeHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	mgr, _ := auth.ManagerFrom(r.Context())
	if mgr == nil || (mgr.ID != id && !mgr.Can(auth.PermTopup)) {
		httpapi.Error(w, http.StatusForbidden, "forbidden", "you may only exchange your own balance")
		return
	}
	var in exchangeRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	if in.FromCurrency == in.ToCurrency {
		httpapi.Error(w, http.StatusUnprocessableEntity, "same_currency", errExchangeSameCurrency.Error())
		return
	}
	if in.Amount <= 0 {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "amount", Message: "must be positive"})
		return
	}

	res, err := m.exchange(r.Context(), id, in)
	if err != nil {
		switch {
		case errors.Is(err, errExchangeBadRate):
			httpapi.Error(w, http.StatusUnprocessableEntity, "bad_rate", err.Error())
		case errors.Is(err, errExchangeInsufficient):
			httpapi.Error(w, http.StatusUnprocessableEntity, "insufficient_balance", err.Error())
		case errors.Is(err, pgx.ErrNoRows):
			httpapi.Error(w, http.StatusNotFound, "not_found", "currency rate not found")
		default:
			m.internalError(w, "exchange", err)
		}
		return
	}
	_ = auth.Audit(r.Context(), "manager.exchange", "manager", id, nil, res)
	httpapi.JSON(w, http.StatusOK, res)
}

func (m *Module) exchange(ctx context.Context, managerID string, in exchangeRequest) (exchangeResponse, error) {
	tx, err := m.db.Begin(ctx)
	if err != nil {
		return exchangeResponse{}, err
	}
	defer tx.Rollback(ctx)

	// Consistent lock order (alphabetical by currency code) so two concurrent
	// exchanges in opposite directions can never deadlock.
	first, second := in.FromCurrency, in.ToCurrency
	if second < first {
		first, second = second, first
	}
	if _, err := lockBalance(ctx, tx, managerID, first); err != nil {
		return exchangeResponse{}, err
	}
	if _, err := lockBalance(ctx, tx, managerID, second); err != nil {
		return exchangeResponse{}, err
	}

	var rateFrom, rateTo string
	var rate float64
	if err := tx.QueryRow(ctx,
		`SELECT from_currency, to_currency, rate FROM currency_rates WHERE id = $1::uuid`,
		in.CurrencyRateID).Scan(&rateFrom, &rateTo, &rate); err != nil {
		return exchangeResponse{}, err
	}
	if rateFrom != in.FromCurrency || rateTo != in.ToCurrency {
		return exchangeResponse{}, errExchangeBadRate
	}

	var fromDigits, toDigits int
	if err := tx.QueryRow(ctx, `SELECT minor_unit_digits FROM currencies WHERE code = $1`, in.FromCurrency).Scan(&fromDigits); err != nil {
		return exchangeResponse{}, err
	}
	if err := tx.QueryRow(ctx, `SELECT minor_unit_digits FROM currencies WHERE code = $1`, in.ToCurrency).Scan(&toDigits); err != nil {
		return exchangeResponse{}, err
	}
	// in.Amount is minor units of FromCurrency; convert to whole FromCurrency,
	// apply the whole-currency rate, convert to minor units of ToCurrency.
	fromWhole := float64(in.Amount) / pow10(fromDigits)
	toWhole := fromWhole * rate
	toAmount := int64(toWhole*pow10(toDigits) + 0.5) // round to nearest minor unit

	var fromBal int64
	if err := tx.QueryRow(ctx,
		`SELECT balance FROM manager_balances WHERE manager_id = $1::uuid AND currency = $2`,
		managerID, in.FromCurrency).Scan(&fromBal); err != nil {
		return exchangeResponse{}, err
	}
	if fromBal < in.Amount {
		return exchangeResponse{}, errExchangeInsufficient
	}

	reference := "EXG-" + randToken()
	fromTxID, err := insertLedger(ctx, tx, ledgerEntry{
		Type: "exchange", Amount: -in.Amount, Currency: in.FromCurrency,
		ActorManagerID: managerID, Source: "panel", Reference: reference, CurrencyRateID: in.CurrencyRateID,
	})
	if err != nil {
		return exchangeResponse{}, err
	}
	toTxID, err := insertLedger(ctx, tx, ledgerEntry{
		Type: "exchange", Amount: toAmount, Currency: in.ToCurrency,
		ActorManagerID: managerID, Source: "panel", Reference: reference, CurrencyRateID: in.CurrencyRateID,
	})
	if err != nil {
		return exchangeResponse{}, err
	}
	if err := recomputeBalance(ctx, tx, managerID, in.FromCurrency); err != nil {
		return exchangeResponse{}, err
	}
	if err := recomputeBalance(ctx, tx, managerID, in.ToCurrency); err != nil {
		return exchangeResponse{}, err
	}

	var newFromBal, newToBal int64
	if err := tx.QueryRow(ctx, `SELECT balance FROM manager_balances WHERE manager_id = $1::uuid AND currency = $2`,
		managerID, in.FromCurrency).Scan(&newFromBal); err != nil {
		return exchangeResponse{}, err
	}
	if err := tx.QueryRow(ctx, `SELECT balance FROM manager_balances WHERE manager_id = $1::uuid AND currency = $2`,
		managerID, in.ToCurrency).Scan(&newToBal); err != nil {
		return exchangeResponse{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return exchangeResponse{}, err
	}
	return exchangeResponse{
		ExchangeReference: reference, FromLedgerTxID: fromTxID, ToLedgerTxID: toTxID,
		FromBalance: newFromBal, ToBalance: newToBal,
	}, nil
}

func pow10(n int) float64 {
	f := 1.0
	for i := 0; i < n; i++ {
		f *= 10
	}
	return f
}
