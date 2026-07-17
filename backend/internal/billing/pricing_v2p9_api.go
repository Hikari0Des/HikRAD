package billing

// v2 phase 9 (FR-71/FR-73/FR-74, contract C7): admin-only CRUD for plan
// cost, overheads, and reseller wholesale pricing. All three are append-only
// from the API's perspective — a submission is always a new row, never an
// UPDATE of an old one — the same posture v2-4 already established for
// currency_rates (FR-68.4), for the same reason: a past renewal's stamped
// cost_at_sale (or resolved wholesale price) must stay independently
// re-derivable from history even after the "current" figure changes.

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
)

// --- Plan cost price (FR-71) -------------------------------------------------

type costHistoryView struct {
	ID            string `json:"id"`
	Cost          int64  `json:"cost"`
	Currency      string `json:"currency"`
	EffectiveFrom string `json:"effective_from"`
}

type createCostRequest struct {
	Cost     int64  `json:"cost" validate:"required"`
	Currency string `json:"currency" validate:"required"`
}

// createProfileCostHandler serves POST /profiles/{id}/cost (permission
// profiles.edit — cost is plan data, same gate as editing the plan itself).
func (m *Module) createProfileCostHandler(w http.ResponseWriter, r *http.Request) {
	profileID := chi.URLParam(r, "id")
	mgr, _ := auth.ManagerFrom(r.Context())
	var in createCostRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	if in.Cost < 0 {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "cost", Message: "must not be negative"})
		return
	}
	var v costHistoryView
	err := m.db.QueryRow(r.Context(),
		`INSERT INTO profile_cost_history (profile_id, cost, currency, created_by)
		 VALUES ($1::uuid, $2, $3, NULLIF($4,'')::uuid)
		 RETURNING id::text, cost, currency, effective_from::text`,
		profileID, in.Cost, in.Currency, managerID(mgr)).
		Scan(&v.ID, &v.Cost, &v.Currency, &v.EffectiveFrom)
	if err != nil {
		m.internalError(w, "create profile cost", err)
		return
	}
	_ = auth.Audit(r.Context(), "profile.cost.create", "profile", profileID, nil, v)
	httpapi.JSON(w, http.StatusCreated, v)
}

// listProfileCostHistoryHandler serves GET /profiles/{id}/cost-history
// (permission profiles.edit — same reasoning as create: this is plan-cost
// data, never exposed to a reseller, see C8's scoping contract).
func (m *Module) listProfileCostHistoryHandler(w http.ResponseWriter, r *http.Request) {
	profileID := chi.URLParam(r, "id")
	rows, err := m.db.Query(r.Context(),
		`SELECT id::text, cost, currency, effective_from::text FROM profile_cost_history
		  WHERE profile_id = $1::uuid ORDER BY effective_from DESC`, profileID)
	if err != nil {
		m.internalError(w, "list profile cost history", err)
		return
	}
	defer rows.Close()
	out := []costHistoryView{}
	for rows.Next() {
		var v costHistoryView
		if err := rows.Scan(&v.ID, &v.Cost, &v.Currency, &v.EffectiveFrom); err != nil {
			m.internalError(w, "scan cost history", err)
			return
		}
		out = append(out, v)
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": out})
}

// --- Overheads (FR-73) -------------------------------------------------------

type overheadView struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Amount      int64   `json:"amount"`
	Currency    string  `json:"currency"`
	NASID       *string `json:"nas_id"`
	PeriodStart string  `json:"period_start"`
	PeriodEnd   *string `json:"period_end"`
	Notes       string  `json:"notes"`
}

type createOverheadRequest struct {
	Name        string  `json:"name" validate:"required"`
	Amount      int64   `json:"amount" validate:"required"`
	Currency    string  `json:"currency" validate:"required"`
	NASID       *string `json:"nas_id"`
	PeriodStart string  `json:"period_start" validate:"required"`
	PeriodEnd   *string `json:"period_end"`
	Notes       string  `json:"notes"`
}

// createOverheadHandler serves POST /overheads (permission overheads.manage,
// admin-only — business-cost data, never reseller-visible per C8).
func (m *Module) createOverheadHandler(w http.ResponseWriter, r *http.Request) {
	mgr, _ := auth.ManagerFrom(r.Context())
	var in createOverheadRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	var fe []httpapi.FieldError
	if in.Name == "" {
		fe = append(fe, httpapi.FieldError{Field: "name", Message: "this field is required"})
	}
	if in.Amount < 0 {
		fe = append(fe, httpapi.FieldError{Field: "amount", Message: "must not be negative"})
	}
	if fe != nil {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed", fe...)
		return
	}
	var v overheadView
	err := m.db.QueryRow(r.Context(),
		`INSERT INTO overheads (name, amount, currency, nas_id, period_start, period_end, notes, created_by)
		 VALUES ($1, $2, $3, $4::uuid, $5, $6, $7, NULLIF($8,'')::uuid)
		 RETURNING id::text, name, amount, currency, nas_id::text, period_start::text, period_end::text, notes`,
		in.Name, in.Amount, in.Currency, in.NASID, in.PeriodStart, in.PeriodEnd, in.Notes, managerID(mgr)).
		Scan(&v.ID, &v.Name, &v.Amount, &v.Currency, &v.NASID, &v.PeriodStart, &v.PeriodEnd, &v.Notes)
	if err != nil {
		m.internalError(w, "create overhead", err)
		return
	}
	_ = auth.Audit(r.Context(), "overhead.create", "overhead", v.ID, nil, v)
	httpapi.JSON(w, http.StatusCreated, v)
}

