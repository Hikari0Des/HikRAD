package billing

// Per-manager receiving accounts (v2-2, FR-77.2, contract C2). One row per
// (manager, provider): the exact account number/phone/IBAN/recipient name
// shown to THAT manager's subscribers. Deliberately not encrypted at rest —
// these are shown to subscribers, not secret — but every write is still
// audit-logged (a money-relevant fact worth a trail, per the source brief).

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
)

type providerAccountView struct {
	ID                   string  `json:"id"`
	ProviderID           string  `json:"provider_id"`
	AccountDetails       string  `json:"account_details"`
	InstructionsOverride *string `json:"instructions_override"`
}

// requireSelfOrAdmin 403s unless the caller is managerID themselves or holds
// the topup-class "acting on another manager's behalf" posture used
// elsewhere in this package (mirrors balance_api.go's own manager-or-admin
// check — every manager manages their own account/method rows; an admin may
// act for any manager).
func (m *Module) requireSelfOrAdmin(w http.ResponseWriter, r *http.Request, managerID string) bool {
	mgr, _ := auth.ManagerFrom(r.Context())
	if mgr == nil || (mgr.ID != managerID && !mgr.Can(auth.PermTopup)) {
		httpapi.Error(w, http.StatusForbidden, "forbidden", "you may only manage your own account")
		return false
	}
	return true
}

// listProviderAccountsHandler serves GET /managers/{id}/provider-accounts.
func (m *Module) listProviderAccountsHandler(w http.ResponseWriter, r *http.Request) {
	managerID := chi.URLParam(r, "id")
	if !m.requireSelfOrAdmin(w, r, managerID) {
		return
	}
	rows, err := m.db.Query(r.Context(),
		`SELECT id::text, provider_id::text, account_details, instructions_override
		   FROM manager_provider_accounts WHERE manager_id = $1::uuid ORDER BY created_at`, managerID)
	if err != nil {
		m.internalError(w, "list provider accounts", err)
		return
	}
	defer rows.Close()
	out := []providerAccountView{}
	for rows.Next() {
		var v providerAccountView
		if err := rows.Scan(&v.ID, &v.ProviderID, &v.AccountDetails, &v.InstructionsOverride); err != nil {
			m.internalError(w, "scan provider account", err)
			return
		}
		out = append(out, v)
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": out})
}

type putProviderAccountRequest struct {
	AccountDetails       string  `json:"account_details" validate:"required"`
	InstructionsOverride *string `json:"instructions_override"`
}

// putProviderAccountHandler serves PUT /managers/{id}/provider-accounts/{providerId}
// (self or admin, audited — upserts the UNIQUE (manager_id, provider_id) row).
func (m *Module) putProviderAccountHandler(w http.ResponseWriter, r *http.Request) {
	managerID := chi.URLParam(r, "id")
	providerID := chi.URLParam(r, "providerId")
	if !m.requireSelfOrAdmin(w, r, managerID) {
		return
	}
	var in putProviderAccountRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	if in.AccountDetails == "" {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "account_details", Message: "this field is required"})
		return
	}
	var v providerAccountView
	err := m.db.QueryRow(r.Context(),
		`INSERT INTO manager_provider_accounts (manager_id, provider_id, account_details, instructions_override)
		 VALUES ($1::uuid, $2::uuid, $3, $4)
		 ON CONFLICT (manager_id, provider_id) DO UPDATE SET
		   account_details = $3, instructions_override = $4, updated_at = now()
		 RETURNING id::text, provider_id::text, account_details, instructions_override`,
		managerID, providerID, in.AccountDetails, in.InstructionsOverride).
		Scan(&v.ID, &v.ProviderID, &v.AccountDetails, &v.InstructionsOverride)
	if err != nil {
		m.internalError(w, "put provider account", err)
		return
	}
	_ = auth.Audit(r.Context(), "manager_provider_account.put", "manager", managerID, nil, map[string]any{"provider_id": providerID})
	httpapi.JSON(w, http.StatusOK, v)
}
