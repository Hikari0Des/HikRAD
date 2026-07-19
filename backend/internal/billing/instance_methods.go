package billing

// Instance-level default payment methods (owner decision 2026-07-19, amending
// v2-2's Decision 37; migration 0592). These rows mirror the per-manager
// method-settings/provider-account pair but apply ONLY to subscribers with no
// owning manager — resolvePayMethods branches on owner_manager_id IS NULL and
// never mixes the two scopes. Managed by admins holding
// payment_providers.manage, audited like their per-manager counterparts.

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

// listInstanceMethodSettingsHandler serves GET /instance/method-settings —
// every catalog provider plus the two built-ins, absence = off (same shape as
// the per-manager listing).
func (m *Module) listInstanceMethodSettingsHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := m.db.Query(r.Context(),
		`SELECT p.id::text AS method_key, COALESCE(s.enabled, false) AS enabled
		   FROM payment_providers p
		   LEFT JOIN instance_method_settings s ON s.method_key = p.id::text
		  WHERE p.enabled
		  UNION ALL
		 SELECT k.method_key, COALESCE(s.enabled, false)
		   FROM (VALUES ($1), ($2)) AS k(method_key)
		   LEFT JOIN instance_method_settings s ON s.method_key = k.method_key
		  ORDER BY 1`,
		methodKeyScratchCard, methodKeyVoucher)
	if err != nil {
		m.internalError(w, "list instance method settings", err)
		return
	}
	defer rows.Close()
	out := []methodSettingView{}
	for rows.Next() {
		var v methodSettingView
		if err := rows.Scan(&v.MethodKey, &v.Enabled); err != nil {
			m.internalError(w, "scan instance method setting", err)
			return
		}
		out = append(out, v)
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": out})
}

// putInstanceMethodSettingHandler serves PUT /instance/method-settings.
func (m *Module) putInstanceMethodSettingHandler(w http.ResponseWriter, r *http.Request) {
	var in putMethodSettingRequest
	if !httpapi.Bind(w, r, &in) {
		return
	}
	if in.MethodKey == "" {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation_failed", "request validation failed",
			httpapi.FieldError{Field: "method_key", Message: "this field is required"})
		return
	}
	_, err := m.db.Exec(r.Context(),
		`INSERT INTO instance_method_settings (method_key, enabled) VALUES ($1, $2)
		 ON CONFLICT (method_key) DO UPDATE SET enabled = $2`,
		in.MethodKey, in.Enabled)
	if err != nil {
		m.internalError(w, "put instance method setting", err)
		return
	}
	_ = auth.Audit(r.Context(), "instance_method_setting.put", "settings", in.MethodKey, nil, in)
	httpapi.JSON(w, http.StatusOK, methodSettingView{MethodKey: in.MethodKey, Enabled: in.Enabled})
}

// listInstanceProviderAccountsHandler serves GET /instance/provider-accounts.
func (m *Module) listInstanceProviderAccountsHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := m.db.Query(r.Context(),
		`SELECT provider_id::text, provider_id::text, account_details, instructions_override
		   FROM instance_provider_accounts ORDER BY created_at`)
	if err != nil {
		m.internalError(w, "list instance provider accounts", err)
		return
	}
	defer rows.Close()
	out := []providerAccountView{}
	for rows.Next() {
		var v providerAccountView
		if err := rows.Scan(&v.ID, &v.ProviderID, &v.AccountDetails, &v.InstructionsOverride); err != nil {
			m.internalError(w, "scan instance provider account", err)
			return
		}
		out = append(out, v)
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": out})
}

// putInstanceProviderAccountHandler serves PUT /instance/provider-accounts/{providerId}.
func (m *Module) putInstanceProviderAccountHandler(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "providerId")
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
		`INSERT INTO instance_provider_accounts (provider_id, account_details, instructions_override)
		 VALUES ($1::uuid, $2, $3)
		 ON CONFLICT (provider_id) DO UPDATE SET
		   account_details = $2, instructions_override = $3, updated_at = now()
		 RETURNING provider_id::text, provider_id::text, account_details, instructions_override`,
		providerID, in.AccountDetails, in.InstructionsOverride).
		Scan(&v.ID, &v.ProviderID, &v.AccountDetails, &v.InstructionsOverride)
	if err != nil {
		m.internalError(w, "put instance provider account", err)
		return
	}
	_ = auth.Audit(r.Context(), "instance_provider_account.put", "settings", providerID, nil, map[string]any{"provider_id": providerID})
	httpapi.JSON(w, http.StatusOK, v)
}

// resolveInstancePayMethods is the owner_manager_id IS NULL branch of
// resolvePayMethods: same enabled-AND-configured JOIN rules, against the
// instance tables.
func resolveInstancePayMethods(ctx context.Context, db *pgxpool.Pool) ([]PayMethod, error) {
	out := []PayMethod{}

	rows, err := db.Query(ctx,
		`SELECT p.id::text, p.name, a.account_details, COALESCE(a.instructions_override, p.instructions_template)
		   FROM instance_method_settings s
		   JOIN payment_providers p ON p.id::text = s.method_key AND p.enabled
		   JOIN instance_provider_accounts a ON a.provider_id = p.id
		  WHERE s.enabled
		  ORDER BY p.name`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var pm PayMethod
		pm.Kind = "provider"
		if err := rows.Scan(&pm.Key, &pm.ProviderName, &pm.AccountDetails, &pm.InstructionsText); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, pm)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	brows, err := db.Query(ctx,
		`SELECT method_key FROM instance_method_settings
		  WHERE enabled AND method_key IN ($1, $2)`,
		methodKeyScratchCard, methodKeyVoucher)
	if err != nil {
		return nil, err
	}
	defer brows.Close()
	for brows.Next() {
		var key string
		if err := brows.Scan(&key); err != nil {
			return nil, err
		}
		out = append(out, PayMethod{Key: key, Kind: key})
	}
	return out, brows.Err()
}