// listOverheadsHandler serves GET /overheads?nas_id=&as_of= (permission
// overheads.manage). as_of (RFC3339, default now) selects rows whose period
// covers that instant; nas_id filters to one site, or omit for every row
// (global and every site) so the panel can build one screen.
func (m *Module) listOverheadsHandler(w http.ResponseWriter, r *http.Request) {
	nasID := r.URL.Query().Get("nas_id")
	asOf := time.Now().UTC()
	if v := r.URL.Query().Get("as_of"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			asOf = t
		}
	}
	rows, err := m.db.Query(r.Context(),
		`SELECT id::text, name, amount, currency, nas_id::text, period_start::text, period_end::text, notes
		   FROM overheads
		  WHERE ($1 = '' OR nas_id::text = $1)
		    AND period_start <= $2 AND (period_end IS NULL OR period_end >= $2)
		  ORDER BY period_start DESC`, nasID, asOf)
	if err != nil {
		m.internalError(w, "list overheads", err)
		return
	}
	defer rows.Close()
	out := []overheadView{}
	for rows.Next() {
		var v overheadView
		if err := rows.Scan(&v.ID, &v.Name, &v.Amount, &v.Currency, &v.NASID, &v.PeriodStart, &v.PeriodEnd, &v.Notes); err != nil {
			m.internalError(w, "scan overhead", err)
			return
		}
		out = append(out, v)
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": out})
}

// --- Reseller (wholesale) pricing (FR-74) -----------------------------------

type resellerPriceView struct {
	ID            string  `json:"id"`
	ManagerID     string  `json:"manager_id"`
	ProfileID     string  `json:"profile_id"`
	SubscriberID  *string `json:"subscriber_id"`
	Price         int64   `json:"price"`
	Currency      string  `json:"currency"`
	EffectiveFrom string  `json:"effective_from"`
}

type createResellerPriceRequest struct {
	ManagerID    string  `json:"manager_id" validate:"required"`
	ProfileID    string  `json:"profile_id" validate:"required"`
	SubscriberID *string `json:"subscriber_id"`
	Price        int64   `json:"price" validate:"required"`
	Currency     string  `json:"currency" validate:"required"`
}

// createResellerPriceHandler serves POST /reseller-prices (permission
// reseller_prices.manage, admin-only — a reseller never sets their own
// wholesale price). subscriber_id omitted/null = this reseller's plan-wide
// price (FR-74.1); set = a per-subscriber override.
func (m *Module) createResellerPriceHandler(w http.ResponseWriter, r *http.Request) {
	mgr, _ := auth.ManagerFrom(r.Context())
	var in createResellerPriceRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	if in.Price < 0 {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "price", Message: "must not be negative"})
		return
	}
	var v resellerPriceView
	err := m.db.QueryRow(r.Context(),
		`INSERT INTO reseller_prices (manager_id, profile_id, subscriber_id, price, currency, created_by)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, NULLIF($6,'')::uuid)
		 RETURNING id::text, manager_id::text, profile_id::text, subscriber_id::text, price, currency, effective_from::text`,
		in.ManagerID, in.ProfileID, in.SubscriberID, in.Price, in.Currency, managerID(mgr)).
		Scan(&v.ID, &v.ManagerID, &v.ProfileID, &v.SubscriberID, &v.Price, &v.Currency, &v.EffectiveFrom)
	if err != nil {
		m.internalError(w, "create reseller price", err)
		return
	}
	_ = auth.Audit(r.Context(), "reseller_price.create", "reseller_price", v.ID, nil, v)
	httpapi.JSON(w, http.StatusCreated, v)
}

// listResellerPricesHandler serves GET /reseller-prices?manager_id=&profile_id=
// (permission reseller_prices.manage, admin-only — C8: a reseller cannot call
// this for another reseller, or at all for the wholesale-price value itself).
func (m *Module) listResellerPricesHandler(w http.ResponseWriter, r *http.Request) {
	managerIDFilter := r.URL.Query().Get("manager_id")
	profileIDFilter := r.URL.Query().Get("profile_id")
	rows, err := m.db.Query(r.Context(),
		`SELECT id::text, manager_id::text, profile_id::text, subscriber_id::text, price, currency, effective_from::text
		   FROM reseller_prices
		  WHERE ($1 = '' OR manager_id::text = $1) AND ($2 = '' OR profile_id::text = $2)
		  ORDER BY effective_from DESC`, managerIDFilter, profileIDFilter)
	if err != nil {
		m.internalError(w, "list reseller prices", err)
		return
	}
	defer rows.Close()
	out := []resellerPriceView{}
	for rows.Next() {
		var v resellerPriceView
		if err := rows.Scan(&v.ID, &v.ManagerID, &v.ProfileID, &v.SubscriberID, &v.Price, &v.Currency, &v.EffectiveFrom); err != nil {
			m.internalError(w, "scan reseller price", err)
			return
		}
		out = append(out, v)
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": out})
}
