package billing

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
)

type createFixedAssetRequest struct {
	Name             string `json:"name"`
	Cost             int64  `json:"cost"`
	AcquiredDate     string `json:"acquired_date"`
	UsefulLifeMonths int    `json:"useful_life_months"`
	SalvageValue     int64  `json:"salvage_value"`
}

func (m *Module) createFixedAssetHandler(w http.ResponseWriter, r *http.Request) {
	mgr, _ := auth.ManagerFrom(r.Context())
	var in createFixedAssetRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	var fe []httpapi.FieldError
	if in.Name == "" {
		fe = append(fe, httpapi.FieldError{Field: "name", Message: "this field is required"})
	}
	if in.Cost <= 0 {
		fe = append(fe, httpapi.FieldError{Field: "cost", Message: "must be positive"})
	}
	if in.UsefulLifeMonths <= 0 {
		fe = append(fe, httpapi.FieldError{Field: "useful_life_months", Message: "must be positive"})
	}
	if fe != nil {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed", fe...)
		return
	}
	tx, err := m.db.Begin(r.Context())
	if err != nil {
		m.internalError(w, "fixed asset begin", err)
		return
	}
	defer tx.Rollback(r.Context())
	at, err := nowIn(r.Context(), tx)
	if err != nil {
		m.internalError(w, "fixed asset now", err)
		return
	}
	acquired := at
	if in.AcquiredDate != "" {
		if t, err := time.Parse("2006-01-02", in.AcquiredDate); err == nil {
			acquired = t
		}
	}
	assetID, err := createFixedAsset(r.Context(), tx, at, createFixedAssetParams{
		Name: in.Name, Cost: in.Cost, AcquiredDate: acquired, UsefulLifeMonths: in.UsefulLifeMonths,
		SalvageValue: in.SalvageValue, CreatedBy: managerID(mgr), Source: "billing.fixed_asset",
	})
	if err != nil {
		m.internalError(w, "create fixed asset", err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		m.internalError(w, "fixed asset commit", err)
		return
	}
	_ = auth.Audit(r.Context(), "fixed_asset.create", "fixed_asset", assetID, nil, in)
	httpapi.JSON(w, http.StatusCreated, map[string]any{"id": assetID})
}

func (m *Module) listFixedAssetsHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := m.db.Query(r.Context(),
		`SELECT fa.id::text, fa.name, fa.cost, fa.useful_life_months, fa.salvage_value, fa.acquired_date::text,
		        COALESCE((SELECT sum(amount) FROM depreciation_entries WHERE fixed_asset_id = fa.id), 0)
		   FROM fixed_assets fa ORDER BY fa.acquired_date DESC`)
	if err != nil {
		m.internalError(w, "list fixed assets", err)
		return
	}
	defer rows.Close()
	type view struct {
		ID                      string `json:"id"`
		Name                    string `json:"name"`
		Cost                    int64  `json:"cost"`
		UsefulLifeMonths        int    `json:"useful_life_months"`
		SalvageValue            int64  `json:"salvage_value"`
		AcquiredDate            string `json:"acquired_date"`
		AccumulatedDepreciation int64  `json:"accumulated_depreciation"`
	}
	out := []view{}
	for rows.Next() {
		var v view
		if err := rows.Scan(&v.ID, &v.Name, &v.Cost, &v.UsefulLifeMonths, &v.SalvageValue, &v.AcquiredDate, &v.AccumulatedDepreciation); err != nil {
			m.internalError(w, "scan fixed asset", err)
			return
		}
		out = append(out, v)
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": out})
}

// runDepreciationHandler serves POST /fixed-assets/{id}/depreciate — an
// operator-triggered run for the current (server) month, rather than an
// automatic scheduler (v4-9 scope: the substrate + a manual trigger; a
// scheduled monthly job is a natural follow-up, not built here — see the
// gate result).
func (m *Module) runDepreciationHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	mgr, _ := auth.ManagerFrom(r.Context())
	tx, err := m.db.Begin(r.Context())
	if err != nil {
		m.internalError(w, "depreciation begin", err)
		return
	}
	defer tx.Rollback(r.Context())
	at, err := nowIn(r.Context(), tx)
	if err != nil {
		m.internalError(w, "depreciation now", err)
		return
	}
	posted, err := runDepreciation(r.Context(), tx, at, id, at, managerID(mgr), "billing.depreciation")
	if err != nil {
		m.internalError(w, "run depreciation", err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		m.internalError(w, "depreciation commit", err)
		return
	}
	if posted {
		_ = auth.Audit(r.Context(), "fixed_asset.depreciate", "fixed_asset", id, nil, nil)
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"posted": posted})
}
