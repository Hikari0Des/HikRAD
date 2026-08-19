package billing

// Chart of accounts HTTP surface (FR-153). Read is available to any caller
// with accounting.view (statements/journal need to render account names);
// writes (adding a non-system account, e.g. a manager's own second bank
// account) are admin-only via accounting.manage. System (seeded) rows can
// never be edited or archived through this API — is_system is a database
// fact, not a toggle.

import (
	"net/http"

	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
)

type accountView struct {
	ID            string  `json:"id"`
	Code          string  `json:"code"`
	Name          string  `json:"name"`
	Type          string  `json:"type"`
	Subtype       *string `json:"subtype"`
	NormalBalance string  `json:"normal_balance"`
	IsSystem      bool    `json:"is_system"`
	Archived      bool    `json:"archived"`
}

func (m *Module) listAccountsHandler(w http.ResponseWriter, r *http.Request) {
	typeFilter := r.URL.Query().Get("type")
	rows, err := m.db.Query(r.Context(),
		`SELECT id::text, code, name, type, subtype, normal_balance, is_system, archived
		   FROM accounts WHERE ($1 = '' OR type = $1) ORDER BY code`, typeFilter)
	if err != nil {
		m.internalError(w, "list accounts", err)
		return
	}
	defer rows.Close()
	out := []accountView{}
	for rows.Next() {
		var v accountView
		if err := rows.Scan(&v.ID, &v.Code, &v.Name, &v.Type, &v.Subtype, &v.NormalBalance, &v.IsSystem, &v.Archived); err != nil {
			m.internalError(w, "scan account", err)
			return
		}
		out = append(out, v)
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": out})
}

type createAccountRequest struct {
	Code    string  `json:"code" validate:"required"`
	Name    string  `json:"name" validate:"required"`
	Type    string  `json:"type" validate:"required"`
	Subtype *string `json:"subtype"`
}

var validAccountTypes = map[string]bool{"asset": true, "liability": true, "equity": true, "revenue": true, "expense": true}

// normalBalanceFor returns the conventional normal balance side for an
// account type — debit for asset/expense, credit for liability/equity/
// revenue.
func normalBalanceFor(accountType string) string {
	if accountType == "asset" || accountType == "expense" {
		return "debit"
	}
	return "credit"
}

func (m *Module) createAccountHandler(w http.ResponseWriter, r *http.Request) {
	var in createAccountRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	var fe []httpapi.FieldError
	if in.Code == "" {
		fe = append(fe, httpapi.FieldError{Field: "code", Message: "this field is required"})
	}
	if in.Name == "" {
		fe = append(fe, httpapi.FieldError{Field: "name", Message: "this field is required"})
	}
	if !validAccountTypes[in.Type] {
		fe = append(fe, httpapi.FieldError{Field: "type", Message: "must be one of asset, liability, equity, revenue, expense"})
	}
	if fe != nil {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed", fe...)
		return
	}
	var v accountView
	err := m.db.QueryRow(r.Context(),
		`INSERT INTO accounts (code, name, type, subtype, normal_balance, is_system)
		 VALUES ($1, $2, $3, $4, $5, false)
		 RETURNING id::text, code, name, type, subtype, normal_balance, is_system, archived`,
		in.Code, in.Name, in.Type, in.Subtype, normalBalanceFor(in.Type)).
		Scan(&v.ID, &v.Code, &v.Name, &v.Type, &v.Subtype, &v.NormalBalance, &v.IsSystem, &v.Archived)
	if isUniqueViolation(err) {
		httpapi.Error(w, http.StatusConflict, "duplicate_code", "an account with this code already exists")
		return
	}
	if err != nil {
		m.internalError(w, "create account", err)
		return
	}
	_ = auth.Audit(r.Context(), "account.create", "account", v.ID, nil, v)
	httpapi.JSON(w, http.StatusCreated, v)
}
