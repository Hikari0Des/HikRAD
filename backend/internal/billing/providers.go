package billing

// Payment provider catalog (v2-2, FR-77.1, contract C1). Owner-managed —
// name only, no API fields, no online dependency anywhere (NFR-7). Edited
// in place: a provider's name/template is display metadata, not a
// money-affecting figure like FR-71's cost or FR-74's wholesale price, so
// there is no "what was the name at renewal time" question to preserve —
// unlike profile_cost_history/reseller_prices, this table is NOT
// append-only-versioned.

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/jackc/pgx/v5"
)

type paymentProviderView struct {
	ID                   string  `json:"id"`
	Name                 string  `json:"name"`
	LogoPath             *string `json:"logo_path"`
	InstructionsTemplate string  `json:"instructions_template"`
	Enabled              bool    `json:"enabled"`
}

// listProvidersHandler serves GET /payment-providers (any authenticated
// manager, read-only — every manager needs the catalog to configure their
// own account against it).
func (m *Module) listProvidersHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := m.db.Query(r.Context(),
		`SELECT id::text, name, logo_path, instructions_template, enabled
		   FROM payment_providers ORDER BY name`)
	if err != nil {
		m.internalError(w, "list payment providers", err)
		return
	}
	defer rows.Close()
	out := []paymentProviderView{}
	for rows.Next() {
		var v paymentProviderView
		if err := rows.Scan(&v.ID, &v.Name, &v.LogoPath, &v.InstructionsTemplate, &v.Enabled); err != nil {
			m.internalError(w, "scan payment provider", err)
			return
		}
		out = append(out, v)
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": out})
}

type createProviderRequest struct {
	Name                 string `json:"name" validate:"required"`
	InstructionsTemplate string `json:"instructions_template"`
}

// createProviderHandler serves POST /payment-providers (permission
// payment_providers.manage, admin-only, audited).
func (m *Module) createProviderHandler(w http.ResponseWriter, r *http.Request) {
	var in createProviderRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	if in.Name == "" {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "name", Message: "this field is required"})
		return
	}
	var v paymentProviderView
	err := m.db.QueryRow(r.Context(),
		`INSERT INTO payment_providers (name, instructions_template) VALUES ($1, $2)
		 RETURNING id::text, name, logo_path, instructions_template, enabled`,
		in.Name, in.InstructionsTemplate).
		Scan(&v.ID, &v.Name, &v.LogoPath, &v.InstructionsTemplate, &v.Enabled)
	if err != nil {
		m.internalError(w, "create payment provider", err)
		return
	}
	_ = auth.Audit(r.Context(), "payment_provider.create", "payment_provider", v.ID, nil, v)
	httpapi.JSON(w, http.StatusCreated, v)
}

type updateProviderRequest struct {
	Name                 *string `json:"name"`
	InstructionsTemplate *string `json:"instructions_template"`
	Enabled              *bool   `json:"enabled"`
}

// updateProviderHandler serves PUT /payment-providers/{id} (permission
// payment_providers.manage, admin-only, audited). Edited in place — see
// package doc comment for why this table is not versioned.
func (m *Module) updateProviderHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var in updateProviderRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	var v paymentProviderView
	err := m.db.QueryRow(r.Context(),
		`UPDATE payment_providers SET
		   name = COALESCE($2, name),
		   instructions_template = COALESCE($3, instructions_template),
		   enabled = COALESCE($4, enabled)
		 WHERE id = $1::uuid
		 RETURNING id::text, name, logo_path, instructions_template, enabled`,
		id, in.Name, in.InstructionsTemplate, in.Enabled).
		Scan(&v.ID, &v.Name, &v.LogoPath, &v.InstructionsTemplate, &v.Enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "payment provider not found")
		return
	}
	if err != nil {
		m.internalError(w, "update payment provider", err)
		return
	}
	_ = auth.Audit(r.Context(), "payment_provider.update", "payment_provider", v.ID, nil, v)
	httpapi.JSON(w, http.StatusOK, v)
}
