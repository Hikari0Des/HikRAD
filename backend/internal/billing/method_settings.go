package billing

// Per-manager method enablement + Pay-method resolution (v2-2, FR-77.3/77.4,
// contracts C3/C4). C4 is the kickoff-blocker contract: a subscriber sees
// EXACTLY their owning manager's enabled, account-configured methods — no
// fallback to a global/admin account exists anywhere in this resolution
// (Decision 37).

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hikrad/hikrad/internal/auth"
	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	methodKeyScratchCard = "scratch_card"
	methodKeyVoucher     = "voucher"
)

type methodSettingView struct {
	MethodKey string `json:"method_key"`
	Enabled   bool   `json:"enabled"`
}

// listMethodSettingsHandler serves GET /managers/{id}/method-settings — every
// catalog provider plus the two built-ins, defaulting enabled=false for any
// method with no row yet (FR-77.3's "absence is off" rule).
func (m *Module) listMethodSettingsHandler(w http.ResponseWriter, r *http.Request) {
	managerID := chi.URLParam(r, "id")
	if !m.requireSelfOrAdmin(w, r, managerID) {
		return
	}
	rows, err := m.db.Query(r.Context(),
		`SELECT p.id::text AS method_key, COALESCE(s.enabled, false) AS enabled
		   FROM payment_providers p
		   LEFT JOIN manager_method_settings s ON s.manager_id = $1::uuid AND s.method_key = p.id::text
		  WHERE p.enabled
		  UNION ALL
		 SELECT k.method_key, COALESCE(s.enabled, false)
		   FROM (VALUES ($2), ($3)) AS k(method_key)
		   LEFT JOIN manager_method_settings s ON s.manager_id = $1::uuid AND s.method_key = k.method_key
		  ORDER BY 1`,
		managerID, methodKeyScratchCard, methodKeyVoucher)
	if err != nil {
		m.internalError(w, "list method settings", err)
		return
	}
	defer rows.Close()
	out := []methodSettingView{}
	for rows.Next() {
		var v methodSettingView
		if err := rows.Scan(&v.MethodKey, &v.Enabled); err != nil {
			m.internalError(w, "scan method setting", err)
			return
		}
		out = append(out, v)
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": out})
}

type putMethodSettingRequest struct {
	MethodKey string `json:"method_key" validate:"required"`
	Enabled   bool   `json:"enabled"`
}

// putMethodSettingHandler serves PUT /managers/{id}/method-settings (self or
// admin, audited).
func (m *Module) putMethodSettingHandler(w http.ResponseWriter, r *http.Request) {
	managerID := chi.URLParam(r, "id")
	if !m.requireSelfOrAdmin(w, r, managerID) {
		return
	}
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
		`INSERT INTO manager_method_settings (manager_id, method_key, enabled) VALUES ($1::uuid, $2, $3)
		 ON CONFLICT (manager_id, method_key) DO UPDATE SET enabled = $3`,
		managerID, in.MethodKey, in.Enabled)
	if err != nil {
		m.internalError(w, "put method setting", err)
		return
	}
	_ = auth.Audit(r.Context(), "manager_method_setting.put", "manager", managerID, nil, in)
	httpapi.JSON(w, http.StatusOK, methodSettingView{MethodKey: in.MethodKey, Enabled: in.Enabled})
}

// --- Resolved Pay-method list (C4) ------------------------------------------

// PayMethod is one tile the subscriber's Pay screen may render.
type PayMethod struct {
	Key                  string  `json:"key"`  // provider id, or "scratch_card" / "voucher"
	Kind                 string  `json:"kind"` // "provider" | "scratch_card" | "voucher"
	ProviderName         *string `json:"provider_name,omitempty"`
	AccountDetails       *string `json:"account_details,omitempty"`
	InstructionsText     *string `json:"instructions_text,omitempty"`
}

// resolvePayMethods returns exactly what subscriberID's owning manager has
// BOTH enabled AND (for a provider) configured an account for. An OWNED
// subscriber never falls back to any other manager's methods (Decision 37);
// a subscriber with NO owner resolves from the instance-level defaults
// (owner decision 2026-07-19, migration 0592) instead of an empty list.
func resolvePayMethods(ctx context.Context, db *pgxpool.Pool, subscriberID string) ([]PayMethod, error) {
	var ownerID *string
	if err := db.QueryRow(ctx, `SELECT owner_manager_id::text FROM subscribers WHERE id = $1::uuid`, subscriberID).
		Scan(&ownerID); err != nil {
		return nil, err
	}
	if ownerID == nil || *ownerID == "" {
		return resolveInstancePayMethods(ctx, db)
	}

	out := []PayMethod{}

	// Provider methods: enabled AND an account configured — both required,
	// enforced by the JOIN itself (an absent manager_provider_accounts row
	// silently excludes the provider, never errors — a manager mid-setup
	// must never break their subscribers' Pay screen).
	rows, err := db.Query(ctx,
		`SELECT p.id::text, p.name, a.account_details, COALESCE(a.instructions_override, p.instructions_template)
		   FROM manager_method_settings s
		   JOIN payment_providers p ON p.id::text = s.method_key AND p.enabled
		   JOIN manager_provider_accounts a ON a.manager_id = s.manager_id AND a.provider_id = p.id
		  WHERE s.manager_id = $1::uuid AND s.enabled
		  ORDER BY p.name`, *ownerID)
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

	// Built-in methods: scratch_card / voucher, enabled only.
	brows, err := db.Query(ctx,
		`SELECT method_key FROM manager_method_settings
		  WHERE manager_id = $1::uuid AND enabled AND method_key IN ($2, $3)`,
		*ownerID, methodKeyScratchCard, methodKeyVoucher)
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
