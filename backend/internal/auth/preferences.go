package auth

// Per-manager preferences (v2-6, FR-84.1, contract C1). Presentation-only: no
// code outside this file may read Preferences/notification_prefs to decide a
// permission check, a ScopeFilter result, or a monetary amount/currency (gate
// item 5 greps for that boundary).

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NotifChannels is the per-rule notification delivery choice (FR-84.1).
type NotifChannels struct {
	InApp bool `json:"in_app"`
	Push  bool `json:"push"`
}

// Preferences is the doc shape stored in manager_preferences.doc at
// schema_version 1. "" / 0 / an absent map key all mean "unset — the client
// falls back to its own default," never "explicitly set to the zero value."
type Preferences struct {
	Language          string                   `json:"language,omitempty"`
	Theme             string                   `json:"theme,omitempty"`
	Numerals          string                   `json:"numerals,omitempty"`
	LandingPage       string                   `json:"landing_page,omitempty"`
	TablePageSize     int                      `json:"table_page_size,omitempty"`
	NotificationPrefs map[string]NotifChannels `json:"notification_prefs,omitempty"`
	// DashboardLayout (v2-10, FR-90.1) is one more optional key — no schema
	// version bump, no migration. nil (key absent) means "default layout";
	// a non-nil value (even an empty widget list) is an explicit choice,
	// re-filtered by the caller's current permissions at render time, never
	// a stored snapshot (FR-90.3).
	DashboardLayout *DashboardLayout `json:"dashboard_layout,omitempty"`
}

// DashboardLayout is v2-10 contract C2's stored shape.
type DashboardLayout struct {
	Widgets []DashboardWidgetRef `json:"widgets"`
}

// DashboardWidgetRef is one entry of a stored dashboard layout.
type DashboardWidgetRef struct {
	ID   string `json:"id"`
	Size string `json:"size"` // "1x" | "2x"
}

// validNotificationKeys is the closed set C1 validates notification_prefs
// keys against: the nine monitorsvc alert rule types plus FR-80's
// payment_tickets_all. Kept as a local copy (not an import of monitorsvc,
// which internal/auth does not depend on) — see the validation test for the
// cross-check that keeps this list honest.
var validNotificationKeys = map[string]bool{
	"nas_down":            true,
	"nas_up":              true,
	"device_down":         true,
	"device_up":           true,
	"radius_reject_spike": true,
	"acct_backlog":        true,
	"disk_low":            true,
	"expiring_digest":     true,
	"agent_balance_low":   true,
	"payment_tickets_all": true,
}

var validThemes = map[string]bool{"": true, "light": true, "dark": true, "system": true}
var validLanguages = map[string]bool{"": true, "en": true, "ar": true, "ku": true}
var validNumerals = map[string]bool{"": true, "auto": true, "latn": true, "arab": true}
var validTablePageSizes = map[int]bool{0: true, 10: true, 25: true, 50: true, 100: true}

// validDashboardWidgetIDs is v2-10 contract C1's catalog id set, duplicated
// here rather than imported: internal/monitorsvc imports internal/auth (for
// auth.Require/Manager/ScopeFilter), so the reverse import would cycle.
// Mirrors the existing subscribers/importer phone-validation duplication
// pattern in this codebase — this package only needs to know the closed set
// exists, never permissions or sizes, which stay in monitorsvc's catalog.
var validDashboardWidgetIDs = map[string]bool{
	"online-now":              true,
	"revenue-today":           true,
	"radius-rps":              true,
	"subs-active":             true,
	"subs-expired":            true,
	"subs-expiring":           true,
	"pipeline-health":         true,
	"nas-health":              true,
	"my-balance":              true,
	"pending-payment-tickets": true,
	"alerts-feed":             true,
}

var validDashboardWidgetSizes = map[string]bool{"1x": true, "2x": true}

// GetPreferences resolves a manager's preferences (C1/C2). A manager with no
// row gets the zero-value document — never an error — since "no preferences
// set" is the common, valid state for every manager on a fresh install.
func GetPreferences(ctx context.Context, db *pgxpool.Pool, managerID string) (Preferences, error) {
	var raw []byte
	err := db.QueryRow(ctx,
		`SELECT doc FROM manager_preferences WHERE manager_id = $1::uuid`, managerID).Scan(&raw)
	if err == pgx.ErrNoRows {
		return Preferences{}, nil
	}
	if err != nil {
		return Preferences{}, err
	}
	var p Preferences
	if err := json.Unmarshal(raw, &p); err != nil {
		return Preferences{}, err
	}
	return p, nil
}

// SetPreferences upserts a manager's full preferences document (C3, full
// replace — an omitted field reverts to unset).
func SetPreferences(ctx context.Context, db *pgxpool.Pool, managerID string, p Preferences) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx,
		`INSERT INTO manager_preferences (manager_id, doc, updated_at)
		 VALUES ($1::uuid, $2, now())
		 ON CONFLICT (manager_id) DO UPDATE SET doc = $2, updated_at = now()`,
		managerID, raw)
	return err
}

// validatePreferences enforces C3's enum/range/closed-set rules, returning
// field_errors naming the offending JSON path. Nothing is written on a
// non-empty return — the caller must not call SetPreferences.
func validatePreferences(p Preferences) []fieldErrorLike {
	var errs []fieldErrorLike
	if !validLanguages[p.Language] {
		errs = append(errs, fieldErrorLike{"language", "must be one of: en ar ku"})
	}
	if !validThemes[p.Theme] {
		errs = append(errs, fieldErrorLike{"theme", "must be one of: light dark system"})
	}
	if !validNumerals[p.Numerals] {
		errs = append(errs, fieldErrorLike{"numerals", "must be one of: auto latn arab"})
	}
	if !validTablePageSizes[p.TablePageSize] {
		errs = append(errs, fieldErrorLike{"table_page_size", "must be one of: 10 25 50 100"})
	}
	for key := range p.NotificationPrefs {
		if !validNotificationKeys[key] {
			errs = append(errs, fieldErrorLike{"notification_prefs." + key, "unknown notification key"})
		}
	}
	if p.DashboardLayout != nil {
		for i, w := range p.DashboardLayout.Widgets {
			prefix := fmt.Sprintf("dashboard_layout.widgets.%d.", i)
			if !validDashboardWidgetIDs[w.ID] {
				errs = append(errs, fieldErrorLike{prefix + "id", "unknown widget id"})
			}
			if !validDashboardWidgetSizes[w.Size] {
				errs = append(errs, fieldErrorLike{prefix + "size", "must be one of: 1x 2x"})
			}
		}
	}
	return errs
}

// fieldErrorLike avoids an internal/httpapi import cycle in this file; the API
// handler converts it to httpapi.FieldError.
type fieldErrorLike struct {
	Field   string
	Message string
}
